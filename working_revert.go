package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cheap filesystem snapshot prevents discarding edits made after a refresh.
func workingFileStamp(repository string, file WorkingFile) string {
	var result []string
	for _, path := range []string{file.Path, file.OldPath} {
		if path == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(repository, filepath.FromSlash(path)))
		if err != nil {
			result = append(result, path+":"+err.Error())
			continue
		}
		result = append(result, fmt.Sprintf("%s:%d:%d:%d", path, info.Mode(), info.Size(), info.ModTime().UnixNano()))
	}
	return strings.Join(result, "\x00")
}
func revertWorkingFiles(request CommitRequest) (string, error) {
	tree := request.Tree
	path := tree.Repository.Path
	if operationInProgress(path) {
		return "", fmt.Errorf("Finish the in-progress Git operation first")
	}
	current := readWorkingTree(tree.Repository)
	if current.Error != "" {
		return "", fmt.Errorf("%s", current.Error)
	}
	if current.Head != tree.Head || current.Ref != tree.Ref {
		return "", fmt.Errorf("HEAD or branch changed. Refresh to continue")
	}
	indexed := map[string]WorkingFile{}
	for _, file := range current.Files {
		indexed[file.Path] = file
	}
	selected := map[string]bool{}
	for _, file := range request.Files {
		selected[file.Path] = true
	}
	var restore, remove []string
	seen := map[string]bool{}
	for _, file := range request.Files {
		now, exists := indexed[file.Path]
		if !exists || now.Status != file.Status || now.OldPath != file.OldPath || file.Stamp != "" && file.Stamp != now.Stamp {
			return "", fmt.Errorf("File status or content changed. Refresh: %s", file.Path)
		}
		if file.Conflicted() {
			return "", fmt.Errorf("Cannot automatically revert conflicted file: %s", file.Path)
		}
		if file.OldPath != "" && !selected[file.OldPath] {
			if strings.Contains(file.Status, "R") {
				if _, err := os.Lstat(filepath.Join(path, filepath.FromSlash(file.OldPath))); err == nil {
					return "", fmt.Errorf("Original rename path was recreated. Resolve first: %s", file.OldPath)
				}
			}
			if _, exists := indexed[file.OldPath]; exists {
				return "", fmt.Errorf("Original rename path has other changes. Include them or resolve first: %s", file.OldPath)
			}
		}
		for _, p := range []string{file.Path, file.OldPath} {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			if !validWorkingPath(p) {
				return "", fmt.Errorf("Invalid file path")
			}
			info, err := os.Lstat(filepath.Join(path, filepath.FromSlash(p)))
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
			if err == nil && info.IsDir() {
				return "", fmt.Errorf("Cannot automatically delete directory or submodule: %s", p)
			}
			existsInHead := ""
			if tree.Head != "" {
				var err error
				existsInHead, err = git(path, "ls-tree", "-z", tree.Head, "--", ":(literal)"+p)
				if err != nil {
					return "", err
				}
			}
			if existsInHead != "" {
				restore = append(restore, ":(literal)"+p)
			} else {
				remove = append(remove, p)
			}
		}
	}
	if len(restore) > 0 {
		args := []string{"restore", "--source=" + tree.Head, "--staged", "--worktree", "--"}
		args = append(args, restore...)
		if out, err := git(path, args...); err != nil {
			return out, fmt.Errorf("Failed to restore tracked files. Some files may already be restored; refresh: %w", err)
		}
	}
	for _, p := range remove {
		if out, err := git(path, "rm", "--cached", "-f", "--ignore-unmatch", "--", ":(literal)"+p); err != nil {
			return out, err
		}
		if err := os.Remove(filepath.Join(path, filepath.FromSlash(p))); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("Failed to delete uncommitted file: %s: %w", p, err)
		}
	}
	return fmt.Sprintf("Reverted %d files to HEAD and removed selected staged/untracked changes", len(request.Files)), nil
}
