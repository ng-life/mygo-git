package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *client) comparePanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(4).Background(ideaPanel).Children(func() {
		ui.Row(c).Shrink(0).Padding(4, 8).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Compare").Bold()
			ui.Text(c, "A").FontSize(11)
			ui.TextInput(c.Key("compare-a"), &a.compare.A).Label("Compare A").Placeholder("Branch / Tag / SHA / HEAD / Worktree").Grow(1).MinWidth(80)
			ui.Button(c, "Swap").Label("Swap Revisions").Disabled(a.review.Loading).OnClick(func() { a.applyCompare(true) })
			ui.Text(c, "B").FontSize(11)
			ui.TextInput(c.Key("compare-b"), &a.compare.B).Label("Compare B").Placeholder("Branch / Tag / SHA / HEAD / Worktree").Grow(1).MinWidth(80)
			ui.Button(c, "Compare").Label("Apply Revisions").Disabled(a.review.Loading || strings.TrimSpace(a.compare.A) == "" || strings.TrimSpace(a.compare.B) == "").OnClick(func() { a.applyCompare(false) })
		})
		ui.Row(c).Shrink(0).Padding(0, 8).Gap(5).Children(func() {
			for _, tab := range []struct{ Mode, Label string }{{"files", "File Diff"}, {"sides", "Commit Differences"}, {"onlyA", "Only A"}, {"onlyB", "Only B"}, {"both", "Unique to Each"}, {"bases", "Merge Base"}} {
				ui.Button(c.Key(tab.Mode), tab.Label).Selected(a.compare.Mode == tab.Mode).Disabled(a.compare.Request.Kind == "workspace" && tab.Mode != "files").Tooltip("Compare commit reachability. Worktree has no separate commit history").OnClick(func() {
					a.compare.Mode = tab.Mode
					a.clearCompareCommitSelection()
					if tab.Mode == "files" {
						request := a.compare.Request
						a.beginReview(request)
					} else {
						a.clearComparison()
					}
				})
			}
			ui.Spacer(c)
			ui.Button(c, "Close Comparison").OnClick(a.closeReviewEditors)
		})
		if a.compare.Request.Kind != "workspace" {
			ui.Row(c).Shrink(0).Padding(0, 8).Gap(8).Children(func() {
				kind := a.compare.Request.Kind
				ui.Button(c, "A → B Net Diff").Selected(kind == "direct").OnClick(func() {
					request := a.compare.Request
					request.Kind = "direct"
					request.Net = true
					a.beginReview(request)
				})
				ui.Button(c, "Merge Base → B").Selected(kind == "merge-base").OnClick(func() {
					request := a.compare.Request
					request.Kind = "merge-base"
					request.Net = true
					a.beginReview(request)
				})
			})
		}
		ui.Row(c).Shrink(0).Padding(0, 8).Gap(6).Children(func() {
			ignore, large := a.compare.Request.IgnoreWhitespace, a.compare.Request.LargeFiles
			ui.Checkbox(c.Key("ignore-whitespace"), &ignore, "Ignore Whitespace").Disabled(a.review.Loading).OnClick(func() { request := a.compare.Request; request.IgnoreWhitespace = ignore; a.beginReview(request) })
			ui.Checkbox(c.Key("large-files"), &large, "Load Large File (up to 8 MiB)").Disabled(a.review.Loading).OnClick(func() { request := a.compare.Request; request.LargeFiles = large; a.beginReview(request) })
		})
		if a.compare.Request.Kind != "workspace" {
			countA, countB := 0, 0
			for _, endpoint := range a.compare.Result.Endpoints {
				countA += endpoint.CountA
				countB += endpoint.CountB
			}
			ui.Textf(c, "Only A: %d commits · Only B: %d commits", countA, countB).FontSize(11).Padding(0, 8).TextColor(c.Theme().TextMuted)
		}
		if a.compare.Mode == "files" {
			a.compareFileFilters(c)
			a.reviewFilesPanel(c)
			return
		}
		if a.compare.Loading {
			ui.Text(c, "Comparing commit sets…").FontSize(11)
		}
		if a.compare.Result.Error != "" {
			ui.Text(c, a.compare.Result.Error).FontSize(11).TextColor(statusOrange)
		}
		if a.compare.Mode == "bases" {
			ui.Scroll(c).Grow(1).Padding(8).Children(func() {
				for _, endpoint := range a.compare.Result.Endpoints {
					ui.Text(c, endpoint.Repository.Name).Bold()
					ui.Text(c, "A: "+endpoint.A+"\nB: "+endpoint.B).FontSize(11).Selectable()
					if len(endpoint.Bases) == 0 {
						ui.Text(c, "No merge base").FontSize(11)
					}
					for _, base := range endpoint.Bases {
						ui.Row(c).Gap(6).Children(func() {
							ui.Text(c, "Merge base: "+base).FontSize(11).Selectable().Grow(1)
							ui.Button(c, "Compare This Base → B").Label("Compare merge base " + endpoint.Repository.Path + " " + base).OnClick(func() {
								request := a.compare.Request
								request.Kind = "direct"
								request.Net = true
								request.Base = base
								request.Target = endpoint.B
								request.Repositories = []Repository{endpoint.Repository}
								a.beginReview(request)
							})
						})
					}
				}
			})
			return
		}
		ui.TextInput(c.Key("compare-search"), &a.compare.Query).Bind(&a.compareCommitSearchHandle).Label("Search Comparison Commits").Placeholder("Search message / SHA / author / repository").OnChange(a.clearCompareCommitSelection)
		rows := a.compareCommits()
		countA, countB := 0, 0
		for _, endpoint := range a.compare.Result.Endpoints {
			countA += endpoint.CountA
			countB += endpoint.CountB
		}
		ui.Row(c).Shrink(0).Padding(0, 8).Gap(8).Children(func() {
			ui.Textf(c, "Only A %d · Only B %d · Showing %d · Equivalent patches may have different SHAs", countA, countB, len(rows)).FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
			if a.compare.Limit > 0 && (countA+countB) > len(a.compare.Result.Commits) {
				ui.Button(c, "Load More Commits").Disabled(a.compare.Loading).OnClick(func() { a.compare.Limit += 200; a.reloadCompare() })
			}
		})
		if a.compare.Mode == "sides" {
			a.compareSidesPanel(c, rows, countA, countB)
			return
		}
		ui.SplitPercent(c.Key("compare-commit-files"), compareSplitSize(&a.compareSplit), func() {
			ui.List(c.Key("compare-commits"), a.compareListState(0), len(rows)).Fill().ItemKey(func(i int) any { return rows[i].Commit.Key() }).Rows(func(item ui.ListRow) {
				index := item.Index
				entry := rows[index]
				commit := entry.Commit
				row := ui.ButtonBase(item.Context).FillWidth().Height(25).Padding(2, 6).Justify(ui.Start).Label("Compare Commit " + commit.Key()).Tooltip(commit.Repository.Path + "\n" + commit.Hash)
				if a.compare.Selected[commit.Key()] {
					row.Background(ideaSelection)
				}
				mods := row.ClickModifiers()
				row.OnClick(func() {
					flags := 0
					if mods&(ui.Ctrl|ui.Super) != 0 {
						flags |= 1
					}
					if mods&ui.Shift != 0 {
						flags |= 2
					}
					a.selectCompareCommit(index, flags)
				}).Children(func() {
					ui.Text(c, entry.Side).Width(18).FontSize(11).TextColor(statusBlue)
					ui.Text(c, commit.Repository.Name).Width(115).SingleLine().Ellipsis("…").FontSize(11)
					ui.Text(c, shortHash(commit.Hash)).Width(68).FontSize(11)
					ui.Text(c, commit.Author).Width(65).FontSize(11).SingleLine().Ellipsis("…")
					ui.Text(c, time.Unix(commit.Timestamp, 0).Format("01-02 15:04")).Width(78).FontSize(10).TextColor(c.Theme().TextMuted)
					ui.Text(c, commit.Subject).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(12)
					if entry.Equivalent {
						ui.Text(c, "Patch Equivalent").FontSize(10).TextColor(statusOrange)
					}
				})
			})
		}, func() {
			if len(a.compare.Selected) == 0 {
				ui.Text(c, "Select a commit to view changed files. Ctrl / ⌘ to toggle, Shift for range").FontSize(11).Padding(8)
			} else {
				a.compareFileFilters(c)
				a.reviewFilesPanel(c)
			}
		}).Grow(1).FillWidth().MinHeight(0).SplitReset(58)
	})
}
func compareSplitSize(value *float32) *float32 {
	if *value == 0 {
		*value = 58
	}
	return value
}
func (a *client) compareFileFilters(c *ui.Context) {
	ui.Row(c).Shrink(0).Padding(0, 8).Gap(5).Children(func() {
		ui.TextInput(c.Key("compare-file-search"), &a.compare.FileQuery).Bind(&a.compareSearchHandle).Label("Search Comparison Files").Placeholder("Search file path / repository").OnChange(a.reconcileCompareFileFilter).Grow(1).MinWidth(60)
		for _, filter := range []struct{ Status, Label string }{{"", "All"}, {"A", "Added"}, {"M", "Modified"}, {"D", "Delete"}, {"R", "Rename"}} {
			ui.Button(c.Key(filter.Status), filter.Label).FontSize(11).Selected(a.compare.Status == filter.Status).OnClick(func() { a.compare.Status = filter.Status; a.reconcileCompareFileFilter() })
		}
	})
}

