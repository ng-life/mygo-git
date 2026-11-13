package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *client) navigation(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Stretch).Background(t.Surface).Children(func() {
		tabs := ui.TabsBase(c.Key("navigation-tabs"), &a.tab, 3)
		tabs.List.Column().Orientation(ui.Vertical).FocusGroup(ui.Vertical).Width(22).Padding(0).Gap(0).Background(ui.Hex("#343638")).
			OnChange(func() { a.switchTab(a.tab) }).Children(func() {
			for i, label := range []string{"Repositories", "Changes", "Logs"} {
				height := float32(72)
				if i == 0 {
					height = 100
				} else if i == 2 {
					height = 48
				}
				tab := tabs.Tab(i).Width(22).Height(height).Center().Radius(0).Label(label).Tooltip(fmt.Sprintf("%s · ⌘%d / Ctrl+%d", label, i, i))
				if a.tab == i {
					tab.Background(ideaSelection).TextColor(t.AccentText).Bold()
				}
				tab.Children(func() {
					ui.Text(c, label).Absolute().Left(-37).Top((height-18)/2).Width(96).Height(18).TextAlign(ui.Center).FontSize(11).SingleLine().TransformOrigin(0.5, 0.5).Rotate(-90)
				})
			}
		})
		ui.Column(c.Key(fmt.Sprintf("navigation-%d", a.tab))).Grow(1).MinWidth(0).Children(func() {
			switch a.tab {
			case 0:
				a.repositorySection(c)
			case 1:
				if a.changesSplit == 0 {
					a.changesSplit = 60
				}
				ui.SplitVerticalPercent(c.Key("changes-sidebar-split"), &a.changesSplit, func() { a.workingChangesPanel(c) }, func() { a.outgoingPanel(c) }).Fill().SplitReset(60)
			case 2:
				a.logsNavigation(c)
			}
		})
	})
}

func (a *client) toolbar(c *ui.Context) {
	ui.Row(c).Height(34).Shrink(0).Padding(3, 10).Gap(6).Background(ideaToolbar).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "MyGo Git").FontSize(12).Bold()
		ui.Text(c, "/  Git").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		ui.Button(c, "快捷键").OnClick(func() { a.shortcutHelpOpen = true })
		ui.Button(c.Key("settings"), "设置").Disabled(a.busy).OnClick(a.openScanSettings)
		ui.Button(c.Key("refresh"), "刷新全部").Disabled(a.busy).OnClick(a.refresh)
		ui.Button(c.Key("review-entry"), "Review…").Disabled(len(a.repos) == 0 || a.tab == 2 && len(a.refSelection) > 2).OnClick(func() {
			if a.tab == 2 && len(a.refSelection) == 2 {
				a.openReferenceComparison("HEAD", true)
				return
			}
			repos := a.visible()
			if selected := a.targets(); len(selected) > 0 {
				repos = selected
			}
			target := a.branch
			if target != "" && !a.groupTags {
				target = fullBranchRef(target, a.groupRemote)
			}
			a.openReviewDialog(repos, target)
		})
		ui.Button(c.Key("results"), "操作结果").OnClick(func() { a.showOutput = !a.showOutput })
	})
}

// Only visible rows are built; text layout is prepared by the background reader.
func comparisonPane(c *ui.Context, title string, document DiffDocument, changed map[int]bool, added bool, navigation ...CodeNavigation) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
		ui.Text(c, title).Bold().FontSize(12).Padding(6)
		if len(document.Lines) == 0 {
			ui.Text(c, "（空）").TextColor(t.TextMuted)
			return
		}
		ui.List(c.Key("diff-lines"), nil, len(document.Lines)).Grow(1).Selectable().RowWidth(document.Width).Rows(func(item ui.ListRow) {
			i := item.Index
			row := ui.Row(item.Context).Height(20).Gap(10).Padding(1, 6)
			if changed[i+1] {
				color := diffRowColor(c, false)
				if added {
					color = diffRowColor(c, true)
				}
				row.Background(color).TextColor(diffCodeColor(c))
			}
			row.Children(func() {
				ui.Textf(c, "%4d", i+1).Unselectable().Font("monospace").FontSize(11)
				base := c.Theme().Text
				if changed[i+1] {
					base = diffCodeColor(c)
				}
				var nav *CodeNavigation
				if len(navigation) > 0 {
					nav = &navigation[0]
				}
				codeLineWithNavigation(c, document, i, base, nav)
			})
		})
	})
}

