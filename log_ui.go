package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func logVisibleIndices(commits []Commit, query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	var result []int
	for i, commit := range commits {
		if query == "" || strings.Contains(strings.ToLower(commit.Subject+" "+commit.Hash+" "+commit.Author+" "+commit.Repository.Name), query) {
			result = append(result, i)
		}
	}
	return result
}
func (a *client) clickLogCommit(position int, visible []int, mods ui.Modifiers) {
	if position < 0 || position >= len(visible) {
		return
	}
	index := visible[position]
	if mods&ui.Shift == 0 {
		a.clickCommit(index, mods)
		return
	}
	anchor := position
	for i, original := range visible {
		if original == a.commitAnchor {
			anchor = i
			break
		}
	}
	if mods&(ui.Ctrl|ui.Super) == 0 {
		a.commitSelection.Clear()
	}
	lo, hi := min(anchor, position), max(anchor, position)
	for i := lo; i <= hi; i++ {
		a.commitSelection.Add(a.commits[visible[i]].Key())
	}
	a.logIndex = index
	var selected []Commit
	for _, commit := range a.commits {
		if a.commitSelection.Has(commit.Key()) {
			selected = append(selected, commit)
		}
	}
	a.selectCommits(selected)
}
func (a *client) logPanel(c *ui.Context) {
	t := c.Theme()
	visible := logVisibleIndices(a.commits, a.logQuery)
	commits := make([]Commit, 0, len(visible))
	for _, i := range visible {
		commits = append(commits, a.commits[i])
	}
	graph, graphWidth := a.cachedLogGraph(commits)
	ui.Column(c).Fill().Gap(0).Background(ideaPanel).Children(func() {
		ui.Row(c).Height(32).Padding(3, 8).Gap(8).AlignItems(ui.Center).Background(ideaToolbar).Children(func() {
			ui.Text(c, "Log").FontSize(12).Bold()
			ui.TextInput(c.Key("log-query"), &a.logQuery).Bind(&a.logSearchHandle).Label("Search Commits").Placeholder("Text or hash / Author / Repository").Width(230).Height(25)
			scope := "Current Branch"
			if a.group != "" {
				scope = strings.TrimPrefix(a.group, "/")
			}
			scopeLabel := "Branch: " + scope
			if a.tab == 2 && len(a.refSelection) > 1 {
				scopeLabel = fmt.Sprintf("Refs: %d selected", len(a.refSelection))
			}
			ui.Text(c, scopeLabel).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).Tooltip(scope)
			ui.Spacer(c)
			if len(a.activeCommits) > 0 {
				ui.Button(c.Key("clear-commits"), "Deselect All").OnClick(func() { a.commitSelection.Clear(); a.selectCommits(nil) })
			}
			ui.Button(c.Key("reload-log"), "Refresh Log").OnClick(a.reloadHistory)
		})
		if a.historyLoading {
			ui.Text(c, "Loading commits…").FontSize(11).Padding(4, 8).TextColor(t.TextMuted)
		}
		if a.historyError != "" {
			ui.Text(c, a.historyError).FontSize(11).Padding(4, 8)
		}
		if !a.historyLoading && len(commits) == 0 {
			text := "Select a repository or branch to view commit history"
			if a.logQuery != "" {
				text = "No matching commits"
			}
			ui.Text(c, text).Padding(6, 8).TextColor(t.TextMuted)
		}
		table := func() { a.logTable(c, commits, visible, graph, graphWidth) }
		if len(a.activeCommits) > 0 {
			if a.logFilesSplit == 0 {
				a.logFilesSplit = 65
			}
			ui.SplitPercent(c.Key("log-files-split"), &a.logFilesSplit, table, func() { a.filesPanel(c) }).Grow(1).FillWidth().MinHeight(0).SplitReset(65)
		} else {
			ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(table)
		}
		ui.Text(c, fmt.Sprintf("%d / %d commits · %d selected · Ctrl / ⌘ to toggle · Shift for range · Up to 200 per repository", len(commits), len(a.commits), len(a.activeCommits))).FontSize(10).TextColor(t.TextMuted).Height(20).Padding(3, 8)
	})
}
func (a *client) logTable(c *ui.Context, commits []Commit, visible []int, graph []logGraphRow, graphWidth float32) {
	a.logRows.Selected = nil
	a.logRows.Selection = nil
	a.logRows.Key = func(i int) any { return commits[i].Key() }
	ui.Table(c.Key("commits"), &a.logRows, []ui.TableColumn{
		{Title: "Date", Width: 108}, {Title: "Author", Width: 78}, {Title: "Graph", Width: max(float32(62), graphWidth)}, {Title: "Repository", Width: 100}, {Title: "Commit Message", MinWidth: 220}, {Title: "Commit", Width: 78},
	}, len(commits), func(row, col int) {
		commit := commits[row]
		text := ""
		switch col {
		case 0:
			text = time.Unix(commit.Timestamp, 0).Format("01-02 15:04")
		case 1:
			text = commit.Author
		case 2:
			text = "Commit graph " + shortHash(commit.Hash)
		case 3:
			text = commit.Repository.Name
		case 4:
			text = commit.Subject
		case 5:
			text = shortHash(commit.Hash)
		}
		cell := ui.ButtonBase(c.Key("commit-cell")).Fill().Padding(0, 4).Justify(ui.Start).Label(text).Tooltip(commit.Author + "\n" + commit.Repository.Path + "\n" + commit.Hash)
		if a.commitSelection.Has(commit.Key()) {
			paddingX, paddingY := c.Theme().Space(2.5), c.Theme().Space(1.5)
			cell.TextColor(c.Theme().AccentText).Draw(func(p *ui.Painter, r ui.Rect) {
				r.X -= paddingX
				r.Y -= paddingY
				r.W += 2 * paddingX
				r.H += 2 * paddingY
				p.Fill(r, ideaSelection, 0)
			})
		}
		mods := cell.ClickModifiers()
		cell.OnClick(func() { a.clickLogCommit(row, visible, mods) }).Children(func() {
			if col == 2 {
				logGraphCell(c, graph[row])
				return
			}
			label := ui.Text(c, text).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0)
			if (col == 0 || col == 1 || col == 5) && !a.commitSelection.Has(commit.Key()) {
				label.TextColor(c.Theme().TextMuted)
			}
		})
	}).Fill().MinWidth(0)
}