func (a *client) compareSidesPanel(c *ui.Context, rows []CompareCommit, countA, countB int) {
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Gap(4).Children(func() {
		ui.Row(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Gap(1).Children(func() {
			for _, side := range []struct {
				ID, Ref string
				Count   int
			}{{"A", a.compare.A, countA}, {"B", a.compare.B, countB}} {
				ui.Column(c.Key(side.ID)).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Gap(4).Children(func() {
					ui.Textf(c, "Only %s · %s · %d commits", side.ID, side.Ref, side.Count).FontSize(12).Bold().SingleLine().Ellipsis("…").Padding(6)
					var indices []int
					for i, row := range rows {
						if row.Side == side.ID {
							indices = append(indices, i)
						}
					}
					ui.List(c.Key("commits"), a.compareListState(func() int {
						if side.ID == "A" {
							return 1
						}
						return 2
					}()), len(indices)).Grow(1).FillWidth().ItemKey(func(i int) any { return rows[indices[i]].Commit.Key() }).Rows(func(item ui.ListRow) {
						index := indices[item.Index]
						entry := rows[index]
						commit := entry.Commit
						row := ui.ButtonBase(item.Context).Justify(ui.Start).Height(44).FillWidth().Padding(3, 6).Label("Compare Commit " + commit.Key()).Tooltip(commit.Repository.Name + "\n" + commit.Hash + "\n" + commit.Author)
						if a.compare.Selected[commit.Key()] {
							row.Background(ideaSelection)
						}
						mods := row.ClickModifiers()
						row.OnClick(func() {
							flags := 0
							if mods&(ui.Ctrl|ui.Super) != 0 {
								flags |= 1
							}
							if mods&ui.Shift != 0 {
								flags |= 2
							}
							a.selectCompareCommit(index, flags)
						}).Children(func() {
							ui.Column(c).Grow(1).FillWidth().MinWidth(0).AlignItems(ui.Start).Gap(2).Children(func() {
								ui.Text(c, commit.Subject).FontSize(12).SingleLine().Ellipsis("…")
								text := shortHash(commit.Hash) + " · " + commit.Repository.Name + " · " + commit.Author
								if entry.Equivalent {
									text += " · Patch Equivalent"
								}
								ui.Text(c, text).FontSize(10).SingleLine().Ellipsis("…").TextColor(c.Theme().TextMuted)
							})
						})
					})
				})
			}
		})
		if len(a.compare.Selected) > 0 {
			ui.Column(c.Key("commit-files")).Height(160).Children(func() { a.reviewFilesPanel(c) })
		}
	})
}
