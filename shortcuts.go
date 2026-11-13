package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

func primaryShortcutModifier() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Super
	}
	return ui.Ctrl
}
func (a *client) shortcutModalOpen() bool {
	return a.taskPanelOpen || a.shortcutHelpOpen || a.review.Dialog || a.settings.Open || a.tagDialog.Open || a.changesDialog.Open || a.changelistDialog.Open || a.messageHistory.Open || a.refDialog.Open || a.codeNavigation.Open || a.branchDeletion.Open
}
func (a *client) registerShortcuts(c *ui.Context) {
	if a.shortcutModalOpen() {
		return
	}
	mod := primaryShortcutModifier()
	for i, key := range []ui.Key{ui.Key0, ui.Key1, ui.Key2} {
		c.OnWindowShortcut(mod, key, func() {
			if a.tab != i {
				a.switchTab(i)
			}
		})
	}
	c.OnWindowShortcut(mod, ui.KeyR, func() {
		if a.busy {
			return
		}
		if a.review.Open {
			if a.compare.Active {
				a.beginReview(a.compare.Request)
			} else {
				a.beginReview(a.review.Request)
			}
			return
		}
		if a.tab == 0 {
			for _, repo := range a.repos {
				if repo.Path == a.repositoryPath {
					a.repositoryAction(repo, "刷新状态")
					return
				}
			}
			a.refresh()
		} else if a.tab == 1 {
			a.reloadWorkingTrees()
		} else {
			a.reloadHistory()
		}
	})
	c.OnWindowShortcut(mod|ui.Shift, ui.KeyR, func() {
		if !a.busy {
			a.refresh()
		}
	})
	c.OnWindowShortcut(mod, ui.KeyF, func() {
		if a.compare.Active {
			if a.compare.Mode == "files" {
				a.compareSearchHandle.Focus(c)
			} else if a.compare.Mode != "bases" {
				a.compareCommitSearchHandle.Focus(c)
			}
		} else if a.tab == 0 {
			a.repositorySearchHandle.Focus(c)
		} else if a.tab == 2 {
			a.logsSearchHandle.Focus(c)
		} else {
			a.logSearchHandle.Focus(c)
		}
	})
	if !a.review.Open {
		c.OnWindowShortcut(mod, ui.KeyL, func() { a.logSearchHandle.Focus(c) })
	}
	c.OnWindowShortcut(mod, ui.KeyK, func() {
		if a.busy {
			return
		}
		if a.tab != 1 {
			a.switchTab(1)
		}
		a.commitMessageHandle.Focus(c)
	})
	c.OnWindowShortcut(mod, ui.KeyT, func() {
		if a.busy {
			return
		}
		repos := a.visible()
		if selected := a.targets(); len(selected) > 0 {
			repos = selected
		}
		a.runBranchAction("Pull", "当前分支", false, append([]Repository(nil), repos...))
	})
	if a.tab == 1 {
		c.OnWindowShortcut(mod|ui.Shift, ui.KeyK, func() {
			if !a.busy && !a.workingLoading {
				a.openChangesPush(a.outgoing)
			}
		})
	}
	c.OnWindowShortcut(mod, ui.KeyEnter, func() {
		if a.tab == 1 && !a.busy {
			a.commitWorking("")
		}
	})
	c.OnWindowShortcut(mod|ui.Shift, ui.KeyEnter, func() {
		if a.tab == 1 && !a.busy {
			a.openCommitPush("")
		}
	})
}
func (a *client) shortcutHelpPanel(c *ui.Context) {
	if !a.shortcutHelpOpen {
		return
	}
	ui.Modal(c.Key("shortcut-help"), &a.shortcutHelpOpen, func() {
		ui.Column(c).Width(520).Gap(8).Children(func() {
			ui.Text(c, "快捷键").FontSize(16).Bold()
			prefix := "Ctrl"
			if runtime.GOOS == "darwin" {
				prefix = "⌘"
			}
			for _, row := range []struct{ Key, Action string }{{"0", "Repositories"}, {"1", "Changes"}, {"2", "Logs"}, {"R", "刷新当前视图"}, {"Shift+R", "刷新全部仓库"}, {"F", "查找当前视图"}, {"L", "查找 Log"}, {"K", "编辑 Commit Message"}, {"T", "Pull 当前范围仓库（仅快进）"}, {"Shift+K", "Push（Changes 视图，打开确认）"}, {"Enter", "Commit"}, {"Shift+Enter", "Commit and Push（打开确认）"}} {
				ui.Row(c).Children(func() { ui.Text(c, prefix+"+"+row.Key).Width(155).FontSize(12); ui.Text(c, row.Action).FontSize(12) })
			}
			ui.Text(c, "Ctrl+M：提交说明历史 · F7 / Shift+F7：变更块 · F8 / Shift+F8：文件").FontSize(11)
			ui.Button(c, "关闭").OnClick(func() { a.shortcutHelpOpen = false })
		})
	})
}
