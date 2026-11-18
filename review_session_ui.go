package main

import (
	"github.com/egoist/mygo/ui"
	"strings"
)

func (a *client) reviewDialog(c *ui.Context) {
	if !a.review.Dialog {
		return
	}
	ui.Modal(c.Key("review-dialog"), &a.review.Dialog, func() {
		ui.Column(c).Width(680).Gap(10).Children(func() {
			ui.Text(c, "Code Review").Bold().FontSize(18)
			ui.Textf(c, "Scope: %d repositories (from the current repository or branch selection)", len(a.review.DialogRepositories))
			ui.Row(c).Gap(6).Children(func() {
				for _, mode := range []struct{ key, label string }{{"workspace", "Base → Worktree"}, {"merge-base", "Merge Base → Target"}, {"direct", "Base → Target"}, {"commits", "Selected Commits"}} {
					ui.Button(c.Key(mode.key), mode.label).Selected(a.review.Kind == mode.key).OnClick(func() {
						a.review.Kind = mode.key
						if mode.key == "workspace" {
							a.review.Net = true
						}
					})
				}
			})
			if a.review.Kind != "commits" {
				ui.TextInput(c.Key("review-base"), &a.review.Base).Label("Review base").Placeholder("Base branch / SHA, e.g. origin/main")
			}
			if a.review.Kind == "direct" || a.review.Kind == "merge-base" {
				ui.TextInput(c.Key("review-target"), &a.review.Target).Label("Review target").Placeholder("Target branch / SHA")
			}
			if a.review.Kind == "commits" {
				ui.Textf(c, "%d commits selected; Net Diff includes all commits between endpoints", len(a.activeCommits))
			}
			ui.Row(c).Gap(6).Children(func() {
				ui.Button(c, "Net Diff").Selected(a.review.Net).OnClick(func() { a.review.Net = true })
				ui.Button(c, "Per Commit").Disabled(a.review.Kind == "workspace").Selected(!a.review.Net).OnClick(func() { a.review.Net = false })
			})
			ui.Text(c, "Merge Base mode shows changes introduced by the target branch. Direct comparison shows the final content difference between revisions. Worktree includes untracked files. Refs are resolved per repository; failures are reported separately.").FontSize(12)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { a.review.Dialog = false })
				ui.Button(c, "Start Review").Disabled((a.review.Kind == "commits" && len(a.activeCommits) == 0) || (a.review.Kind != "commits" && strings.TrimSpace(a.review.Base) == "") || ((a.review.Kind == "direct" || a.review.Kind == "merge-base") && strings.TrimSpace(a.review.Target) == "")).OnClick(func() {
					request := ReviewRequest{Kind: a.review.Kind, Base: a.review.Base, Target: a.review.Target, Net: a.review.Net, Repositories: append([]Repository(nil), a.review.DialogRepositories...)}
					if request.Kind == "commits" {
						request.Commits = append([]Commit(nil), a.activeCommits...)
						request.Repositories = reviewCommitRepositories(request.Commits)
					}
					a.beginReview(request)
				})
			})
		})
	})
}
func reviewCommitRepositories(commits []Commit) []Repository {
	seen := map[string]bool{}
	var repos []Repository
	for _, c := range commits {
		if !seen[c.Repository.Path] {
			repos = append(repos, c.Repository)
			seen[c.Repository.Path] = true
		}
	}
	return repos
}
func (a *client) reviewPanel(c *ui.Context) {
	if a.compare.Active {
		a.comparePanel(c)
		return
	}
	a.reviewFilesPanel(c)
}
func (a *client) reviewFilesPanel(c *ui.Context) {
	panel := ui.Column(c).Padding(6).Gap(4)
	if a.compare.Active {
		panel.Grow(1).FillWidth().MinHeight(0)
	} else {
		panel.Fill()
	}
	panel.Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			read := 0
			for _, item := range a.review.Items {
				if a.isReviewed(item) {
					read++
				}
			}
			ui.Textf(c, "Review · %d / %d reviewed", read, len(a.review.Items)).Bold()
			ui.Button(c, "Unreviewed Only").Selected(a.review.UnreadOnly).OnClick(a.toggleUnreadOnly)
			if a.review.Request.Kind != "workspace" && !a.compare.Active {
				ui.Button(c, "Net Diff").Selected(a.review.Request.Net).OnClick(func() { r := a.review.Request; r.Net = true; a.beginReview(r) })
				ui.Button(c, "Per Commit").Selected(!a.review.Request.Net).OnClick(func() { r := a.review.Request; r.Net = false; a.beginReview(r) })
			}
			ui.Spacer(c)
			ui.Button(c, "Refresh Review").OnClick(func() {
				if a.compare.Active {
					a.beginReview(a.compare.Request)
				} else {
					a.beginReview(a.review.Request)
				}
			})
			ui.Button(c, "Close Review").OnClick(a.closeReviewEditors)
		})
		if a.review.Request.Kind == "commits" && a.review.Request.Net {
			ui.Text(c, "Net Diff includes all changes from the first parent of the earliest selected commit to the latest selected commit, including unselected intervening commits.").FontSize(11)
		}
		if (a.review.Request.Kind == "direct" || a.review.Request.Kind == "merge-base") && !a.review.Request.Net {
			ui.Text(c, "Per Commit mode shows commits reachable from the target but not the base. Each commit is compared with its first parent; this differs from Net Diff between revisions.").FontSize(11)
		}
		if a.review.Loading {
			ui.Text(c, "Loading comparison range and file content…")
			return
		}
		if a.review.Summary != "" {
			ui.Scroll(c.Key("review-summary")).MaxHeight(72).Children(func() { ui.Text(c, a.review.Summary).FontSize(11).Selectable() })
		}
		if a.review.Error != "" {
			ui.Text(c, a.review.Error).FontSize(12)
		}
		items := a.reviewVisibleItems()
		if len(items) == 0 {
			ui.Text(c, "No files to review in the current scope")
		}
		ui.Scroll(c).TrackScroll(func() *ui.ScrollState {
			if tab := a.editorContext(); tab != nil {
				return &tab.ReviewScroll
			}
			return nil
		}()).Grow(1).Children(func() {
			byRepo := map[string][]WorkingFile{}
			repos := map[string]Repository{}
			lookup := map[string]ReviewItem{}
			var paths []string
			for _, item := range items {
				r := item.File.Repository
				if _, ok := repos[r.Path]; !ok {
					paths = append(paths, r.Path)
				}
				repos[r.Path] = r
				byRepo[r.Path] = append(byRepo[r.Path], WorkingFile{Path: item.File.Path})
				lookup[item.File.Key()] = item
			}
			for _, path := range paths {
				open := a.referenceOpen("review-repository:" + path)
				ui.Tree(c.Key(path), func() {
					treeContentAction(folderTreeItem(c.Key("repository"), repos[path].Name, open, func() { a.reviewNodes(c, workingHierarchy(byRepo[path]), path, lookup) }).Label("Review repository "+repos[path].Name), nil, func() { *open = !*open })
				})
			}
		})
	})
}
func (a *client) reviewNodes(c *ui.Context, node *workingNode, repo string, lookup map[string]ReviewItem) {
	for _, child := range node.Children {
		if child.File == nil {
			key := "review-folder:" + repo + "/" + child.Path
			if a.expanded == nil {
				a.expanded = map[string]*bool{}
			}
			if a.expanded[key] == nil {
				v := true
				a.expanded[key] = &v
			}
			open := a.expanded[key]
			treeContentAction(folderTreeItem(c.Key(repo+child.Path), child.Name, open, func() { a.reviewNodes(c, child, repo, lookup) }).Label(child.Name), nil, func() { *open = !*open })
			continue
		}
		item := lookup[repo+"\x00"+child.File.Path]
		ui.Row(c.Key(item.File.Key())).Gap(8).Children(func() {
			label := "Unreviewed"
			if a.isReviewed(item) {
				label = "Reviewed"
			}
			ui.Button(c, label).Selected(a.isReviewed(item)).Disabled(item.Fingerprint == "").Label("Mark as Reviewed " + item.File.Path).OnClick(func() { a.toggleReviewMark(item) })
			fileTypeIcon(c, item.File.Path)
			ui.Button(c, child.Name).Selected(a.selectedFile == item.File.Key()).Label("Review file " + item.File.Path).Tooltip(item.File.Path).OnClick(func() { a.selectReviewItem(item) })
		})
	}
}

func reviewAfterLabel(hash string) string {
	if hash == "" {
		return "Worktree"
	}
	return shortHash(hash)
}
