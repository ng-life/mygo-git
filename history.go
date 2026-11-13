package main

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const historyLimit = 200
const blobLimit = 1024 * 1024

type HistorySource struct {
	Repository Repository
	Refs       []string
}
type Commit struct {
	Repository            Repository
	Hash, Author, Subject string
	Parents               []string
	Timestamp             int64
}

func (c Commit) Key() string { return c.Repository.Path + ":" + c.Hash }
func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

type ChangedFile struct{ Status, Path, OldPath string }

func (f ChangedFile) Label() string {
	if f.OldPath != "" {
		return f.OldPath + " → " + f.Path
	}
	return f.Path
}

type CommitDetail struct {
	Commit Commit
	Parent string
	Files  []ChangedFile
}
type FileComparison struct {
	IgnoreWhitespace              bool
	BeforeSource, AfterSource     CodeSource
	Version                       string
	Filename                      string
	Hunks                         []DiffHunk
	ReviewRows                    []ReviewRow
	ReviewHunks                   int
	BeforeDocument, AfterDocument DiffDocument
	Before, After, Notice         string
	OldLines, NewLines            map[int]bool
}

func historySources(repos []Repository, group string, remote bool) []HistorySource {
	var sources []HistorySource
	for _, r := range repos {
		s := HistorySource{Repository: r}
		if group == "" {
			s.Refs = []string{"HEAD"}
		} else {
			for _, branch := range repositoryBranches(r, remote) {
				key := "/" + branch
				if key == group || strings.HasPrefix(key, group+"/") {
					s.Refs = append(s.Refs, fullBranchRef(branch, remote))
				}
			}
		}
		if len(s.Refs) > 0 {
			sources = append(sources, s)
		}
	}
	return sources
}
func readHistory(sources []HistorySource) ([]Commit, string) {
	return readHistoryContext(context.Background(), sources)
}
func readHistoryContext(ctx context.Context, sources []HistorySource) ([]Commit, string) {
	var commits []Commit
	var errors []string
	for _, source := range sources {
		args := []string{"log", fmt.Sprintf("-%d", historyLimit), "--format=%H%x00%ct%x00%an%x00%s%x00%P%x00"}
		args = append(args, source.Refs...)
		args = append(args, "--")
		out, err := gitContext(ctx, source.Repository.Path, args...)
		if err != nil {
			errors = append(errors, source.Repository.Name+": "+out)
			continue
		}
		fields := strings.Split(out, "\x00")
		for i := 0; i+4 < len(fields); i += 5 {
			timestamp, err := strconv.ParseInt(fields[i+1], 10, 64)
			if err != nil {
				continue
			}
			commits = append(commits, Commit{Repository: source.Repository, Hash: strings.TrimSpace(fields[i]), Timestamp: timestamp, Author: fields[i+2], Subject: fields[i+3], Parents: strings.Fields(fields[i+4])})
		}
	}
	// Preserve Git's newest-first order within a repository when commit
	// timestamps tie (several commits often share the same second).
	sort.SliceStable(commits, func(i, j int) bool { return commits[i].Timestamp > commits[j].Timestamp })
	return commits, strings.Join(errors, "\n")
}
func readCommit(commit Commit) (CommitDetail, error) {
	return readCommitContext(context.Background(), commit)
}
func readCommitContext(ctx context.Context, commit Commit) (CommitDetail, error) {
	d := CommitDetail{Commit: commit}
	parents, err := gitContext(ctx, commit.Repository.Path, "rev-list", "--parents", "-n", "1", commit.Hash, "--")
	if err != nil {
		return d, fmt.Errorf("读取父提交：%s: %w", parents, err)
	}
	fields := strings.Fields(parents)
	if len(fields) > 1 {
		d.Parent = fields[1]
	}
	var args []string
	if d.Parent != "" {
		args = []string{"diff", "--name-status", "-z", "-M", d.Parent, commit.Hash, "--"}
	} else {
		args = []string{"diff-tree", "--root", "--no-commit-id", "--name-status", "-z", "-r", "-M", commit.Hash, "--"}
	}
	out, err := gitContext(ctx, commit.Repository.Path, args...)
	if err != nil {
		return d, fmt.Errorf("读取变更文件：%s: %w", out, err)
	}
	tokens := strings.Split(out, "\x00")
	for i := 0; i+1 < len(tokens) && tokens[i] != ""; {
		f := ChangedFile{Status: tokens[i], Path: tokens[i+1]}
		i += 2
		if strings.HasPrefix(f.Status, "R") || strings.HasPrefix(f.Status, "C") {
			if i >= len(tokens) {
				return d, fmt.Errorf("变更文件格式不完整")
			}
			f.OldPath = f.Path
			f.Path = tokens[i]
			i++
		}
		d.Files = append(d.Files, f)
	}
	return d, nil
}

