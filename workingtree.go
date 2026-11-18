package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type WorkingFile struct{ Status, Path, OldPath, Stamp, Change string }

func (f WorkingFile) Label() string {
	if f.OldPath != "" {
		return f.OldPath + " → " + f.Path
	}
	return f.Path
}
func (f WorkingFile) Conflicted() bool {
	return strings.Contains(f.Status, "U") || f.Status == "AA" || f.Status == "DD"
}

type WorkingTree struct {
	Repository Repository
	Head, Ref  string
	Files      []WorkingFile
	Error      string
}

func workingFileKey(repository, path string) string { return repository + "\x00" + path }
func parseWorkingStatus(data []byte) ([]WorkingFile, error) {
	var files []WorkingFile
	positions := map[string]int{}
	tokens := bytes.Split(data, []byte{0})
	for i := 0; i < len(tokens) && len(tokens[i]) > 0; i++ {
		record := string(tokens[i])
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("Cannot parse working tree status")
		}
		f := WorkingFile{Status: record[:2], Path: record[3:]}
		// In porcelain -z, a rename is emitted as destination NUL source NUL.
		if strings.ContainsAny(f.Status, "RC") {
			i++
			if i >= len(tokens) || len(tokens[i]) == 0 {
				return nil, fmt.Errorf("Incomplete rename paths")
			}
			f.OldPath = string(tokens[i])
		}
		if at, ok := positions[f.Path]; ok {
			previous := files[at]
			if f.Status == "??" {
				previous.Status = previous.Status[:1] + "?"
			} else if previous.Status == "??" {
				previous.Status = f.Status[:1] + "?"
			}
			if previous.OldPath == "" {
				previous.OldPath = f.OldPath
			}
			files[at] = previous
		} else {
			positions[f.Path] = len(files)
			files = append(files, f)
		}
	}
	return files, nil
}
func currentPosition(path string) (head, ref string) {
	return currentPositionContext(context.Background(), path)
}
func currentPositionContext(ctx context.Context, path string) (head, ref string) {
	var err error
	head, err = gitContext(ctx, path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		head = ""
	}
	ref, _ = gitContext(ctx, path, "symbolic-ref", "--quiet", "HEAD")
	if !strings.HasPrefix(ref, "refs/heads/") {
		ref = ""
	}
	return
}
func readWorkingTree(repository Repository) WorkingTree {
	return readWorkingTreeContext(context.Background(), repository)
}
func readWorkingTreeContext(ctx context.Context, repository Repository) WorkingTree {
	tree := WorkingTree{Repository: repository}
	data, err := rawGitContext(ctx, repository.Path, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		tree.Error = err.Error()
		return tree
	}
	tree.Files, err = parseWorkingStatus(data)
	if err != nil {
		tree.Error = err.Error()
		return tree
	}
	tree.Head, tree.Ref = currentPositionContext(ctx, repository.Path)
	tree.Files, err = changelistFiles(ctx, repository.Path, tree.Head, tree.Files)
	if err != nil {
		tree.Error = err.Error()
		return tree
	}
	for i := range tree.Files {
		tree.Files[i].Stamp = workingFileStamp(repository.Path, tree.Files[i])
	}
	if tree.Ref != "" {
		tree.Repository.Branch = strings.TrimPrefix(tree.Ref, "refs/heads/")
	} else {
		tree.Repository.Branch = "detached " + shortHash(tree.Head)
	}
	return tree
}
func readWorkingTrees(repositories []Repository) []WorkingTree {
	return readWorkingTreesContext(context.Background(), repositories)
}
func readWorkingTreesContext(ctx context.Context, repositories []Repository) []WorkingTree {
	trees := make([]WorkingTree, 0, len(repositories))
	for _, r := range repositories {
		trees = append(trees, readWorkingTreeContext(ctx, r))
	}
	return trees
}
func readWorkingComparison(tree WorkingTree, file WorkingFile) (FileComparison, error) {
	return readWorkingComparisonContext(context.Background(), tree, file)
}
func readWorkingComparisonContext(ctx context.Context, tree WorkingTree, file WorkingFile) (result FileComparison, resultErr error) {
	result.Filename = file.Path
	result.BeforeSource = CodeSource{Repository: tree.Repository.Path, Path: file.Path, Revision: tree.Head}
	if file.OldPath != "" {
		result.BeforeSource.Path = file.OldPath
	}
	result.AfterSource = CodeSource{Repository: tree.Repository.Path, Path: file.Path}
	defer result.prepare()
	var err error
	oldPath := file.OldPath
	if oldPath == "" {
		oldPath = file.Path
	}
	result.Before, result.Notice, err = readBlobContext(ctx, tree.Repository.Path, tree.Head, oldPath)
	if err != nil {
		return result, err
	}
	path := filepath.Join(tree.Repository.Path, filepath.FromSlash(file.Path))
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return result, err
	}
	if err == nil {
		if info.IsDir() {
			result.Notice = "Submodule or directory; view and commit in its repository"
			return result, nil
		}
		if info.Size() > int64(comparisonBlobLimit(ctx)) {
			result.Notice = fmt.Sprintf("File exceeds %d MiB; text not loaded", comparisonBlobLimit(ctx)/(1024*1024))
			return result, nil
		}
		var content []byte
		if info.Mode()&os.ModeSymlink != 0 {
			target, e := os.Readlink(path)
			err = e
			content = []byte(target)
		} else {
			content, err = os.ReadFile(path)
		}
		if err != nil {
			return result, err
		}
		if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
			result.Notice = "Text comparison is unavailable for binary or non-UTF-8 files"
			return result, nil
		}
		result.After = string(content)
	}
	if tree.Head != "" && !strings.Contains(file.Status, "?") {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=0", tree.Head, "--", ":(literal)" + file.Path}
		if file.OldPath != "" {
			args = append(args, ":(literal)"+file.OldPath)
		}
		patch, e := gitContext(ctx, tree.Repository.Path, args...)
		if e != nil {
			return result, e
		}
		result.Hunks = parseDiffHunks(patch)
		result.OldLines, result.NewLines = changedLines(patch)
	} else {
		result.OldLines = map[int]bool{}
		result.NewLines = map[int]bool{}
		for _, side := range []struct {
			text  string
			lines map[int]bool
		}{{result.Before, result.OldLines}, {result.After, result.NewLines}} {
			count := strings.Count(side.text, "\n")
			if side.text != "" && !strings.HasSuffix(side.text, "\n") {
				count++
			}
			for i := 0; i < count; i++ {
				side.lines[i+1] = true
			}
		}
	}
	return result, nil
}

