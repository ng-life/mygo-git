package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *client) outgoingPanel(c *ui.Context) {
	ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Unpushed Commits").FontSize(14).Bold()
			ui.Spacer(c)
			ui.Button(c, "Push All…").Disabled(a.busy || a.workingLoading).OnClick(func() { a.openChangesPush(a.outgoing) })
		})
		if a.workingLoading {
			ui.Text(c, "Refreshing unpushed commits…").FontSize(11)
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
						a.workingDisclosure(c, "outgoing:"+source.Repository.Path, source.Repository.Name+" unpushed")
						state := ui.CheckOff
						if len(selected) == len(source.Commits) {
							state = ui.CheckOn
						} else if len(selected) > 0 {
							state = ui.CheckMixed
						}
						ui.CheckboxState(c.Key("outgoing-selected"), &state, "").Label("Select unpushed repository " + source.Repository.Name).OnClick(func() {
							if a.outgoingSelection == nil {
								a.outgoingSelection = map[string]bool{}
							}
							for _, commit := range source.Commits {
								a.outgoingSelection[commit.Key()] = state == ui.CheckOn
							}
						})
						treeContentAction(ui.ButtonBase(c.Key("outgoing-name")).Grow(1).MinWidth(0).Justify(ui.Start).Label("Unpushed repository "+source.Repository.Name).Tooltip(source.Repository.Path+"\n"+source.Ref), func() { a.selectRepository(source.Repository) }, func() {
							key := "outgoing:" + source.Repository.Path
							a.workingCollapsed[key] = !a.workingCollapsed[key]
						}).Children(func() {
							folderTypeIcon(c)
							ui.Textf(c, "%s · %s · %d", source.Repository.Name, strings.TrimPrefix(source.Ref, "refs/heads/"), len(source.Commits)).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
						})
						ui.Button(c, "Push…").Label("Push " + source.Repository.Name).Height(22).FontSize(11).Disabled(a.busy || source.Ref == "").OnClick(func() { a.openChangesPush([]Outgoing{source}) })
						ui.Button(c, "Squash…").Label("Squash Unpushed Commits " + source.Repository.Name).Height(22).FontSize(11).Disabled(a.busy || len(selected) < 2 || source.Ref == "").OnClick(func() { a.openChangesSquash(source) })
					})
					if !a.workingCollapsed["outgoing:"+source.Repository.Path] {
						ui.List(c.Key("outgoing-commits"), nil, len(source.Commits)).Height(float32(min(len(source.Commits), 10) * 25)).FillWidth().Shrink(0).ItemKey(func(i int) any { return source.Commits[len(source.Commits)-1-i].Key() }).Rows(func(item ui.ListRow) {
							a.outgoingCommitRow(item.Context, source, source.Commits[len(source.Commits)-1-item.Index], selected)
						})
					}
				})
			}
			if count == 0 && !a.workingLoading {
				ui.Text(c, "No unpushed commits").FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
		ui.Text(c, "Select commits to squash. Push sends all unpublished history on this branch").FontSize(11).TextColor(c.Theme().TextMuted)
	})
}
func (a *client) changesActionDialog(c *ui.Context) {
	if !a.changesDialog.Open {
		return
	}
	ui.Modal(c.Key("changes-action"), &a.changesDialog.Open, func() {
		ui.Column(c).Width(720).MaxHeight(720).Gap(10).Children(func() {
			kind := a.changesDialog.Kind
			title := map[string]string{"revert": "Revert Files", "push": "Push Branch", "commit-push": "Commit & Push", "amend": "Amend Last Commit", "squash": "Squash Unpushed Commits"}[kind]
			if kind == "commit-push" && len(a.changesDialog.Requests) > 0 && a.changesDialog.Requests[0].Amend {
				title = "Amend Commit and Push"
			}
			ui.Text(c, title).FontSize(18).Bold()
			if a.changesDialog.Error != "" {
				ui.Text(c, a.changesDialog.Error).FontSize(12)
			}
			if kind == "revert" {
				ui.Text(c, "Discard staged and unstaged changes in selected files and restore HEAD. Added and untracked files will be deleted. Git cannot recover discarded uncommitted content.").FontSize(12)
			}
			if kind == "squash" {
				ui.Text(c, "Squash unpublished linear history within one repository. Nonconsecutive selections are squashed first, then intervening and subsequent commits are replayed. The original branch is preserved on conflict. The final tree must match the original HEAD.").FontSize(12)
			}
			if kind == "amend" {
				ui.Text(c, "Edit the message of the latest unpublished commit in this repository. Other staged files are excluded. Published commits are not rewritten.").FontSize(12)
			}
			if kind == "push" || kind == "commit-push" {
				ui.Text(c, "Push all history on the current branch without force. Configure the remote and target branch per repository. Local commits are preserved if push fails.").FontSize(12)
			}
			ui.Scroll(c).MaxHeight(280).Gap(6).Children(func() {
				for i := range a.changesDialog.PushTargets {
					target := &a.changesDialog.PushTargets[i]
					ui.Column(c.Key(target.Source.Repository.Path)).Gap(4).Children(func() {
						ui.Textf(c, "%s · %s", target.Source.Repository.Name, strings.TrimPrefix(target.Source.Ref, "refs/heads/")).Bold().SingleLine()
						ui.Row(c).Gap(8).Children(func() {
							ui.TextInput(c.Key("remote"), &target.Remote).Label("Push remote " + target.Source.Repository.Name).Width(130).Placeholder("origin")
							ui.TextInput(c.Key("destination"), &target.Destination).Label("Push Branch " + target.Source.Repository.Name).Grow(1).Placeholder("Target branch")
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
				ui.TextArea(c.Key("changes-message"), &a.changesDialog.Message).Label("Changes operation message").Placeholder("Commit message").Height(110)
			}
			disabled := a.busy || ((kind == "squash" || kind == "amend" || kind == "commit-push") && strings.TrimSpace(a.changesDialog.Message) == "")
			for _, target := range a.changesDialog.PushTargets {
				if strings.TrimSpace(target.Remote) == "" || strings.TrimSpace(target.Destination) == "" || target.Source.Ref == "" {
					disabled = true
				}
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { a.changesDialog.Open = false })
				ui.PrimaryButton(c, fmt.Sprintf("Confirm %s", title)).Disabled(disabled).OnClick(a.applyChangesDialog)
			})
		})
	})
}

func (a *client) outgoingCommitRow(c *ui.Context, source Outgoing, commit Commit, selected []Commit) {
	ui.Row(c.Key(commit.Hash)).Padding(0, 0, 0, 18).Gap(4).Height(25).AlignItems(ui.Center).Children(func() {
		on := a.outgoingSelection[commit.Key()]
		ui.Checkbox(c.Key("commit-selected"), &on, "").Label("Select unpushed commit " + source.Repository.Name + "/" + shortHash(commit.Hash)).OnClick(func() {
			if a.outgoingSelection == nil {
				a.outgoingSelection = map[string]bool{}
			}
			a.outgoingSelection[commit.Key()] = on
		})
		ui.ButtonBase(c.Key("commit-content")).Grow(1).MinWidth(0).Justify(ui.Start).Gap(6).Label("Unpushed commit " + source.Repository.Name + "/" + shortHash(commit.Hash)).Tooltip(commit.Hash + "\n" + commit.Author + "\n" + commit.Subject).OnClick(func() { a.selectCommit(commit) }).ContextMenu(func(menu *ui.Menu) {
			if menu.Item("Show Commit").Chosen() {
				a.selectCommit(commit)
			}
			if menu.Item("Amend Last Commit…").Disabled(a.busy || commit.Hash != source.Head).Chosen() {
				a.openChangesAmend(source)
			}
			if menu.Item("Squash Selected Unpushed Commits…").Disabled(a.busy || len(selected) < 2).Chosen() {
				a.openChangesSquash(source)
			}
			if menu.Item("Push Current Branch…").Disabled(a.busy || source.Ref == "").Chosen() {
				a.openChangesPush([]Outgoing{source})
			}
		}).Children(func() {
			ui.Text(c, shortHash(commit.Hash)).Font("monospace").FontSize(11).TextColor(statusBlue)
			ui.Text(c, commit.Subject).FontSize(12).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		})
	})
}
