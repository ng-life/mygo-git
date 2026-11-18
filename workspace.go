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

	})
}

// openToolbarReview preserves the current selection when opening Review from the menu.
func (a *client) openToolbarReview() {
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
}

// Only visible rows are built; text layout is prepared by the background reader.
func comparisonPane(c *ui.Context, title string, document DiffDocument, changed map[int]bool, added bool, navigation ...CodeNavigation) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
		ui.Text(c, title).Bold().FontSize(12).Padding(6)
		if len(document.Lines) == 0 {
			ui.Text(c, "(empty)").TextColor(t.TextMuted)
			return
		}
		ui.List(c.Key("diff-lines"), func() *ui.ListState {
			if len(navigation) > 0 && navigation[0].Client != nil {
				if tab := navigation[0].Client.activeEditor(); tab != nil && tab.Kind == "file" {
					return &tab.FileList
				}
			}
			return nil
		}(), len(document.Lines)).Grow(1).Selectable().RowWidth(document.Width).Rows(func(item ui.ListRow) {
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

func (a *client) view(c *ui.Context) {
	a.syncApplicationMenu()
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
					a.editorPanel(c)
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
		status := "Ready"
		if a.busy {
			status = "Running Git operation…"
		}
		ui.Row(c).Height(23).Shrink(0).Padding(0, 6).Gap(6).Background(ideaToolbar).AlignItems(ui.Center).Children(func() {
			ui.Text(c, status+" · "+a.output).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(a.output)
			a.backgroundTaskIndicator(c)
		})
		a.backgroundTasksPanel(c)
		a.shortcutHelpPanel(c)
	})
}
