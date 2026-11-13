package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *client) outgoingPanel(c *ui.Context) {
	ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "未 Push 提交").FontSize(14).Bold()
			ui.Spacer(c)
			ui.Button(c, "Push 全部…").Disabled(a.busy || a.workingLoading).OnClick(func() { a.openChangesPush(a.outgoing) })
		})
		if a.workingLoading {
			ui.Text(c, "正在刷新未 Push 提交…").FontSize(11)
		}
		ui.Scroll(c).Grow(1).Children(func() {
			count := 0
			for _, source := range a.outgoing {
				if source.Error != "" {
					ui.Text(c, source.Repository.Name+": "+source.Error).FontSize(11)
				}
				if len(source.Commits) == 0 {
					continue
				}
				count++
				ui.Column(c.Key(source.Repository.Path)).Children(func() {
					selected := a.selectedOutgoing(source)
					ui.Row(c).Gap(4).Height(26).AlignItems(ui.Center).Children(func() {
						a.workingDisclosure(c, "outgoing:"+source.Repository.Path, source.Repository.Name+" 未 Push")
						state := ui.CheckOff
						if len(selected) == len(source.Commits) {
							state = ui.CheckOn
						} else if len(selected) > 0 {
							state = ui.CheckMixed
						}
						ui.CheckboxState(c.Key("outgoing-selected"), &state, "").Label("选择未 Push 仓库 " + source.Repository.Name).OnClick(func() {
							if a.outgoingSelection == nil {
								a.outgoingSelection = map[string]bool{}
							}
							for _, commit := range source.Commits {
								a.outgoingSelection[commit.Key()] = state == ui.CheckOn
							}
						})
						treeContentAction(ui.ButtonBase(c.Key("outgoing-name")).Grow(1).MinWidth(0).Justify(ui.Start).Label("未 Push 仓库 "+source.Repository.Name).Tooltip(source.Repository.Path+"\n"+source.Ref), func() { a.selectRepository(source.Repository) }, func() {
							key := "outgoing:" + source.Repository.Path
							a.workingCollapsed[key] = !a.workingCollapsed[key]
						}).Children(func() {
							ui.Textf(c, "%s · %s · %d", source.Repository.Name, strings.TrimPrefix(source.Ref, "refs/heads/"), len(source.Commits)).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
						})
						ui.Button(c, "Push…").Label("Push " + source.Repository.Name).Height(22).FontSize(11).Disabled(a.busy || source.Ref == "").OnClick(func() { a.openChangesPush([]Outgoing{source}) })
						ui.Button(c, "合并…").Label("合并未 Push 提交 " + source.Repository.Name).Height(22).FontSize(11).Disabled(a.busy || len(selected) < 2 || source.Ref == "").OnClick(func() { a.openChangesSquash(source) })
					})
					if !a.workingCollapsed["outgoing:"+source.Repository.Path] {
						ui.List(c.Key("outgoing-commits"), nil, len(source.Commits)).Height(float32(min(len(source.Commits), 10) * 25)).FillWidth().Shrink(0).ItemKey(func(i int) any { return source.Commits[len(source.Commits)-1-i].Key() }).Rows(func(item ui.ListRow) {
							a.outgoingCommitRow(item.Context, source, source.Commits[len(source.Commits)-1-item.Index], selected)
						})
					}
				})
			}
			if count == 0 && !a.workingLoading {
				ui.Text(c, "暂无未 Push 提交").FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
		ui.Text(c, "勾选用于合并提交；Push 推送该分支全部未发布历史").FontSize(11).TextColor(c.Theme().TextMuted)
	})
}
func (a *client) changesActionDialog(c *ui.Context) {
	if !a.changesDialog.Open {
		return
	}
	ui.Modal(c.Key("changes-action"), &a.changesDialog.Open, func() {
		ui.Column(c).Width(720).MaxHeight(720).Gap(10).Children(func() {
			kind := a.changesDialog.Kind
			title := map[string]string{"revert": "Revert 文件", "push": "Push 分支", "commit-push": "Commit & Push", "amend": "Amend 最新提交", "squash": "合并未 Push 提交"}[kind]
			if kind == "commit-push" && len(a.changesDialog.Requests) > 0 && a.changesDialog.Requests[0].Amend {
				title = "Amend Commit and Push"
			}
			ui.Text(c, title).FontSize(18).Bold()
			if a.changesDialog.Error != "" {
				ui.Text(c, a.changesDialog.Error).FontSize(12)
			}
			if kind == "revert" {
				ui.Text(c, "丢弃所选文件的已暂存与未暂存修改，恢复 HEAD；新增及未跟踪文件将被删除。此操作不能通过 Git 找回未提交内容。").FontSize(12)
			}
			if kind == "squash" {
				ui.Text(c, "仅处理同仓库未发布的线性历史。不连续选择会先合并所选提交，再重放其间及后续提交；冲突时保留原分支。最终文件树必须与原 HEAD 一致。").FontSize(12)
			}
			if kind == "amend" {
				ui.Text(c, "只修改该仓库最新未发布提交的 message，不带入其他暂存文件；已发布提交不会改写。").FontSize(12)
			}
			if kind == "push" || kind == "commit-push" {
				ui.Text(c, "推送当前分支全部历史，不强制覆盖远程分支。可分别设置各仓库的 remote 和目标分支；Commit 已成功但 Push 失败时，本地提交保留。").FontSize(12)
			}
			ui.Scroll(c).MaxHeight(280).Gap(6).Children(func() {
				for i := range a.changesDialog.PushTargets {
					target := &a.changesDialog.PushTargets[i]
					ui.Column(c.Key(target.Source.Repository.Path)).Gap(4).Children(func() {
						ui.Textf(c, "%s · %s", target.Source.Repository.Name, strings.TrimPrefix(target.Source.Ref, "refs/heads/")).Bold().SingleLine()
						ui.Row(c).Gap(8).Children(func() {
							ui.TextInput(c.Key("remote"), &target.Remote).Label("Push remote " + target.Source.Repository.Name).Width(130).Placeholder("origin")
							ui.TextInput(c.Key("destination"), &target.Destination).Label("Push 分支 " + target.Source.Repository.Name).Grow(1).Placeholder("目标分支")
						})
					})
				}
				if kind == "revert" {
					for _, request := range a.changesDialog.Requests {
						ui.Text(c, request.Tree.Repository.Name).Bold()
						for _, file := range request.Files {
							ui.Text(c, file.Label()).FontSize(12).SingleLine().Tooltip(file.Label())
						}
					}
				}
				if kind == "squash" {
					ui.Text(c, a.changesDialog.Source.Repository.Name).Bold()
					for _, commit := range a.changesDialog.Commits {
						ui.Textf(c, "%s · %s", shortHash(commit.Hash), commit.Subject).FontSize(12).SingleLine()
					}
				}
				if kind == "amend" {
					ui.Textf(c, "%s · %s", a.changesDialog.Source.Repository.Name, shortHash(a.changesDialog.Source.Head)).FontSize(12)
				}
			})
			if kind == "squash" || kind == "amend" || kind == "commit-push" {
				ui.TextArea(c.Key("changes-message"), &a.changesDialog.Message).Label("Changes 操作 message").Placeholder("Commit message").Height(110)
			}
			disabled := a.busy || ((kind == "squash" || kind == "amend" || kind == "commit-push") && strings.TrimSpace(a.changesDialog.Message) == "")
			for _, target := range a.changesDialog.PushTargets {
				if strings.TrimSpace(target.Remote) == "" || strings.TrimSpace(target.Destination) == "" || target.Source.Ref == "" {
					disabled = true
				}
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(func() { a.changesDialog.Open = false })
				ui.PrimaryButton(c, fmt.Sprintf("确认%s", title)).Disabled(disabled).OnClick(a.applyChangesDialog)
			})
		})
	})
}

