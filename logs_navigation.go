package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func tagHistorySources(repos []Repository, group string) []HistorySource {
	var sources []HistorySource
	for _, repo := range repos {
		source := HistorySource{Repository: repo}
		for _, tag := range repo.Tags {
			key := "/" + tag
			if group == key || strings.HasPrefix(key, group+"/") {
				source.Refs = append(source.Refs, "refs/tags/"+tag)
			}
		}
		if len(source.Refs) > 0 {
			sources = append(sources, source)
		}
	}
	return sources
}
func referenceRepositories(repos []Repository, kind, query string) []Repository {
	copies := make([]Repository, 0, len(repos))
	query = strings.ToLower(strings.TrimSpace(query))
	for _, repo := range repos {
		values := repo.Branches
		if kind == "remote" {
			values = repo.RemoteBranches
		} else if kind == "tags" {
			values = repo.Tags
		}
		var matches []string
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				matches = append(matches, value)
			}
		}
		copy := repo
		copy.Branches = matches
		copy.RemoteBranches = nil
		copies = append(copies, copy)
	}
	return copies
}
func referenceTargets(repos []Repository, name, kind string) []Repository {
	if kind != "tags" {
		return branchTargets(repos, name, kind == "remote")
	}
	var targets []Repository
	for _, repo := range repos {
		for _, tag := range repo.Tags {
			if tag == name {
				targets = append(targets, repo)
				break
			}
		}
	}
	return targets
}
func (a *client) referenceOpen(key string) *bool {
	if a.expanded == nil {
		a.expanded = map[string]*bool{}
	}
	if a.expanded[key] == nil {
		v := true
		a.expanded[key] = &v
	}
	return a.expanded[key]
}
func (a *client) selectReference(key, name, kind string) {
	a.refSelection = nil
	a.refAnchor = ""
	if name != "" {
		choice := ReferenceChoice{Kind: kind, Name: name}
		if len(a.referenceRepositoriesFor(choice)) > 0 {
			a.refSelection = []ReferenceChoice{choice}
			a.refAnchor = choice.Key()
		}
	}
	a.selectReferenceHistory(key, name, kind)
}
func (a *client) selectReferenceHistory(key, name, kind string) {
	a.saveEditorContext()
	defer a.restoreEditorContext()
	if a.review.Open {
		a.closeReview()
	}
	a.clearWorkingComparison()
	a.repositoryPath = ""
	a.selected = map[string]bool{}
	a.group = key
	a.groupRemote = kind == "remote"
	a.groupTags = kind == "tags"
	a.branch = name
	if a.groupTags && name != "" {
		a.branch = "refs/tags/" + name
	}
	a.reloadHistory()
}
func (a *client) logsNavigation(c *ui.Context) {
	ui.Column(c).Fill().Padding(5, 4).Gap(4).Children(func() {
		ui.TextInput(c.Key("logs-search"), &a.logsQuery).Bind(&a.logsSearchHandle).Label("Search Branches or Tags").Placeholder("Branch or tag")
		ui.Scroll(c).Grow(1).Children(func() {
			head := ReferenceChoice{Kind: "head"}
			ui.Tree(c.Key("head-tree"), func() {
				item := ui.TreeItem(c.Key("logs-head"), "HEAD (Current Branch)", nil, nil).Label("HEAD (Current Branch)").Selected(a.referenceSelected(head))
				mods := item.ClickModifiers()
				item.OnClick(func() { a.clickReference(head, mods) }).ContextMenu(func(menu *ui.Menu) { a.referenceContext(head); a.referenceSelectionMenu(menu) })
			})
			for _, section := range []struct{ kind, title string }{{"local", "Local"}, {"remote", "Remote"}, {"tags", "Tags"}} {
				root := branchTree(referenceRepositories(a.repos, section.kind, a.logsQuery), false)
				ui.Tree(c.Key(section.kind), func() {
					open := a.referenceOpen("logs-section:" + section.kind)
					item := treeContentAction(ui.TreeItem(c.Key("section"), section.title, open, func() {
						if len(root.Children) == 0 {
							ui.Text(c, "No matching refs").FontSize(11).TextColor(c.Theme().TextMuted)
						}
						a.referenceNodes(c, root, section.kind)
					}).Label(section.title), nil, func() { *open = !*open })
					if section.kind == "tags" {
						item.ContextMenu(func(menu *ui.Menu) {
							if menu.Item("Fetch Tags · Remote Tags").Disabled(a.busy || len(a.repos) == 0).Chosen() {
								a.fetchTags(a.repos)
							}
							if menu.Item("New Tag…").Disabled(a.busy || len(a.repos) == 0).Chosen() {
								a.openTagDialog("create", "", a.repos)
							}
						})
					}
				})
			}
		})
		ui.Textf(c, "%d refs selected · Ctrl / ⌘ to toggle · Shift for range", len(a.refSelection)).FontSize(10).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			ui.Button(c, "Fetch Tags").Disabled(a.busy || len(a.repos) == 0).OnClick(func() { a.fetchTags(a.repos) })
			ui.Button(c, "New Tag…").Grow(1).Disabled(a.busy || len(a.repos) == 0).OnClick(func() { a.openTagDialog("create", "", a.visible()) })
		})
	})
}
func (a *client) referenceNodes(c *ui.Context, node *TreeNode, kind string) {
	for _, child := range node.Children {
		var open *bool
		var children func()
		if len(child.Children) > 0 {
			open = a.referenceOpen("logs:" + kind + child.Key)
			children = func() { a.referenceNodes(c, child, kind) }
		}
		label := fmt.Sprintf("%s  (%d)", child.Name, child.Count)
		accessible := label
		if kind == "remote" {
			accessible = "Remote " + label
		} else if kind == "tags" {
			accessible = "Tag " + label
		}
		display := label
		var names []string
		targets := referenceTargets(a.repos, child.Branch, kind)
		for _, repo := range targets {
			names = append(names, repo.Name)
		}
		if len(names) > 0 {
			display += " · " + strings.Join(names, ", ")
		}
		current := kind == "local" && currentLocalBranch(child.Branch, targets)
		if current {
			display += " · Current"
		}
		selected := a.group == child.Key && a.groupRemote == (kind == "remote") && a.groupTags == (kind == "tags")
		var toggle func()
		if open != nil {
			toggle = func() { *open = !*open }
		}
		choice := ReferenceChoice{Kind: kind, Name: child.Branch}
		if child.Branch != "" {
			selected = a.referenceSelected(choice)
		}
		row := referenceTreeItem(c.Key(kind+child.Key), display, open, children).Label(accessible).Selected(selected).Tooltip(strings.Join(names, "\n"))
		if current && !selected {
			row.Background(ui.Hex("#344653"))
		}
		mods := row.ClickModifiers()
		item := treeContentAction(row, func() {
			if child.Branch != "" {
				a.clickReference(choice, mods)
			} else {
				a.selectReference(child.Key, child.Branch, kind)
			}
		}, toggle)
		if child.Branch != "" {
			item.ContextMenu(func(menu *ui.Menu) { a.referenceContext(choice); a.referenceSelectionMenu(menu) })
		}
	}
}