// Blob output must remain byte-exact; the normal git helper trims command messages.
func rawGit(path string, args ...string) ([]byte, error) {
	return rawGitContext(context.Background(), path, args...)
}
func rawGitContext(ctx context.Context, path string, args ...string) ([]byte, error) {
	return commandGit(ctx, 30*time.Second, path, args...)
}

func readBlob(path, revision, file string) (string, string, error) {
	return readBlobContext(context.Background(), path, revision, file)
}
func readBlobContext(ctx context.Context, path, revision, file string) (string, string, error) {
	if revision == "" || file == "" {
		return "", "", nil
	}
	out, err := gitContext(ctx, path, "ls-tree", "-z", revision, "--", ":(literal)"+file)
	if err != nil {
		return "", "", err
	}
	if out == "" {
		return "", "", nil
	}
	metadata, _, _ := strings.Cut(out, "\t")
	fields := strings.Fields(metadata)
	if len(fields) != 3 {
		return "", "", fmt.Errorf("无法读取文件对象")
	}
	if fields[1] != "blob" {
		return "", "子模块对象：" + fields[2], nil
	}
	sizeText, err := gitContext(ctx, path, "cat-file", "-s", fields[2])
	if err != nil {
		return "", "", err
	}
	size, err := strconv.Atoi(sizeText)
	if err != nil {
		return "", "", err
	}
	if size > comparisonBlobLimit(ctx) {
		return "", fmt.Sprintf("文件超过 %d MiB，未加载文本", comparisonBlobLimit(ctx)/(1024*1024)), nil
	}
	data, err := rawGitContext(ctx, path, "cat-file", "blob", fields[2])
	if err != nil {
		return "", "", err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", "二进制或非 UTF-8 文件，无法进行文本比较", nil
	}
	return string(data), "", nil
}

var hunkPattern = regexp.MustCompile(`(?m)^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func changedLines(patch string) (map[int]bool, map[int]bool) {
	old, new := map[int]bool{}, map[int]bool{}
	for _, m := range hunkPattern.FindAllStringSubmatch(patch, -1) {
		for _, part := range []struct {
			start, count string
			lines        map[int]bool
		}{{m[1], m[2], old}, {m[3], m[4], new}} {
			start, _ := strconv.Atoi(part.start)
			count := 1
			if part.count != "" {
				count, _ = strconv.Atoi(part.count)
			}
			for n := start; n < start+count; n++ {
				part.lines[n] = true
			}
		}
	}
	return old, new
}
func readComparison(d CommitDetail, f ChangedFile) (FileComparison, error) {
	return readComparisonContext(context.Background(), d, f)
}
func readComparisonContext(ctx context.Context, d CommitDetail, f ChangedFile) (result FileComparison, resultErr error) {
	result.Filename = f.Path
	result.BeforeSource = CodeSource{Repository: d.Commit.Repository.Path, Path: f.Path, Revision: d.Parent}
	if f.OldPath != "" {
		result.BeforeSource.Path = f.OldPath
	}
	result.AfterSource = CodeSource{Repository: d.Commit.Repository.Path, Path: f.Path, Revision: d.Commit.Hash}
	defer result.prepare()
	path := d.Commit.Repository.Path
	oldPath := f.OldPath
	if oldPath == "" {
		oldPath = f.Path
	}
	var beforeNotice, afterNotice string
	var err error
	if !strings.HasPrefix(f.Status, "A") {
		result.Before, beforeNotice, err = readBlobContext(ctx, path, d.Parent, oldPath)
		if err != nil {
			return result, err
		}
	}
	if !strings.HasPrefix(f.Status, "D") {
		result.After, afterNotice, err = readBlobContext(ctx, path, d.Commit.Hash, f.Path)
		if err != nil {
			return result, err
		}
	}
	if beforeNotice != "" || afterNotice != "" {
		result.Notice = strings.TrimSpace(beforeNotice + "\n" + afterNotice)
		return result, nil
	}
	var args []string
	if d.Parent != "" {
		args = []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=0", d.Parent, d.Commit.Hash, "--"}
	} else {
		args = []string{"diff-tree", "--root", "--no-commit-id", "-p", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=0", d.Commit.Hash, "--"}
	}
	args = append(args, ":(literal)"+oldPath)
	if oldPath != f.Path {
		args = append(args, ":(literal)"+f.Path)
	}
	patch, err := gitContext(ctx, path, args...)
	if err != nil {
		return result, err
	}
	result.Hunks = parseDiffHunks(patch)
	result.OldLines, result.NewLines = changedLines(patch)
	return result, nil
}
