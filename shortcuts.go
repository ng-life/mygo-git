package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

func primaryShortcutModifier() ui.Modifiers {
	return primaryShortcutModifierForOS(runtime.GOOS)
}

func primaryShortcutModifierForOS(goos string) ui.Modifiers {
	if goos == "darwin" {
		return ui.Super
	}
	return ui.Ctrl
}
func (a *client) shortcutModalOpen() bool {
	return a.taskPanelOpen || a.shortcutHelpOpen || a.review.Dialog || a.settings.Open || a.tagDialog.Open || a.changesDialog.Open || a.changelistDialog.Open || a.messageHistory.Open || a.refDialog.Open || (a.codeNavigation.Open && !a.codeNavigation.Loading) || a.branchDeletion.Open
}
func (a *client) registerShortcuts(c *ui.Context) {
	if a.shortcutModalOpen() {
		return
	}
	mod := primaryShortcutModifier()
	c.OnWindowShortcut(mod, ui.KeyW, a.closeActiveEditor)
	c.OnWindowShortcut(ui.Ctrl, ui.KeyTab, func() { a.cycleEditor(1) })
	c.OnWindowShortcut(ui.Ctrl|ui.Shift, ui.KeyTab, func() { a.cycleEditor(-1) })
	c.OnWindowShortcut(mod, ui.KeyComma, func() { a.applicationAction("settings") })
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
					a.repositoryAction(repo, "Refresh Status")
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
		a.runBranchAction("Pull", "Current Branch", false, append([]Repository(nil), repos...))
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
			ui.Text(c, "Keyboard Shortcuts").FontSize(16).Bold()
			prefix := "Ctrl"
			if runtime.GOOS == "darwin" {
				prefix = "⌘"
			}
			for _, row := range []struct{ Key, Action string }{{"0", "Repositories"}, {"1", "Changes"}, {"2", "Logs"}, {",", "Settings"}, {"W", "Close Editor Tab"}, {"R", "Refresh Current View"}, {"Shift+R", "Refresh All Repositories"}, {"F", "Find in Current View"}, {"L", "Find in Log"}, {"K", "Edit Commit Message"}, {"T", "Pull Repositories in Scope (fast-forward only)"}, {"Shift+K", "Push (Changes view, confirm)"}, {"Enter", "Commit"}, {"Shift+Enter", "Commit and Push (confirm)"}} {
				ui.Row(c).Children(func() { ui.Text(c, prefix+"+"+row.Key).Width(155).FontSize(12); ui.Text(c, row.Action).FontSize(12) })
			}
			ui.Text(c, "Ctrl+Tab / Ctrl+Shift+Tab: Editor Tabs · Ctrl+M: Message History · F7 / Shift+F7: Hunks · F8 / Shift+F8: Files").FontSize(11)
			ui.Button(c, "Close").OnClick(func() { a.shortcutHelpOpen = false })
		})
	})
}
