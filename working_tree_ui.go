package main

import (
	"path"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

type workingNode struct {
	Name, Path string
	File       *WorkingFile
	Children   []*workingNode
	Files      []WorkingFile
}

func workingHierarchy(files []WorkingFile) *workingNode {
	root := &workingNode{}
	for _, file := range files {
		node := root
		node.Files = append(node.Files, file)
		parts := strings.Split(file.Path, "/")
		for i, name := range parts {
			var child *workingNode
			for _, candidate := range node.Children {
				if candidate.Name == name {
					child = candidate
					break
				}
			}
			if child == nil {
				child = &workingNode{Name: name, Path: strings.Join(parts[:i+1], "/")}
				node.Children = append(node.Children, child)
			}
			child.Files = append(child.Files, file)
			node = child
		}
		copy := file
		node.File = &copy
	}
	var order func(*workingNode)
	order = func(node *workingNode) {
		// A deleted file and a newly created directory can share a Git path.
		if node.File != nil && len(node.Children) > 0 {
			file := node.File
			node.File = nil
			node.Children = append(node.Children, &workingNode{Name: node.Name, Path: node.Path + "\x00original-file", File: file, Files: []WorkingFile{*file}})
		}
		sort.Slice(node.Children, func(i, j int) bool {
			a, b := node.Children[i], node.Children[j]
			if (a.File == nil) != (b.File == nil) {
				return a.File == nil
			}
			return a.Name < b.Name
		})
		for _, child := range node.Children {
			order(child)
			for child.File == nil && len(child.Children) == 1 && child.Children[0].File == nil {
				next := child.Children[0]
				child.Name += "/" + next.Name
				child.Path = next.Path
				child.Children = next.Children
			}
		}
	}
	order(root)
	return root
}
func (a *client) workingCheckState(repository string, files []WorkingFile) ui.CheckState {
	total, selected := 0, 0
	for _, file := range files {
		if file.Conflicted() {
			continue
		}
		total++
		if a.workingSelection[workingFileKey(repository, file.Path)] {
			selected++
		}
	}
	if selected == 0 {
		return ui.CheckOff
	}
	if selected == total {
		return ui.CheckOn
	}
	return ui.CheckMixed
}
func (a *client) toggleWorkingFiles(repository string, files []WorkingFile, on bool) {
	if a.workingSelection == nil {
		a.workingSelection = map[string]bool{}
	}
	for _, file := range files {
		if !file.Conflicted() {
			a.workingSelection[workingFileKey(repository, file.Path)] = on
		}
	}
}
func (a *client) workingDisclosure(c *ui.Context, key, label string) bool {
	if a.workingCollapsed == nil {
		a.workingCollapsed = map[string]bool{}
	}
	arrow := "▾"
	if a.workingCollapsed[key] {
		arrow = "▸"
	}
	ui.ButtonBase(c.Key("disclosure")).Width(18).Height(22).Label("展开或折叠 " + label).OnClick(func() { a.workingCollapsed[key] = !a.workingCollapsed[key] }).Children(func() { ui.Text(c, arrow) })
	return !a.workingCollapsed[key]
}
func (a *client) workingNodes(c *ui.Context, tree WorkingTree, nodes []*workingNode, depth int) {
	for _, node := range nodes {
		ui.Column(c.Key(node.Path)).Children(func() {
			open := true
			line := ui.Row(c).Padding(0, 0, 0, float32(depth*18)).Gap(4).Height(22).AlignItems(ui.Center)
			if node.File != nil && a.workingFileKey == workingFileKey(tree.Repository.Path, node.File.Path) {
				line.Background(ideaSelection)
			}
			line.Children(func() {
				state := a.workingCheckState(tree.Repository.Path, node.Files)
				disabled := a.busy || a.workingLoading || tree.Error != ""
				if node.File == nil {
					open = a.workingDisclosure(c, workingFileKey(a.workingTreeScope(tree), node.Path), node.Path)
					ui.CheckboxState(c.Key("folder-selected"), &state, "").Label("选择文件夹 " + tree.Repository.Name + "/" + node.Path).Disabled(disabled).Tooltip(node.Path).OnClick(func() { a.toggleWorkingFiles(tree.Repository.Path, node.Files, state == ui.CheckOn) })
					treeContentAction(ui.ButtonBase(c.Key("folder-name")).Grow(1).MinWidth(0).Justify(ui.Start).Label("文件夹 "+node.Path).Tooltip(node.Path), nil, func() {
						key := workingFileKey(a.workingTreeScope(tree), node.Path)
						a.workingCollapsed[key] = !a.workingCollapsed[key]
					}).ContextMenu(func(menu *ui.Menu) { a.changelistMoveMenu(menu, tree, node.Files) }).Children(func() {
						ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, workingFolderIcon).Width(14).Height(14).Shrink(0).TextColor(c.Theme().TextMuted)
							ui.Text(c, node.Name).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
							ui.Textf(c, "%d 文件", len(node.Files)).FontSize(11).Shrink(0).TextColor(c.Theme().TextMuted)
							ui.Spacer(c)
						})
					})
					return
				}
				ui.Spacer(c).Width(18).Grow(0).Shrink(0)
				file := *node.File
				color := workingStatusColor(file, c.Theme().TextMuted)
				key := workingFileKey(tree.Repository.Path, file.Path)
				ui.CheckboxState(c.Key("file-selected"), &state, "").Label("选择文件 " + tree.Repository.Name + "/" + file.Path).Disabled(disabled || file.Conflicted()).OnClick(func() { a.toggleWorkingFiles(tree.Repository.Path, []WorkingFile{file}, state == ui.CheckOn) })
				ui.Icon(c, workingFileIcon).Width(14).Height(14).Shrink(0).TextColor(color).Tooltip(file.Status)
				label := path.Base(file.Path)
				if file.OldPath != "" {
					label = path.Base(file.OldPath) + " → " + label
				}
				row := ui.ButtonBase(c.Key("working-file")).Grow(1).MinWidth(0).Justify(ui.Start).Label(file.Label()).Tooltip(file.Label() + "\nGit 状态：" + file.Status)
				if a.workingFileKey == key {
					row.Background(ideaSelection)
				}
				row.OnClick(func() { a.selectWorkingFile(tree, file) }).ContextMenu(func(menu *ui.Menu) {
					a.changelistMoveMenu(menu, tree, []WorkingFile{file})
					if menu.Item("Revert 此文件…").Disabled(a.busy || file.Conflicted()).Chosen() {
						a.openChangesRevert([]CommitRequest{{Tree: tree, Files: []WorkingFile{file}}})
					}
				}).Children(func() {
					ui.Text(c, label).FontSize(12).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).TextColor(color)
				})
			})
			if open && len(node.Children) > 0 {
				a.workingNodes(c, tree, node.Children, depth+1)
			}
		})
	}
}
