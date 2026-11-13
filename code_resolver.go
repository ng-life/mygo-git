package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const navigationFileLimit = 250
const navigationMatchLimit = 60

type CodeTarget struct {
	Source     CodeSource
	Definition CodeDefinition
}
type CodeResolution struct {
	Targets []CodeTarget
	Notice  string
}

func languageGlobs(language string) []string {
	switch language {
	case "Go":
		return []string{"*.go"}
	case "Java":
		return []string{"*.java"}
	case "C#":
		return []string{"*.cs", "*.csx"}
	case "JavaScript", "TypeScript":
		return []string{"*.js", "*.jsx", "*.ts", "*.tsx", "*.mjs", "*.cjs"}
	case "Python":
		return []string{"*.py", "*.pyi"}
	case "C", "C++":
		return []string{"*.c", "*.h", "*.cpp", "*.hpp", "*.cc"}
	}
	return nil
}
func safeCodePath(root, file string) (string, error) {
	if !validWorkingPath(file) {
		return "", fmt.Errorf("无效文件路径")
	}
	full := filepath.Join(root, filepath.FromSlash(file))
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("文件位于仓库之外")
	}
	return real, nil
}
func readCodeSource(ctx context.Context, source CodeSource) (DiffDocument, error) {
	var text string
	if source.Revision != "" {
		content, notice, err := readBlobContext(ctx, source.Repository, source.Revision, source.Path)
		if err != nil {
			return DiffDocument{}, err
		}
		if notice != "" {
			return DiffDocument{}, errors.New(notice)
		}
		text = content
	} else {
		full, err := safeCodePath(source.Repository, source.Path)
		if err != nil {
			return DiffDocument{}, err
		}
		file, err := os.Open(full)
		if err != nil {
			return DiffDocument{}, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return DiffDocument{}, err
		}
		if !info.Mode().IsRegular() {
			return DiffDocument{}, fmt.Errorf("仅支持普通文本文件")
		}
		data, err := io.ReadAll(io.LimitReader(file, blobLimit+1))
		if err != nil {
			return DiffDocument{}, err
		}
		if len(data) > blobLimit {
			return DiffDocument{}, fmt.Errorf("文件超过 1 MiB")
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			return DiffDocument{}, fmt.Errorf("二进制文件")
		}
		text = string(data)
	}
	if err := ctx.Err(); err != nil {
		return DiffDocument{}, err
	}
	document := prepareDiff(text)
	highlightDocument(&document, source.Path)
	return document, nil
}
func resolveCode(ctx context.Context, source CodeSource, document DiffDocument, ref CodeReference) (CodeResolution, error) {
	result := CodeResolution{}
	if document.Analysis == nil {
		return result, fmt.Errorf("当前文件不支持语法分析")
	}
	analysis := document.Analysis
	for _, def := range analysis.localTargets(ref) {
		result.Targets = append(result.Targets, CodeTarget{Source: source, Definition: def})
	}
	if len(result.Targets) > 0 {
		return result, nil
	}
	if len(languageGlobs(analysis.Language)) == 0 {
		return result, fmt.Errorf("当前语言暂不支持跨文件跳转")
	}
	searchRef := ref
	if ref.Qualifier == "" {
		for _, imp := range analysis.Imports {
			if imp.Alias == ref.Name && imp.Name != "" {
				searchRef.Name = imp.Name
				break
			}
		}
	}
	files, notice, err := candidateCodeFiles(ctx, source, analysis, searchRef)
	if err != nil {
		return result, err
	}
	result.Notice = notice
	expected := analysis.receiverType(ref)
	for _, file := range files {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if file == source.Path {
			continue
		}
		targetSource := source
		targetSource.Path = file
		candidate, err := readCodeSource(ctx, targetSource)
		if err != nil {
			result.Notice = "部分候选文件无法读取；结果可能不完整"
			continue
		}
		if candidate.Analysis == nil {
			continue
		}
		if analysis.Language == "Go" && ref.Qualifier == "" && candidate.Analysis.Package != analysis.Package {
			continue
		}

		for _, def := range candidate.Analysis.Definitions {
			if def.Name != searchRef.Name || def.ScopeStart != 0 {
				continue
			}
			if ref.Call && def.Kind != "function" && def.Kind != "method" && def.Kind != "type" {
				continue
			}
			if ref.Call && ref.Arity >= 0 && def.Arity >= 0 && ref.Arity != def.Arity {
				continue
			}
			if ref.Qualifier != "" && expected != "" && def.Container != expected {
				continue
			}
			if ref.Qualifier != "" && expected == "" && def.Container != "" {
				continue
			}
			if ref.Qualifier == "" && def.Kind == "method" {
				continue
			}
			result.Targets = append(result.Targets, CodeTarget{Source: targetSource, Definition: def})
		}
	}
	sort.SliceStable(result.Targets, func(i, j int) bool {
		a, b := result.Targets[i], result.Targets[j]
		if a.Source.Path != b.Source.Path {
			return a.Source.Path < b.Source.Path
		}
		return a.Definition.Line < b.Definition.Line
	})
	return result, nil
}
func goCandidateDirectory(ctx context.Context, source CodeSource, a *CodeAnalysis, ref CodeReference) (string, bool) {
	if ref.Qualifier == "" || a.receiverType(ref) != "" {
		return path.Dir(source.Path), true
	}
	imported := ""
	for _, imp := range a.Imports {
		if imp.Alias == ref.Qualifier {
			imported = imp.Path
		}
	}
	if imported == "" {
		return "", false
	}
	moduleSource := source
	moduleSource.Path = "go.mod"
	module, err := readCodeSource(ctx, moduleSource)
	if err == nil {
		for _, line := range module.Lines {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				if imported == fields[1] {
					return ".", true
				}
				if strings.HasPrefix(imported, fields[1]+"/") {
					return strings.TrimPrefix(imported, fields[1]+"/"), true
				}
			}
		}
	}
	return "", false
}

