package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *client) changelistGroup(c *ui.Context, list Changelist) {
	trees := a.changelistTrees(list.ID)
	total := 0
	selected := 0
	selectable := 0
	for _, tree := range trees {
		for _, file := range tree.Files {
			total++
			if !file.Conflicted() {
				selectable++
			}
			if a.workingSelection[workingFileKey(tree.Repository.Path, file.Path)] {
				selected++
			}
		}
	}
	ui.Column(c.Key("changelist:" + list.ID)).Gap(0).Children(func() {
		key := "changelist:" + list.ID
		ui.Row(c).Gap(4).Height(22).AlignItems(ui.Center).Children(func() {
			a.workingDisclosure(c, key, list.Name)
			state := ui.CheckOff
			if selected > 0 {
				state = ui.CheckMixed
				if selected == selectable {
					state = ui.CheckOn
				}
			}
			ui.CheckboxState(c.Key("select-list"), &state, "").Label("Select changelist " + list.Name).Disabled(a.busy || a.workingLoading || selectable == 0).OnClick(func() {
				for _, tree := range trees {
					a.toggleWorkingFiles(tree.Repository.Path, tree.Files, state == ui.CheckOn)
				}
			})
			label := list.Name
			if list.ID == a.changelists.Active {
				label += " · Active"
			}
			row := treeContentAction(ui.ButtonBase(c.Key("list-name")).Grow(1).MinWidth(0).Justify(ui.Start).Label("Changelist "+list.Name).Tooltip("Click to select; double-click to expand or collapse; right-click to manage").Disabled(a.busy), func() { a.selectChangelist(list.ID) }, func() { a.workingCollapsed[key] = !a.workingCollapsed[key] })
			if list.ID == a.changelists.Selected {
				row.Background(ideaSelection)
			}
			row.ContextMenu(func(menu *ui.Menu) {
				if menu.Item("Set Active Changelist").Disabled(a.busy || list.ID == a.changelists.Active).Chosen() {
					a.activateChangelist(list.ID)
				}
				if menu.Item("Rename Changelist…").Disabled(a.busy).Chosen() {
					a.changelistDialog = changelistDialogState{Open: true, Kind: "rename", ID: list.ID, Name: list.Name}
				}
				if menu.Item("Delete Changelist…").Disabled(a.busy || list.ID == defaultChangelist).Chosen() {
					a.changelistDialog = changelistDialogState{Open: true, Kind: "delete", ID: list.ID, Name: list.Name}
				}
				if menu.Item("Revert This Changelist…").Disabled(a.busy || total == 0).Chosen() {
					a.openChangesRevert(changelistRequests(trees))
				}
			}).Children(func() {
				ui.Text(c, label).FontSize(12).Bold().SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				ui.Textf(c, "%d files", total).FontSize(11).TextColor(c.Theme().TextMuted).Shrink(0)
			})
		})
		if !a.workingCollapsed[key] {
			if total == 0 {
				ui.Text(c, "No changes").FontSize(11).TextColor(c.Theme().TextMuted).Padding(3, 0, 3, 42)
			}
			for _, tree := range trees {
				a.changelistRepository(c, tree)
			}
		}
	})
}
func changelistRequests(trees []WorkingTree) []CommitRequest {
	requests := make([]CommitRequest, 0, len(trees))
	for _, tree := range trees {
		requests = append(requests, CommitRequest{Tree: tree, Files: append([]WorkingFile(nil), tree.Files...)})
	}
	return requests
}
func (a *client) changelistMoveMenu(menu *ui.Menu, tree WorkingTree, files []WorkingFile) {
	for _, list := range a.changelists.Lists {
		all := true
		for _, file := range files {
			if a.fileChangelist(tree, file) != list.ID {
				all = false
				break
			}
		}
		if menu.Item("Move to Changelist:" + list.Name).Disabled(a.busy || a.workingLoading || all).Chosen() {
			a.moveToChangelist([]CommitRequest{{Tree: tree, Files: files}}, list.ID)
		}
	}
	if menu.Item("Move to New Changelist…").Disabled(a.busy || a.workingLoading).Chosen() {
		a.changelistDialog = changelistDialogState{Open: true, Kind: "create", Activate: true, Requests: []CommitRequest{{Tree: tree, Files: files}}}
	}
}
func (a *client) changelistManagementPanel(c *ui.Context) {
	if !a.changelistDialog.Open {
		return
	}
	ui.Modal(c.Key("changelist-management"), &a.changelistDialog.Open, func() {
		ui.Column(c).Width(460).MaxHeight(540).Gap(10).Children(func() {
			kind := a.changelistDialog.Kind
			title := map[string]string{"create": "New Changelist", "rename": "Rename Changelist", "delete": "Delete Changelist", "move": "Move Changes"}[kind]
			ui.Text(c, title).FontSize(18).Bold()
			if kind == "create" || kind == "rename" {
				ui.TextInput(c.Key("list-name"), &a.changelistDialog.Name).Label("Changelist name").Placeholder("e.g. SCM-N31137 Optimize automatic wave dispatch")
				if kind == "create" {
					ui.Checkbox(c, &a.changelistDialog.Activate, "Set Active Changelist")
				}
			}
			if kind == "delete" {
				ui.Text(c, a.changelistDialog.Name).Bold()
				ui.Text(c, "Move changes back to "+a.changelists.list(defaultChangelist).Name+". Delete this list's message draft; file contents and the Git index are preserved.").FontSize(12)
			}
			if kind == "move" {
				total := 0
				for _, request := range a.changelistDialog.Requests {
					total += len(request.Files)
				}
				ui.Text(c, fmt.Sprintf("Move %d files to:", total)).FontSize(12)
				ui.Scroll(c).MaxHeight(280).Children(func() {
					for _, list := range a.changelists.Lists {
						on := a.changelistDialog.Target == list.ID
						ui.Checkbox(c.Key(list.ID), &on, list.Name).Label("Target changelist " + list.Name).OnClick(func() { a.changelistDialog.Target = list.ID })
					}
				})
			}
			if a.changelistDialog.Error != "" {
				ui.Text(c, a.changelistDialog.Error).TextColor(statusOrange).FontSize(12)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { a.changelistDialog.Open = false })
				ui.PrimaryButton(c, "Confirm "+title).Disabled(a.busy).OnClick(a.applyChangelistDialog)
			})
		})
	})
}
