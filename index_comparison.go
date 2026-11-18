package main

import (
	"context"
	"strings"
)

// ':' identifies the live index, not HEAD or the working tree.
func readIndexComparison(ctx context.Context, tree WorkingTree, file WorkingFile, mode string) (result FileComparison, err error) {
	if mode == "combined" || mode == "file" {
		if mode == "file" {
			tree.Head = ""
		}
		return readWorkingComparisonContext(ctx, tree, file)
	}
	result.Filename = file.Path
	defer result.prepare()
	oldPath := file.OldPath
	if oldPath == "" {
		oldPath = file.Path
	}
	if mode == "worktree" {
		result, err = readWorkingComparisonContext(ctx, WorkingTree{Repository: tree.Repository}, file)
		if err != nil || result.Notice != "" {
			return result, err
		}
		result.Before, result.Notice, err = readBlobContext(ctx, tree.Repository.Path, ":", file.Path)
		result.BeforeSource = CodeSource{Repository: tree.Repository.Path, Path: file.Path, Revision: ":"}
	} else {
		result.Before, result.Notice, err = readBlobContext(ctx, tree.Repository.Path, tree.Head, oldPath)
		result.BeforeSource = CodeSource{Repository: tree.Repository.Path, Path: oldPath, Revision: tree.Head}
		if err == nil && result.Notice == "" {
			result.After, result.Notice, err = readBlobContext(ctx, tree.Repository.Path, ":", file.Path)
		}
		result.AfterSource = CodeSource{Repository: tree.Repository.Path, Path: file.Path, Revision: ":"}
	}
	if err != nil || result.Notice != "" {
		return result, err
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=0"}
	if mode == "index" {
		args = append(args, "--cached")
		if tree.Head != "" {
			args = append(args, tree.Head)
		}
	}
	args = append(args, "--", ":(literal)"+file.Path)
	if oldPath != file.Path {
		args = append(args, ":(literal)"+oldPath)
	}
	patch, e := gitContext(ctx, tree.Repository.Path, args...)
	if e != nil {
		return result, e
	}
	result.Hunks = parseDiffHunks(patch)
	result.OldLines, result.NewLines = changedLines(patch)
	// Untracked files are absent from git diff and still need a complete added block.
	if mode == "worktree" && strings.Contains(file.Status, "?") {
		result.Hunks = nil
	}
	return result, nil
}
func (a *client) loadEditorWorkingMode(id, mode string) {
	tab := a.editorByID(id)
	if tab == nil {
		return
	}
	if tab.Kind == "file" && tab.Code != nil && tab.CodeSource.Revision != "" {
		a.reloadDefinitionEditor(id)
		return
	}
	tab.Mode = mode
	tab.Document.Request = ""
	tab.Loading = true
	tab.Error = ""
	tree, file := tab.Tree, tab.File
	a.startRead("editor:"+id, func(ctx context.Context) func() {
		fresh := readWorkingTreeContext(ctx, tree.Repository)
		if fresh.Error == "" {
			tree.Head, tree.Ref = fresh.Head, fresh.Ref
			tree.Repository = fresh.Repository
		}
		comparison, err := readIndexComparison(ctx, tree, file, mode)
		return func() {
			tab := a.editorByID(id)
			if tab == nil {
				return
			}
			tab.Loading = false
			tab.setComparison(comparison)
			a.refreshEditorPreview(id)
			tab.Tree.Head, tab.Tree.Ref = tree.Head, tree.Ref
			tab.Tree.Repository = tree.Repository
			if err != nil {
				tab.Error = err.Error()
			}
		}
	})
}