func candidateCodeFiles(ctx context.Context, source CodeSource, a *CodeAnalysis, ref CodeReference) ([]string, string, error) {
	root := source.Repository
	goDirectory := ""
	if a.Language == "Go" {
		directory, ok := goCandidateDirectory(ctx, source, a, ref)
		if !ok {
			return nil, "外部依赖或未识别的 Go 模块路径不在仓库搜索范围内", nil
		}
		goDirectory = directory
	}
	allowed := func(file string) bool {
		if a.Language == "Go" {
			if path.Dir(file) != goDirectory {
				return false
			}
			if !strings.HasSuffix(source.Path, "_test.go") && strings.HasSuffix(file, "_test.go") {
				return false
			}
		}
		return candidatePathAllowed(source, a, ref, file)
	}
	notice := ""
	if source.Revision != "" {
		// Search exact historical blobs, never current worktree contents.
		temp, err := os.MkdirTemp("", "mygo-code-snapshot-*")
		if err != nil {
			return nil, "", err
		}
		root = temp
		defer os.RemoveAll(temp)
		list, err := rawGitContext(ctx, source.Repository, "ls-tree", "-r", "--name-only", "-z", source.Revision)
		if err != nil {
			return nil, "", err
		}
		copied := 0
		for _, file := range strings.Split(string(list), "\x00") {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			if !codeFileMatches(file, a.Language) || !allowed(file) {
				continue
			}
			if copied >= navigationFileLimit {
				notice = "历史快照仅搜索前 250 个相关文件；结果可能不完整"
				break
			}
			content, warning, err := readBlobContext(ctx, source.Repository, source.Revision, file)
			if err != nil || warning != "" {
				continue
			}
			target := filepath.Join(temp, filepath.FromSlash(file))
			if !validWorkingPath(file) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return nil, "", err
			}
			if err := os.WriteFile(target, []byte(content), 0600); err != nil {
				return nil, "", err
			}
			copied++
		}
	}
	args := []string{"--max-filesize", "1M", "--files-with-matches", "--null", "--fixed-strings", "--word-regexp", "--hidden", "--glob", "!.git/**", "--glob", "!node_modules/**", "--glob", "!vendor/**", "--glob", "!**/bin/**", "--glob", "!**/obj/**"}
	for _, glob := range languageGlobs(a.Language) {
		args = append(args, "--glob", glob)
	}
	args = append(args, "--", ref.Name, ".")
	searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(searchCtx, ripgrepExecutable(), args...)
	cmd.Dir = root
	cmd.WaitDelay = 250 * time.Millisecond
	configureGitCancellation(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if searchCtx.Err() != nil {
		return nil, "", searchCtx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			if errors.Is(err, exec.ErrNotFound) {
				return nil, "", fmt.Errorf("跨文件跳转需要安装 ripgrep（rg）")
			}
			return nil, "", fmt.Errorf("ripgrep 搜索失败：%s: %w", stderr.String(), err)
		}
	}
	var files []string
	for _, file := range strings.Split(string(out), "\x00") {
		file = filepath.ToSlash(strings.TrimPrefix(file, "./"))
		if file == "" || !allowed(file) {
			continue
		}
		files = append(files, file)
	}
	sort.Strings(files)
	if len(files) > navigationMatchLimit {
		files = files[:navigationMatchLimit]
		notice = "仅分析前 60 个匹配文件；结果可能不完整"
	}
	return files, notice, nil
}
func codeFileMatches(file, language string) bool {
	for _, glob := range languageGlobs(language) {
		if ok, _ := path.Match(glob, path.Base(file)); ok {
			return true
		}
	}
	return false
}
func candidatePathAllowed(source CodeSource, a *CodeAnalysis, ref CodeReference, file string) bool {
	if !validWorkingPath(file) {
		return false
	}
	if a.Language == "Go" && (ref.Qualifier == "" || a.receiverType(ref) != "") {
		return path.Dir(file) == path.Dir(source.Path)
	}
	// Relative JS/TS imports narrow candidates before parsing declarations.
	if a.Language == "JavaScript" || a.Language == "TypeScript" {
		for _, imp := range a.Imports {
			if (imp.Alias == ref.Name || imp.Name == ref.Name || imp.Alias == ref.Qualifier) && strings.HasPrefix(imp.Path, ".") {
				base := path.Clean(path.Join(path.Dir(source.Path), imp.Path))
				stem := strings.TrimSuffix(file, path.Ext(file))
				return file == base || stem == base || path.Dir(file) == base && path.Base(stem) == "index"
			}
		}
	}
	return true
}

// GUI launchers on macOS may omit Homebrew from PATH.
func ripgrepExecutable() string {
	if executable, err := exec.LookPath("rg"); err == nil {
		return executable
	}
	for _, executable := range []string{"/opt/homebrew/bin/rg", "/usr/local/bin/rg"} {
		if info, err := os.Stat(executable); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return executable
		}
	}
	return "rg"
}
