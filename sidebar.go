package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *client) selectRepository(r Repository) {
	if a.review.Open {
		a.closeReview()
	}
	if a.repositoryPath != r.Path {
		a.clearWorkingComparison()
	}
	a.repositoryPath = r.Path
	a.group = ""
	a.groupRemote = false
	a.groupTags = false
	a.branch = ""
	a.query = ""
	a.selected = map[string]bool{r.Path: true}
	a.reloadHistory()
	if *a.browserOpen("repository:" + r.Path) {
		a.loadRepositoryFiles(r, false)
	}
}
func (a *client) repositoryAction(r Repository, action string) {
	if a.busy {
		return
	}
	entries := entriesFor([]Repository{r})
	a.jobProgress(action+" · "+r.Name, func(report taskReporter) (func(), error) {
		report(action+" · "+r.Name, 0, 2, nil)
		out, err := runRepositoryOperation(r.Path, action)
		report(action+" · "+r.Name, 1, 2, err)
		result := formatResults([]OperationResult{{Repository: r, Output: out, Err: err}})
		repos := inspectWithProgress(entries, report, 1, 2)
		return func() { a.mergeRepositories(repos); a.output = action + "\n\n" + result }, nil
	})
}
func (a *client) branchMenu(m *ui.Menu, branch string, remote bool, targets []Repository) {
	m.Item(fmt.Sprintf("%s · 全部 %d 个仓库", branch, len(targets))).Disabled(true)
	m.Separator()
	if m.Item("Review… · 图形比较此分支").Chosen() {
		a.openReviewDialog(targets, fullBranchRef(branch, remote))
	}
	for _, op := range []struct{ action, label string }{
		{"checkout", "checkout · 切换到此分支"},
		{"merge", "merge · 合并到各仓库当前分支"},
		{"rebase", "rebase · 各仓库当前分支变基到此"},
		{"diff", "diff · 当前 HEAD 与此分支比较"},
		{"log", "log · 查看此分支提交"},
		{"刷新状态", "刷新状态 · 对应仓库"},
		{"Fetch", "Fetch · 对应仓库"},
		{"Pull", "Pull · 各仓库当前分支"},
	} {
		if m.Item(op.label).Disabled(a.busy || len(targets) == 0).Chosen() {
			a.runBranchAction(op.action, branch, remote, targets)
		}
	}
	m.Separator()
	label := "删除本地分支…"
	if remote {
		label = "删除远程分支…"
	}
	if m.Item(label).Disabled(a.busy || len(targets) == 0).Chosen() {
		a.requestBranchDeletion(branch, remote, targets)
	}

}
func (a *client) repositoryMenu(m *ui.Menu, r Repository) {
	m.Item(r.Name + " · 仅此仓库").Disabled(true)
	m.Separator()
	if m.Item("Review… · 当前工作区").Chosen() {
		a.openReviewDialog([]Repository{r}, "")
	}
	for _, action := range []string{"刷新状态", "Fetch", "Pull", "diff · 工作区修改", "log · 当前分支提交"} {
		if m.Item(action).Disabled(a.busy).Chosen() {
			a.repositoryAction(r, action)
		}
	}
	m.Separator()
	for _, remote := range []bool{false, true} {
		title := "本地分支操作"
		if remote {
			title = "远程分支操作"
		}
		branches := repositoryBranches(r, remote)
		m.Submenu(title, func(sub *ui.Menu) {
			for _, branch := range branches {
				sub.Submenu(branch, func(ops *ui.Menu) { a.branchMenu(ops, branch, remote, []Repository{r}) })
			}
		}).Disabled(a.busy || len(branches) == 0)
	}
}
func (a *client) repositorySection(c *ui.Context) {
	ui.Column(c.Key("repositories")).Fill().Padding(5, 4).Gap(4).Children(func() {
		ui.Text(c, "仓库列表").Bold().TextColor(c.Theme().TextMuted)
		ui.TextInput(c.Key("repository-search"), &a.repositoryQuery).Bind(&a.repositorySearchHandle).Placeholder("搜索仓库").Label("搜索仓库")
		ui.Scroll(c).Grow(1).Children(func() {
			count := 0
			for _, repo := range a.repos {
				if !repositoryMatches(repo, a.repositoryQuery) {
					continue
				}
				count++
				open := a.browserOpen("repository:" + repo.Path)
				ui.Column(c.Key(repo.Path)).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
						arrow := "▸"
						if *open {
							arrow = "▾"
						}
						ui.ButtonBase(c.Key("repository-disclosure")).Width(18).Height(22).Label("展开仓库 " + repo.Name).OnClick(func() { a.toggleRepositoryTree(repo) }).Children(func() { ui.Text(c, arrow) })
						name := ui.ButtonBase(c.Key("repository-select")).Grow(1).MinWidth(0).Justify(ui.Start).Height(22).Label(repo.Name).Tooltip(repositoryStateTooltip(repo))
						if a.repositoryPath == repo.Path {
							name.Background(ideaSelection).TextColor(c.Theme().Text).Bold()
						}
						treeContentAction(name, func() { a.selectRepository(repo) }, func() { a.toggleRepositoryTree(repo) }).OnShortcut(0, ui.KeyRight, func() { *open = true; a.loadRepositoryFiles(repo, false) }).OnShortcut(0, ui.KeyLeft, func() { *open = false }).ContextMenu(func(m *ui.Menu) { a.repositoryMenu(m, repo) }).Children(func() {
							color := repositoryStateColor(repo, c.Theme().TextMuted)
							ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Text(c, repo.Name).FontSize(12).Bold().SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).TextColor(c.Theme().Text)
								ui.Text(c, repo.Branch).FontSize(12).Bold().SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).TextColor(color).Tooltip(repositoryStateTooltip(repo))
								if repo.Ahead > 0 {
									ui.Textf(c, "↑ %d", repo.Ahead).FontSize(11).Shrink(0).TextColor(statusBlue)
								}
								if repo.Status != "" {
									ui.Text(c, "●").FontSize(11).Shrink(0).TextColor(statusOrange)
								}
								ui.Spacer(c)
							})
						})
					})
					if *open {
						ui.Column(c).Padding(0, 0, 0, 12).Children(func() {
							state := a.repositoryFiles[repo.Path]
							if message := repositoryFileMessage(state); message != "" {
								ui.Text(c, message).FontSize(11)
							}
							if state.Root != nil {
								ui.Tree(c, func() { a.repositoryFileNodes(c, repo, state, state.Root.Children) })
							}
						})
					}
				})
			}
			if count == 0 {
				text := "暂无仓库"
				if len(a.repos) > 0 {
					text = "暂无匹配仓库"
				}
				ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
	})
}