func (a *client) filesPanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(0).Background(ideaPanel).Children(func() {
		ui.Row(c).Height(25).Padding(3, 8).Background(ideaToolbar).Children(func() { ui.Textf(c, "Changed Files · %d commits", len(a.activeCommits)).FontSize(12).Bold() })
		if a.detailLoading {
			ui.Text(c, "Combining file changes…").FontSize(11).Padding(6, 8)
			return
		}
		if a.detailError != "" {
			ui.Text(c, a.detailError).FontSize(11).Padding(4, 8)
		}
		if len(a.files) == 0 {
			ui.Text(c, "No file changes in selected commits").FontSize(11).Padding(6, 8).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Scroll(c.Key("history-files")).Grow(1).Children(func() { a.historyFileTree(c) })
		ui.Column(c).Padding(6, 8).Gap(3).MaxHeight(78).Background(ideaToolbar).Children(func() {
			if len(a.activeCommits) == 1 {
				commit := a.activeCommits[0]
				ui.Text(c, commit.Subject).FontSize(12).SingleLine().Ellipsis("…")
				ui.Text(c, commit.Author+" · "+shortHash(commit.Hash)).FontSize(11).TextColor(c.Theme().TextMuted)
			} else {
				ui.Textf(c, "Combined view: %d commits, %d files", len(a.activeCommits), len(a.files)).FontSize(11).TextColor(c.Theme().TextMuted)
			}
		})
	})
}
func (a *client) historyFileTree(c *ui.Context) {
	var repos []Repository
	grouped := map[string][]WorkingFile{}
	files := map[string]MergedFile{}
	for _, file := range a.files {
		if _, exists := grouped[file.Repository.Path]; !exists {
			repos = append(repos, file.Repository)
		}
		status := file.Status()
		if len(file.Changes) > 0 {
			status = file.Changes[0].File.Status
		}
		grouped[file.Repository.Path] = append(grouped[file.Repository.Path], WorkingFile{Path: file.Path, Status: status})
		files[file.Key()] = file
	}
	ui.Tree(c.Key("history-file-tree"), func() {
		for _, repo := range repos {
			open := a.referenceOpen("history-files:" + repo.Path)
			label := fmt.Sprintf("%s  %d files", repo.Name, len(grouped[repo.Path]))
			treeContentAction(folderTreeItem(c.Key(repo.Path), label, open, func() { a.historyFileNodes(c, repo, workingHierarchy(grouped[repo.Path]).Children, files) }).Label("Changed repository "+repo.Name), nil, func() { *open = !*open })
		}
	})
}
func (a *client) historyFileNodes(c *ui.Context, repo Repository, nodes []*workingNode, files map[string]MergedFile) {
	for _, node := range nodes {
		if node.File == nil {
			open := a.referenceOpen("history-folder:" + repo.Path + "\x00" + node.Path)
			treeContentAction(folderTreeItem(c.Key(node.Path), node.Name, open, func() { a.historyFileNodes(c, repo, node.Children, files) }).Label("Changed directory "+node.Path).Tooltip(node.Path), nil, func() { *open = !*open })
			continue
		}
		file := files[workingFileKey(repo.Path, node.File.Path)]
		color := workingStatusColor(*node.File, c.Theme().TextMuted)
		if a.selectedFile == file.Key() {
			color = c.Theme().AccentText
		}
		item := fileTreeItem(c.Key(node.Path), node.Name, file.Path).Label(file.Path).Tooltip(repo.Path + "\n" + file.Path).Selected(a.selectedFile == file.Key()).TextColor(color)
		item.OnClick(func() { a.selectFile(file) })
	}
}