type CommitRequest struct {
	Amend bool
	Tree  WorkingTree
	Files []WorkingFile
}

func validWorkingPath(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return path != "" && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !strings.ContainsRune(path, 0)
}
func commitWorkingTree(request CommitRequest, message string) (string, error) {
	return commitWorkingTreeWithCommand(request, message, git)
}
func commitWorkingTreeWithCommand(request CommitRequest, message string, run func(string, ...string) (string, error)) (string, error) {
	tree := request.Tree
	path := tree.Repository.Path
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("Commit message is required")
	}
	if len(request.Files) == 0 && !request.Amend {
		return "", fmt.Errorf("No files selected")
	}
	if tree.Error != "" {
		return "", fmt.Errorf("Failed to read working tree status: %s", tree.Error)
	}
	if operationInProgress(path) {
		return "", fmt.Errorf("Finish the in-progress Git operation first")
	}
	current := readWorkingTree(tree.Repository)
	if current.Error != "" {
		return "", fmt.Errorf("%s", current.Error)
	}
	if current.Head != tree.Head || current.Ref != tree.Ref {
		return "", fmt.Errorf("Repository branch or HEAD changed. Refresh the working tree and retry")
	}
	if request.Amend {
		if current.Head == "" {
			return "", fmt.Errorf("Cannot amend before the initial commit")
		}
		if err := requireUnpublished(path, current.Head); err != nil {
			return "", err
		}
	}
	for _, f := range current.Files {
		if f.Conflicted() {
			return "", fmt.Errorf("Resolve working tree conflicts first")
		}
	}
	currentFiles := map[string]WorkingFile{}
	for _, file := range current.Files {
		currentFiles[file.Path] = file
	}
	selectedPaths := map[string]bool{}
	for _, file := range request.Files {
		selectedPaths[file.Path] = true
	}
	for _, file := range request.Files {
		if file.OldPath != "" && !selectedPaths[file.OldPath] {
			if _, recreated := currentFiles[file.OldPath]; recreated {
				return "", fmt.Errorf("New working tree changes at the original path. Refresh and verify file selection: %s", file.OldPath)
			}
		}
	}
	var stagePaths []string
	stageSeen := map[string]bool{}
	for _, file := range request.Files {
		now, ok := currentFiles[file.Path]
		if !ok || now.OldPath != file.OldPath {
			return "", fmt.Errorf("Selected file status changed. Refresh and retry: %s", file.Path)
		}
		candidates := []string{now.Path}
		// A staged rename/deletion has already removed its old path from the
		// index. Keep that path in the commit scope, but do not add it again.
		if now.Status[0] == 'D' && now.Status[1] == ' ' {
			candidates = nil
		}
		if now.OldPath != "" && now.Status[0] != 'R' && now.Status[0] != 'C' {
			candidates = append(candidates, now.OldPath)
		}
		for _, p := range candidates {
			if !stageSeen[p] {
				stageSeen[p] = true
				stagePaths = append(stagePaths, ":(literal)"+p)
			}
		}
	}
	var paths []string
	seen := map[string]bool{}
	for _, file := range request.Files {
		for _, p := range []string{file.Path, file.OldPath} {
			if p == "" {
				continue
			}
			if !validWorkingPath(p) {
				return "", fmt.Errorf("Invalid file path: %q", p)
			}
			if !seen[p] {
				seen[p] = true
				paths = append(paths, ":(literal)"+p)
			}
		}
	}
	specifications, err := os.CreateTemp("", "mygo-git-paths-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(specifications.Name())
	_, err = specifications.WriteString(strings.Join(paths, "\x00") + "\x00")
	closeErr := specifications.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	msg, err := os.CreateTemp("", "mygo-git-message-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(msg.Name())
	_, err = msg.WriteString(message)
	closeErr = msg.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	// Stage only explicit files. --only excludes all unrelated staged changes.
	var out string
	if len(stagePaths) > 0 {
		staging, err := os.CreateTemp("", "mygo-git-stage-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(staging.Name())
		_, err = staging.WriteString(strings.Join(stagePaths, "\x00") + "\x00")
		closeErr := staging.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		out, err = git(path, "add", "-A", "--pathspec-from-file="+staging.Name(), "--pathspec-file-nul")
		if err != nil {
			return out, fmt.Errorf("Staging failed: %w", err)
		}
	}

	head, ref := currentPosition(path)
	if head != tree.Head || ref != tree.Ref {
		return "", fmt.Errorf("Repository branch or HEAD changed after staging. Index preserved; refresh to verify")
	}
	expectedTree, snapshotErr := git(path, "write-tree")
	if snapshotErr != nil {
		return "", fmt.Errorf("Failed to capture commit snapshot: %w", snapshotErr)
	}
	args := []string{"commit", "--only", "--cleanup=verbatim", "--file", msg.Name()}
	if request.Amend {
		args = append(args, "--amend", "--allow-empty")
	}
	if len(paths) > 0 {
		args = append(args, "--pathspec-from-file="+specifications.Name(), "--pathspec-file-nul")
	}
	out, err = run(path, args...)
	if request.Amend {
		return verifyAmendedCommit(path, tree.Head, tree.Ref, expectedTree, message, out, err, paths)
	}
	return verifyCommit(path, tree.Head, tree.Ref, expectedTree, message, out, err, paths)

}
func executeWorkingCommits(requests []CommitRequest, message string, reporters ...taskReporter) []OperationResult {
	results := make([]OperationResult, 0, len(requests))
	for i, request := range requests {
		if len(reporters) > 0 {
			reporters[0]("Commit · "+request.Tree.Repository.Name, i, len(requests)*3, nil)
		}
		out, err := commitWorkingTree(request, message)
		results = append(results, OperationResult{Repository: request.Tree.Repository, Output: out, Err: err})
		if len(reporters) > 0 {
			reporters[0]("Commit · "+request.Tree.Repository.Name, i+1, len(requests)*3, err)
		}
	}
	return results
}
