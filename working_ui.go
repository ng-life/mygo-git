package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *client) workingChangesPanel(c *ui.Context) {
	a.ensureChangelists()
	t := c.Theme()
	ui.Column(c).Fill().Padding(6).Gap(4).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Changelists").FontSize(13).Bold()
			ui.Button(c.Key("new-changelist"), "+").Height(26).Label("新建 Changelist").Tooltip("新建 Changelist").Disabled(a.busy).OnClick(func() { a.changelistDialog = changelistDialogState{Open: true, Kind: "create", Activate: true} })
			ui.Spacer(c)
			ui.Button(c.Key("refresh-working"), "刷新变更").Height(26).FontSize(11).Disabled(a.busy || a.workingLoading).OnClick(a.reloadWorkingTrees)
		})
		if a.changelistsLoadError != "" {
			ui.Text(c, a.changelistsLoadError).FontSize(11).TextColor(statusOrange)
		}
		if a.changelistsError != "" {
			ui.Text(c, a.changelistsError).FontSize(11).TextColor(statusOrange)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "活动："+a.changelists.list(a.changelists.Active).Name).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).TextColor(t.TextMuted)
			ui.Button(c.Key("collapse-working"), "折叠").Height(22).FontSize(11).Tooltip("折叠全部工作区目录").OnClick(func() { a.setWorkingCollapsed(true) })
			ui.Button(c.Key("expand-working"), "展开").Height(22).FontSize(11).Tooltip("展开全部工作区目录").OnClick(func() { a.setWorkingCollapsed(false) })
			ui.Spacer(c)
			ui.Button(c, "移动所选…").Label("移动所选到 Changelist").Height(22).FontSize(11).Disabled(a.busy || a.workingLoading || len(a.workingRequests("")) == 0).OnClick(func() {
				a.changelistDialog = changelistDialogState{Open: true, Kind: "move", Target: a.changelists.Selected, Requests: a.workingRequests("")}
			})
		})
		if a.workingLoading {
			ui.Text(c, "正在读取工作区变更…").FontSize(12).TextColor(t.TextMuted)
		}
		ui.Scroll(c).Grow(1).Gap(4).Children(func() {
			changed := 0
			for _, tree := range a.workingTrees {
				if len(tree.Files) > 0 {
					changed++
				}
				if tree.Error != "" {
					ui.Text(c, tree.Repository.Name+"："+tree.Error).FontSize(11)
				}
			}
			if changed == 0 && !a.workingLoading {
				ui.Text(c, "暂无工作区变更").TextColor(t.TextMuted)
			}
			for _, list := range a.changelists.Lists {
				a.changelistGroup(c, list)
			}
		})
		a.workingChangeSummary(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Commit Message").FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, a.changelists.list(a.changelists.Selected).Name).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).TextColor(t.TextMuted)
			ui.Checkbox(c.Key("working-amend"), &a.workingAmend, "Amend").Disabled(a.busy).Tooltip("补充所选文件并修改最新未发布提交").OnClick(a.loadAmendMessage)
			ui.Button(c.Key("message-history"), "历史").Label("Commit Message History").Height(22).FontSize(11).Tooltip("Commit Message History · Ctrl+M").Disabled(a.busy).OnClick(a.openCommitMessageHistory).OnShortcut(ui.Ctrl, ui.KeyM, a.openCommitMessageHistory)
			ui.Spacer(c)
			ui.Button(c, "Revert…").Label("Revert 已选文件").Height(22).FontSize(11).Disabled(a.busy || a.workingLoading || len(a.workingRequests("")) == 0).OnClick(func() { a.openChangesRevert(a.workingRequests("")) })
		})
		ui.TextArea(c.Key("commit-message"), &a.commitMessage).Bind(&a.commitMessageHandle).Height(100).Background(ideaEditor).Placeholder("Commit message").Label("Commit message").Disabled(a.busy).OnChange(a.persistChangelists)
		count := len(a.workingRequests(""))
		commitLabel, pushLabel := commitButtonLabels(a.workingAmend)
		ui.Row(c).Gap(6).Children(func() {
			ui.PrimaryButton(c.Key("commit-selected"), commitLabel).Shrink(0).Label("Commit selected repositories").Disabled(a.busy || a.workingLoading || count == 0 || strings.TrimSpace(a.commitMessage) == "").OnClick(func() { a.commitWorking("") })
			ui.Button(c.Key("commit-push-selected"), pushLabel).Shrink(0).Label("Commit and push selected repositories").Disabled(a.busy || a.workingLoading || count == 0 || strings.TrimSpace(a.commitMessage) == "").OnClick(func() { a.openCommitPush("") })
		})
		ui.Text(c, "提交当前工作分支；勾选文件按工作区完整内容暂存并提交").FontSize(11).TextColor(t.TextMuted)
	})
}
func (a *client) workingComparisonPanel(c *ui.Context) {
	ui.Column(c.Key(a.workingFileKey)).Fill().Padding(0).Gap(4).Children(func() {
		tree, file := a.workingFileTree, a.workingFile
		ui.Text(c, tree.Repository.Name+" · "+tree.Repository.Branch+" · "+file.Label()).FontSize(12).SingleLine().Ellipsis("…").Padding(5, 8).FillWidth().Background(ideaToolbar).Tooltip(file.Label())
		a.reviewFileNavigation(c)
		if a.workingComparisonLoading {
			ui.Text(c, "正在读取工作区文件…")
			return
		}
		if a.workingComparisonError != "" {
			ui.Text(c, a.workingComparisonError)
			return
		}
		if a.workingComparison.Notice != "" {
			ui.Text(c, a.workingComparison.Notice)
			return
		}
		before := "HEAD · " + shortHash(tree.Head)
		if tree.Head == "" {
			before = "空树"
		}
		reviewComparison(c, a.workingComparison, before, "当前文件", a)
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
			ui.CheckboxState(c.Key("repo-selected"), &state, "").Label("选择仓库 " + tree.Repository.Name).
				Disabled(a.busy || a.workingLoading || tree.Error != "" || len(tree.Files) == 0).Tooltip(tree.Repository.Name + "\n" + tree.Repository.Path).
				OnClick(func() { a.toggleWorkingRepository(tree, state == ui.CheckOn) })
			treeContentAction(ui.ButtonBase(c.Key("repository-name")).Grow(1).MinWidth(0).Justify(ui.Start).Label("仓库名 "+tree.Repository.Name).Tooltip(tree.Repository.Name+"\n"+tree.Repository.Path).Disabled(a.busy || a.workingLoading || tree.Error != ""), func() { a.selectRepository(tree.Repository) }, func() { a.workingCollapsed[scope] = !a.workingCollapsed[scope] }).ContextMenu(func(menu *ui.Menu) { a.changelistMoveMenu(menu, tree, tree.Files) }).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, workingFolderIcon).Width(14).Height(14).Shrink(0).TextColor(t.TextMuted)
					ui.Text(c, tree.Repository.Name).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					ui.Textf(c, "%d 文件", len(tree.Files)).FontSize(11).Shrink(0).TextColor(t.TextMuted)
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
				state := "上次 Commit 成功"
				if result.Err != nil {
					state = "上次 Commit 失败：" + result.Err.Error()
					if strings.Contains(result.Err.Error(), "Commit 已成功") {
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