func (a *client) outgoingCommitRow(c *ui.Context, source Outgoing, commit Commit, selected []Commit) {
	ui.Row(c.Key(commit.Hash)).Padding(0, 0, 0, 18).Gap(4).Height(25).AlignItems(ui.Center).Children(func() {
		on := a.outgoingSelection[commit.Key()]
		ui.Checkbox(c.Key("commit-selected"), &on, "").Label("选择未 Push Commit " + source.Repository.Name + "/" + shortHash(commit.Hash)).OnClick(func() {
			if a.outgoingSelection == nil {
				a.outgoingSelection = map[string]bool{}
			}
			a.outgoingSelection[commit.Key()] = on
		})
		ui.ButtonBase(c.Key("commit-content")).Grow(1).MinWidth(0).Justify(ui.Start).Gap(6).Label("未 Push Commit " + source.Repository.Name + "/" + shortHash(commit.Hash)).Tooltip(commit.Hash + "\n" + commit.Author + "\n" + commit.Subject).OnClick(func() { a.selectCommit(commit) }).ContextMenu(func(menu *ui.Menu) {
			if menu.Item("查看提交").Chosen() {
				a.selectCommit(commit)
			}
			if menu.Item("Amend 最新提交…").Disabled(a.busy || commit.Hash != source.Head).Chosen() {
				a.openChangesAmend(source)
			}
			if menu.Item("合并所选未 Push 提交…").Disabled(a.busy || len(selected) < 2).Chosen() {
				a.openChangesSquash(source)
			}
			if menu.Item("Push 当前分支…").Disabled(a.busy || source.Ref == "").Chosen() {
				a.openChangesPush([]Outgoing{source})
			}
		}).Children(func() {
			ui.Text(c, shortHash(commit.Hash)).Font("monospace").FontSize(11).TextColor(statusBlue)
			ui.Text(c, commit.Subject).FontSize(12).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		})
	})
}
