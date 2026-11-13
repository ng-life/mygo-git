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
			ui.TextInput(c.Key("compare-a"), &a.compare.A).Label("Compare A").Placeholder("分支 / Tag / SHA / HEAD / Worktree").Grow(1).MinWidth(80)
			ui.Button(c, "交换").Label("交换比较版本").Disabled(a.review.Loading).OnClick(func() { a.applyCompare(true) })
			ui.Text(c, "B").FontSize(11)
			ui.TextInput(c.Key("compare-b"), &a.compare.B).Label("Compare B").Placeholder("分支 / Tag / SHA / HEAD / Worktree").Grow(1).MinWidth(80)
			ui.Button(c, "比较").Label("应用比较版本").Disabled(a.review.Loading || strings.TrimSpace(a.compare.A) == "" || strings.TrimSpace(a.compare.B) == "").OnClick(func() { a.applyCompare(false) })
		})
		ui.Row(c).Shrink(0).Padding(0, 8).Gap(5).Children(func() {
			for _, tab := range []struct{ Mode, Label string }{{"files", "文件差异"}, {"onlyA", "仅 A"}, {"onlyB", "仅 B"}, {"both", "双方独有"}, {"bases", "共同祖先"}} {
				ui.Button(c.Key(tab.Mode), tab.Label).Selected(a.compare.Mode == tab.Mode).Disabled(a.compare.Request.Kind == "workspace" && tab.Mode != "files").Tooltip("按提交可达关系比较；Worktree 没有独立提交历史").OnClick(func() {
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
			ui.Button(c, "结束 Compare").OnClick(a.closeReview)
		})
		if a.compare.Request.Kind != "workspace" {
			ui.Row(c).Shrink(0).Padding(0, 8).Gap(8).Children(func() {
				kind := a.compare.Request.Kind
				ui.Button(c, "A → B 最终差异").Selected(kind == "direct").OnClick(func() {
					request := a.compare.Request
					request.Kind = "direct"
					request.Net = true
					a.beginReview(request)
				})
				ui.Button(c, "共同祖先 → B").Selected(kind == "merge-base").OnClick(func() {
					request := a.compare.Request
					request.Kind = "merge-base"
					request.Net = true
					a.beginReview(request)
				})
			})
		}
		ui.Row(c).Shrink(0).Padding(0, 8).Gap(6).Children(func() {
			ignore, large := a.compare.Request.IgnoreWhitespace, a.compare.Request.LargeFiles
			ui.Checkbox(c.Key("ignore-whitespace"), &ignore, "忽略空白").Disabled(a.review.Loading).OnClick(func() { request := a.compare.Request; request.IgnoreWhitespace = ignore; a.beginReview(request) })
			ui.Checkbox(c.Key("large-files"), &large, "加载大文件（最多 8 MiB）").Disabled(a.review.Loading).OnClick(func() { request := a.compare.Request; request.LargeFiles = large; a.beginReview(request) })
		})
		if a.compare.Mode == "files" {
			a.compareFileFilters(c)
			a.reviewFilesPanel(c)
			return
		}
		if a.compare.Loading {
			ui.Text(c, "正在比较提交集合…").FontSize(11)
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
						ui.Text(c, "无共同祖先").FontSize(11)
					}
					for _, base := range endpoint.Bases {
						ui.Row(c).Gap(6).Children(func() {
							ui.Text(c, "Merge base: "+base).FontSize(11).Selectable().Grow(1)
							ui.Button(c, "比较此祖先 → B").Label("比较共同祖先 " + endpoint.Repository.Path + " " + base).OnClick(func() {
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
		ui.TextInput(c.Key("compare-search"), &a.compare.Query).Bind(&a.compareCommitSearchHandle).Label("搜索比较提交").Placeholder("搜索提交说明 / SHA / 作者 / 仓库").OnChange(a.clearCompareCommitSelection)
		rows := a.compareCommits()
		countA, countB := 0, 0
		for _, endpoint := range a.compare.Result.Endpoints {
			countA += endpoint.CountA
			countB += endpoint.CountB
		}
		ui.Row(c).Shrink(0).Padding(0, 8).Gap(8).Children(func() {
			ui.Textf(c, "仅 A %d · 仅 B %d · 当前显示 %d · 相同补丁不等于相同 SHA", countA, countB, len(rows)).FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
			if a.compare.Limit > 0 && (countA+countB) > len(a.compare.Result.Commits) {
				ui.Button(c, "加载更多提交").Disabled(a.compare.Loading).OnClick(func() { a.compare.Limit += 200; a.reloadCompare() })
			}
		})
		ui.SplitPercent(c.Key("compare-commit-files"), compareSplitSize(&a.compareSplit), func() {
			ui.List(c.Key("compare-commits"), nil, len(rows)).Fill().ItemKey(func(i int) any { return rows[i].Commit.Key() }).Rows(func(item ui.ListRow) {
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
						ui.Text(c, "补丁等价").FontSize(10).TextColor(statusOrange)
					}
				})
			})
		}, func() {
			if len(a.compare.Selected) == 0 {
				ui.Text(c, "选择 Commit 查看关联文件；Ctrl / ⌘ 多选，Shift 连选").FontSize(11).Padding(8)
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
		ui.TextInput(c.Key("compare-file-search"), &a.compare.FileQuery).Bind(&a.compareSearchHandle).Label("搜索比较文件").Placeholder("搜索文件路径 / 仓库").OnChange(a.reconcileCompareFileFilter).Grow(1).MinWidth(60)
		for _, filter := range []struct{ Status, Label string }{{"", "全部"}, {"A", "新增"}, {"M", "修改"}, {"D", "删除"}, {"R", "重命名"}} {
			ui.Button(c.Key(filter.Status), filter.Label).FontSize(11).Selected(a.compare.Status == filter.Status).OnClick(func() { a.compare.Status = filter.Status; a.reconcileCompareFileFilter() })
		}
	})
}
