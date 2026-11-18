package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *client) selectRepository(r Repository) {
	a.saveEditorContext()
	defer a.restoreEditorContext()
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
	m.Item(fmt.Sprintf("%s · All %d repositories", branch, len(targets))).Disabled(true)
	m.Separator()
	if m.Item("Review… · Compare Branch").Chosen() {
		a.openReviewDialog(targets, fullBranchRef(branch, remote))
	}
	for _, op := range []struct{ action, label string }{
		{"checkout", "Checkout Branch"},
		{"merge", "Merge into Current Branch in Each Repository"},
		{"rebase", "Rebase Current Branch in Each Repository onto This Branch"},
		{"diff", "Diff · HEAD to Branch"},
		{"log", "Log · Branch History"},
		{"Refresh Status", "Refresh Status · Related Repositories"},
		{"Fetch", "Fetch · Related Repositories"},
		{"Pull", "Pull · Current Branch in Each Repository"},
	} {
		if m.Item(op.label).Disabled(a.busy || len(targets) == 0).Chosen() {
			a.runBranchAction(op.action, branch, remote, targets)
		}
	}
	m.Separator()
	label := "Delete Local Branch…"
	if remote {
		label = "Delete Remote Branch…"
	}
	if m.Item(label).Disabled(a.busy || len(targets) == 0 || (!remote && currentLocalBranch(branch, targets))).Chosen() {
		a.requestBranchDeletion(branch, remote, targets)
	}

}
func (a *client) repositoryMenu(m *ui.Menu, r Repository) {
	m.Item(r.Name + " · This repository").Disabled(true)
	m.Separator()
	if m.Item("Review… · Working Tree").Chosen() {
		a.openReviewDialog([]Repository{r}, "")
	}
	for _, action := range []string{"Refresh Status", "Fetch", "Pull", "Diff · Working Tree Changes", "Log · Current Branch"} {
		if m.Item(action).Disabled(a.busy).Chosen() {
			a.repositoryAction(r, action)
		}
	}
	m.Separator()
	for _, remote := range []bool{false, true} {
		title := "Local Branch Operations"
		if remote {
			title = "Remote Branch Operations"
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
		ui.Text(c, "Repositories").Bold().TextColor(c.Theme().TextMuted)
		ui.TextInput(c.Key("repository-search"), &a.repositoryQuery).Bind(&a.repositorySearchHandle).Placeholder("Search Repositories").Label("Search Repositories")
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
						ui.ButtonBase(c.Key("repository-disclosure")).Width(18).Height(22).Label("Expand repository " + repo.Name).OnClick(func() { a.toggleRepositoryTree(repo) }).Children(func() { ui.Text(c, arrow) })
						name := ui.ButtonBase(c.Key("repository-select")).Grow(1).MinWidth(0).Justify(ui.Start).Height(22).Label(repo.Name).Tooltip(repositoryStateTooltip(repo))
						if a.repositoryPath == repo.Path {
							name.Background(ideaSelection).TextColor(c.Theme().Text).Bold()
						}
						treeContentAction(name, func() { a.selectRepository(repo) }, func() { a.toggleRepositoryTree(repo) }).OnShortcut(0, ui.KeyRight, func() { *open = true; a.loadRepositoryFiles(repo, false) }).OnShortcut(0, ui.KeyLeft, func() { *open = false }).ContextMenu(func(m *ui.Menu) { a.repositoryMenu(m, repo) }).Children(func() {
							color := repositoryStateColor(repo, c.Theme().TextMuted)
							ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
								folderTypeIcon(c)
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
				text := "No repositories"
				if len(a.repos) > 0 {
					text = "No matching repositories"
				}
				ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
	})
}