func (a *client) comparisonBlock(c *ui.Context, part CommitComparison) {
	change := part.Change
	commit := change.Detail.Commit
	ui.Column(c.Key(commit.Key() + "\x00" + change.File.Path)).Fill().Padding(0).Gap(4).Children(func() {
		afterLabel := reviewAfterLabel(commit.Hash)
		if a.review.Open && a.review.Request.Reverse {
			afterLabel = shortHash(part.Comparison.AfterSource.Revision)
		}
		ui.Text(c, commit.Repository.Name+" · "+afterLabel+" · "+change.File.Label()).FontSize(12).SingleLine().Ellipsis("…").Padding(5, 8).FillWidth().Background(ideaToolbar).Tooltip(change.File.Label())
		ui.Text(c, commit.Subject).FontSize(12).TextColor(c.Theme().TextMuted)
		if part.Error != "" {
			ui.Text(c, part.Error)
			return
		}
		if part.Comparison.Notice != "" {
			ui.Text(c, part.Comparison.Notice)
			return
		}
		beforeRef := shortHash(change.Detail.Parent)
		if beforeRef == "" {
			beforeRef = "空树"
		}
		afterRef := reviewAfterLabel(commit.Hash)
		if a.review.Open && a.review.Request.Reverse {
			beforeRef = "Worktree"
			afterRef = shortHash(part.Comparison.AfterSource.Revision)
		}
		reviewComparison(c, part.Comparison, "变更前 · "+beforeRef, "变更后 · "+afterRef, a)
	})
}
func (a *client) comparisonPanel(c *ui.Context) {
	if a.selectedFile == "" {
		return
	}
	if a.comparisonLoading {
		ui.Text(c, "正在读取文件内容…").Padding(12)
		return
	}
	ui.Column(c).Fill().Gap(4).Children(func() {
		a.reviewFileNavigation(c)
		if len(a.comparisons) == 1 {
			a.comparisonBlock(c, a.comparisons[0])
			return
		}
		ui.Scroll(c).Fill().Gap(10).Children(func() {
			for _, part := range a.comparisons {
				ui.Column(c.Key(part.Change.Detail.Commit.Key())).Height(300).Shrink(0).Children(func() { a.comparisonBlock(c, part) })
			}
		})
	})
}
func (a *client) view(c *ui.Context) {
	c.SetTheme(ideaTheme)
	a.registerShortcuts(c)
	ui.Column(c).Fill().AlignItems(ui.Stretch).Background(c.Theme().Background).Children(func() {
		a.toolbar(c)
		defaultWidth := float32(270)
		if a.tab == 1 {
			defaultWidth = 400
		}
		width := &a.sidebarWidths[a.tab]
		if *width == 0 {
			*width = defaultWidth
		}
		minimumWidth := float32(240)
		if a.tab == 1 {
			minimumWidth = 320
		}
		*width = max(minimumWidth, min(*width, 800))
		ui.Split(c.Key("navigation-width"), width, func() { a.navigation(c) }, func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.SplitVerticalPercent(c.Key("workspace-split"), &a.workspaceSplit, func() {
					if a.review.Open {
						a.comparisonPanel(c)
					} else if a.tab == 0 && a.workingFileKey != "" {
						a.repositoryFilePanel(c)
					} else if a.tab == 1 {
						if a.workingFileKey != "" {
							a.workingComparisonPanel(c)
						} else {
							a.comparisonPanel(c)
						}
					} else {
						a.comparisonPanel(c)
					}
				}, func() {
					if a.review.Open {
						a.reviewPanel(c)
					} else {
						a.logPanel(c)
					}
				}).Fill().SplitReset(55)
			})
		}).Grow(1).FillWidth().MinHeight(0).SplitReset(defaultWidth)
		if a.showOutput {
			ui.TextArea(c.Key("output"), &a.output).ReadOnly(true).Height(100).Font("monospace")
		}
		a.codeNavigationPanel(c)
		a.branchDeletionPanel(c)
		a.reviewDialog(c)
		a.scanSettingsPanel(c)
		a.tagManagementPanel(c)
		a.changesActionDialog(c)
		a.changelistManagementPanel(c)
		a.commitMessageHistoryPanel(c)
		a.referenceManagementPanel(c)
		status := "就绪"
		if a.busy {
			status = "正在执行 Git 操作…"
		}
		ui.Row(c).Height(23).Shrink(0).Padding(0, 6).Gap(6).Background(ideaToolbar).AlignItems(ui.Center).Children(func() {
			ui.Text(c, status+" · "+a.output).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(a.output)
			a.backgroundTaskIndicator(c)
		})
		a.backgroundTasksPanel(c)
		a.shortcutHelpPanel(c)
	})
}
