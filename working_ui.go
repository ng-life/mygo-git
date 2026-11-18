package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *client) workingChangesPanel(c *ui.Context) {
	if a.changeViewMode != "" {
		a.scopedChangesPanel(c)
		return
	}
	a.ensureChangelists()
	t := c.Theme()
	ui.Column(c).Fill().Padding(6).Gap(4).Children(func() {
		a.changeScopeTabs(c)
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Changelists").Label("Changelists Heading").FontSize(13).Bold()
			ui.Button(c.Key("new-changelist"), "+").Height(26).Label("New Changelist").Tooltip("New Changelist").Disabled(a.busy).OnClick(func() { a.changelistDialog = changelistDialogState{Open: true, Kind: "create", Activate: true} })
			ui.Spacer(c)
			ui.Button(c.Key("refresh-working"), "Refresh Changes").Height(26).FontSize(11).Disabled(a.busy || a.workingLoading).OnClick(a.reloadWorkingTrees)
		})
		if a.changelistsLoadError != "" {
			ui.Text(c, a.changelistsLoadError).FontSize(11).TextColor(statusOrange)
		}
		if a.changelistsError != "" {
			ui.Text(c, a.changelistsError).FontSize(11).TextColor(statusOrange)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Active: "+a.changelists.list(a.changelists.Active).Name).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).TextColor(t.TextMuted)
			ui.Button(c.Key("collapse-working"), "Collapse").Height(22).FontSize(11).Tooltip("Collapse All Working Tree Directories").OnClick(func() { a.setWorkingCollapsed(true) })
			ui.Button(c.Key("expand-working"), "Expand").Height(22).FontSize(11).Tooltip("Expand All Working Tree Directories").OnClick(func() { a.setWorkingCollapsed(false) })
			ui.Spacer(c)
			ui.Button(c, "Move Selected…").Label("Move Selected to Changelist").Height(22).FontSize(11).Disabled(a.busy || a.workingLoading || len(a.workingRequests("")) == 0).OnClick(func() {
				a.changelistDialog = changelistDialogState{Open: true, Kind: "move", Target: a.changelists.Selected, Requests: a.workingRequests("")}
			})
		})
		if a.workingLoading {
			ui.Text(c, "Loading working tree changes…").FontSize(12).TextColor(t.TextMuted)
		}
		ui.Scroll(c).Grow(1).Gap(4).Children(func() {
			changed := 0
			for _, tree := range a.workingTrees {
				if len(tree.Files) > 0 {
					changed++
				}
				if tree.Error != "" {
					ui.Text(c, tree.Repository.Name+": "+tree.Error).FontSize(11)
				}
			}
			if changed == 0 && !a.workingLoading {
				ui.Text(c, "No working tree changes").TextColor(t.TextMuted)
			}
			for _, list := range a.changelists.Lists {
				a.changelistGroup(c, list)
			}
		})
		a.workingChangeSummary(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Commit Message").FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, a.changelists.list(a.changelists.Selected).Name).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).TextColor(t.TextMuted)
			ui.Checkbox(c.Key("working-amend"), &a.workingAmend, "Amend").Disabled(a.busy).Tooltip("Include selected files and amend the latest unpublished commit").OnClick(a.loadAmendMessage)
			ui.Button(c.Key("message-history"), "History").Label("Commit Message History").Height(22).FontSize(11).Tooltip("Commit Message History · Ctrl+M").Disabled(a.busy).OnClick(a.openCommitMessageHistory).OnShortcut(ui.Ctrl, ui.KeyM, a.openCommitMessageHistory)
			ui.Spacer(c)
			ui.Button(c, "Revert…").Label("Revert Selected Files").Height(22).FontSize(11).Disabled(a.busy || a.workingLoading || len(a.workingRequests("")) == 0).OnClick(func() { a.openChangesRevert(a.workingRequests("")) })
		})
		ui.TextArea(c.Key("commit-message"), &a.commitMessage).Bind(&a.commitMessageHandle).Height(100).Background(ideaEditor).Placeholder("Commit message").Label("Commit message").Disabled(a.busy).OnChange(a.persistChangelists)
		count := len(a.workingRequests(""))
		commitLabel, pushLabel := commitButtonLabels(a.workingAmend)
		ui.Row(c).Gap(6).Children(func() {
			ui.PrimaryButton(c.Key("commit-selected"), commitLabel).Shrink(0).Label("Commit selected repositories").Disabled(a.busy || a.workingLoading || count == 0 || strings.TrimSpace(a.commitMessage) == "").OnClick(func() { a.commitWorking("") })
			ui.Button(c.Key("commit-push-selected"), pushLabel).Shrink(0).Label("Commit and push selected repositories").Disabled(a.busy || a.workingLoading || count == 0 || strings.TrimSpace(a.commitMessage) == "").OnClick(func() { a.openCommitPush("") })
		})
		ui.Text(c, "Commit to the current branch. Selected files are staged and committed using their full working tree content").FontSize(11).TextColor(t.TextMuted)
	})
}
func (a *client) changelistRepository(c *ui.Context, tree WorkingTree) {
	t := c.Theme()
	scope := a.workingTreeScope(tree)
	commitLabel, _ := commitButtonLabels(a.workingAmend)
	ui.Column(c.Key(tree.Repository.Path)).Padding(0, 0, 0, 12).Gap(0).Children(func() {
		selected := 0
		for _, file := range tree.Files {
			if a.workingSelection[workingFileKey(tree.Repository.Path, file.Path)] {
				selected++
			}
		}
		ui.Row(c).Gap(4).Height(22).AlignItems(ui.Center).Children(func() {
			a.workingDisclosure(c, scope, tree.Repository.Name)
			state := a.workingCheckState(tree.Repository.Path, tree.Files)
			ui.CheckboxState(c.Key("repo-selected"), &state, "").Label("Select repository " + tree.Repository.Name).
				Disabled(a.busy || a.workingLoading || tree.Error != "" || len(tree.Files) == 0).Tooltip(tree.Repository.Name + "\n" + tree.Repository.Path).
				OnClick(func() { a.toggleWorkingRepository(tree, state == ui.CheckOn) })
			treeContentAction(ui.ButtonBase(c.Key("repository-name")).Grow(1).MinWidth(0).Justify(ui.Start).Label("Repository name "+tree.Repository.Name).Tooltip(tree.Repository.Name+"\n"+tree.Repository.Path).Disabled(a.busy || a.workingLoading || tree.Error != ""), func() { a.selectRepository(tree.Repository) }, func() { a.workingCollapsed[scope] = !a.workingCollapsed[scope] }).ContextMenu(func(menu *ui.Menu) { a.changelistMoveMenu(menu, tree, tree.Files) }).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
					folderTypeIcon(c)
					ui.Text(c, tree.Repository.Name).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					ui.Textf(c, "%d files", len(tree.Files)).FontSize(11).Shrink(0).TextColor(t.TextMuted)
					ui.Text(c, tree.Repository.Branch).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).TextColor(t.TextMuted).Tooltip(tree.Repository.Branch)
					ui.Spacer(c)
				})
			})
			ui.Button(c.Key("commit-one"), commitLabel).Height(22).FontSize(11).Label("Commit " + tree.Repository.Name).
				Disabled(a.busy || a.workingLoading || selected == 0 || strings.TrimSpace(a.commitMessage) == "" || tree.Error != "").
				OnClick(func() { a.commitChangelistRepository(tree, false) }).ContextMenu(func(menu *ui.Menu) {
				_, pushLabel := commitButtonLabels(a.workingAmend)
				if menu.Item(pushLabel).Disabled(a.busy || selected == 0).Chosen() {
					a.commitChangelistRepository(tree, true)
				}
			})
		})
		if tree.Error != "" {
			ui.Text(c, tree.Error).FontSize(11)
		}

		if !a.workingCollapsed[scope] {
			a.workingNodes(c, tree, workingHierarchy(tree.Files).Children, 1)
		}

		for _, result := range a.workingResults {
			if result.Repository.Path == tree.Repository.Path {
				state := "Last commit succeeded"
				if result.Err != nil {
					state = "Last commit failed: " + result.Err.Error()
					if strings.Contains(result.Err.Error(), "Commit succeeded") {
						state = result.Err.Error()
					}
					if isUncertainCommit(result.Err) {
						state = result.Err.Error()
					}
				}
				ui.Text(c, state).FontSize(11).Tooltip(result.Output)
			}
		}
	})
}
