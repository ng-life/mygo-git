package main

import "github.com/egoist/mygo/ui"

var workingFolderIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path fill="currentColor" d="M1 3h5l2 2h7v9H1z"/></svg>`))
var workingFileIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path fill="none" stroke="currentColor" stroke-width="1.3" d="M3 1.5h6l4 4v9H3zM9 1.5v4h4M5 9h6M5 11.5h6"/></svg>`))

type workingChangeCounts struct{ Added, Modified, Deleted, Conflicted int }

func countWorkingChanges(trees []WorkingTree) workingChangeCounts {
	var counts workingChangeCounts
	for _, tree := range trees {
		for _, file := range tree.Files {
			switch workingChangeKind(file) {
			case "added":
				counts.Added++
			case "deleted":
				counts.Deleted++
			case "conflicted":
				counts.Conflicted++
			default:
				counts.Modified++
			}
		}
	}
	return counts
}
func (a *client) setWorkingCollapsed(collapsed bool) {
	if a.workingCollapsed == nil {
		a.workingCollapsed = map[string]bool{}
	}
	a.ensureChangelists()
	for _, list := range a.changelists.Lists {
		a.workingCollapsed["changelist:"+list.ID] = collapsed
		for _, tree := range a.changelistTrees(list.ID) {
			scope := a.workingTreeScope(tree)
			a.workingCollapsed[scope] = collapsed
			var visit func(*workingNode)
			visit = func(node *workingNode) {
				for _, child := range node.Children {
					if child.File == nil {
						a.workingCollapsed[workingFileKey(scope, child.Path)] = collapsed
						visit(child)
					}
				}
			}
			visit(workingHierarchy(tree.Files))
		}
	}
}
func (a *client) workingChangeSummary(c *ui.Context) {
	counts := countWorkingChanges(a.workingTrees)
	ui.Row(c).Gap(10).Children(func() {
		ui.Textf(c, "%d 新增", counts.Added).FontSize(11).TextColor(statusGreen)
		ui.Textf(c, "%d 修改", counts.Modified).FontSize(11).TextColor(statusBlue)
		ui.Textf(c, "%d 删除", counts.Deleted).FontSize(11).TextColor(c.Theme().TextMuted)
		if counts.Conflicted > 0 {
			ui.Textf(c, "%d 冲突", counts.Conflicted).FontSize(11).TextColor(statusOrange)
		}
	})
}
