package main

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
)

func readChangeView(ctx context.Context, repos []Repository, scope string) []WorkingTree {
	var trees []WorkingTree
	for _, repo := range repos {
		tree := WorkingTree{Repository: repo}
		tree.Head, tree.Ref = currentPositionContext(ctx, repo.Path)
		raw, err := rawGitContext(ctx, repo.Path, "status", "--porcelain=v1", "-z", "--untracked-files=all")
		if err != nil {
			tree.Error = err.Error()
		} else {
			files, e := parseWorkingStatus(raw)
			if e != nil {
				tree.Error = e.Error()
			}
			for _, file := range files {
				if len(file.Status) != 2 {
					continue
				}
				if scope == "index" && file.Status[0] != ' ' && file.Status[0] != '?' || scope == "worktree" && (file.Status[1] != ' ' || file.Status == "??") {
					if scope == "index" {
						file.Change = file.Status[:1]
					} else if file.Status != "??" {
						file.Change = file.Status[1:]
					}
					tree.Files = append(tree.Files, file)
				}
			}
		}
		if len(tree.Files) > 0 || tree.Error != "" {
			trees = append(trees, tree)
		}
	}
	return trees
}
func (a *client) reloadChangeView() {
	if a.changeViewMode == "" || a.tab != 1 {
		return
	}
	mode := a.changeViewMode
	repos := append([]Repository(nil), a.repos...)
	a.changeViewLoading = true
	a.startRead("change-view", func(ctx context.Context) func() {
		trees := readChangeView(ctx, repos, mode)
		return func() {
			if a.changeViewMode == mode && a.tab == 1 {
				a.changeViewTrees = trees
				a.changeViewLoading = false
			}
		}
	})
}
func (a *client) changeScopeTabs(c *ui.Context) {
	ui.Row(c).Padding(4, 6).Gap(4).Children(func() {
		for _, scope := range []struct{ ID, Label string }{{"", "Changelists"}, {"worktree", "Worktree"}, {"index", "Index"}} {
			ui.Button(c.Key(scope.ID), scope.Label).FontSize(11).Label("Changes View " + scope.Label).Selected(a.changeViewMode == scope.ID).OnClick(func() { a.changeViewMode = scope.ID; a.reloadChangeView() })
		}
	})
}
func (a *client) scopedChangesPanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(4).Children(func() {
		a.changeScopeTabs(c)
		ui.Row(c).Padding(4, 6).Children(func() {
			title := "Unstaged Changes"
			if a.changeViewMode == "index" {
				title = "Staged Changes"
			}
			ui.Text(c, title).Bold()
			ui.Spacer(c)
			ui.Button(c, "Refresh").OnClick(a.reloadChangeView)
		})
		if a.changeViewLoading {
			ui.Text(c, "Loading changes…").Padding(6)
		}
		ui.Scroll(c).Grow(1).Children(func() {
			if len(a.changeViewTrees) == 0 && !a.changeViewLoading {
				ui.Text(c, "No changes").Padding(8).TextColor(c.Theme().TextMuted)
			}
			for _, tree := range a.changeViewTrees {
				if tree.Error != "" {
					ui.Text(c, tree.Repository.Name+": "+tree.Error).Padding(6)
				}
				open := a.referenceOpen("change-view:" + a.changeViewMode + tree.Repository.Path)
				ui.Tree(c.Key(tree.Repository.Path), func() {
					treeContentAction(folderTreeItem(c.Key("repo"), tree.Repository.Name, open, func() { a.changeViewNodes(c, tree, workingHierarchy(tree.Files)) }).Tooltip(tree.Repository.Path), nil, func() { *open = !*open })
				})
			}
		})
		ui.Text(c, "Read-only view. Commit selected files from Changelists.").FontSize(11).Padding(6).TextColor(c.Theme().TextMuted)
	})
}
func (a *client) changeViewNodes(c *ui.Context, tree WorkingTree, node *workingNode) {
	for _, child := range node.Children {
		if child.File == nil {
			open := a.referenceOpen("change-view-folder:" + a.changeViewMode + tree.Repository.Path + child.Path)
			treeContentAction(folderTreeItem(c.Key(child.Path), child.Name, open, func() { a.changeViewNodes(c, tree, child) }), nil, func() { *open = !*open })
			continue
		}
		file := *child.File
		fileTreeItem(c.Key(child.Path), child.Name, file.Path).Label("Scoped Change " + a.changeViewMode + " " + tree.Repository.Name + "/" + file.Path).TextColor(workingStatusColor(file, c.Theme().TextMuted)).OnClick(func() { a.openScopedChange(tree, file, a.changeViewMode) })
	}
}
func (a *client) openScopedChange(tree WorkingTree, file WorkingFile, mode string) {
	a.saveEditorContext()
	a.closeReview()
	key := "changes:" + workingFileKey(tree.Repository.Path, file.Path) + "\x00" + mode
	tab := a.openEditor(key, file.Path+" · "+tree.Repository.Name+" · "+strings.ToUpper(mode[:1])+mode[1:]+" Diff", "changes", "")
	tab.Tree, tab.File = tree, file
	if a.changeViewMode == mode {
		tab.Sources = append([]WorkingTree(nil), a.changeViewTrees...)
	} else {
		tab.Sources = []WorkingTree{tree}
	}
	a.clearWorkingComparison()
	a.workingFileTree = tree
	a.workingFile = file
	a.workingFileKey = workingFileKey(tree.Repository.Path, file.Path)
	a.loadEditorWorkingMode(tab.ID, mode)
}
func (a *client) openEditorWorkingMode(id, mode string) {
	tab := a.editorByID(id)
	if tab == nil {
		return
	}
	a.openScopedChange(tab.Tree, tab.File, mode)
}
