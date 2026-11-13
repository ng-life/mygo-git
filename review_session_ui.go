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
			ui.Text(c, "开始 Review").Bold().FontSize(18)
			ui.Textf(c, "范围：%d 个仓库（来自当前仓库或分支选择）", len(a.review.DialogRepositories))
			ui.Row(c).Gap(6).Children(func() {
				for _, mode := range []struct{ key, label string }{{"workspace", "基准 → 工作区"}, {"merge-base", "共同祖先 → 目标"}, {"direct", "基准 → 目标"}, {"commits", "所选提交"}} {
					ui.Button(c.Key(mode.key), mode.label).Selected(a.review.Kind == mode.key).OnClick(func() {
						a.review.Kind = mode.key
						if mode.key == "workspace" {
							a.review.Net = true
						}
					})
				}
			})
			if a.review.Kind != "commits" {
				ui.TextInput(c.Key("review-base"), &a.review.Base).Label("Review 基准").Placeholder("基准分支 / SHA，例如 origin/main")
			}
			if a.review.Kind == "direct" || a.review.Kind == "merge-base" {
				ui.TextInput(c.Key("review-target"), &a.review.Target).Label("Review 目标").Placeholder("目标分支 / SHA")
			}
			if a.review.Kind == "commits" {
				ui.Textf(c, "已选 %d 个提交；净差异包含端点间所有提交", len(a.activeCommits))
			}
			ui.Row(c).Gap(6).Children(func() {
				ui.Button(c, "净差异").Selected(a.review.Net).OnClick(func() { a.review.Net = true })
				ui.Button(c, "逐提交").Disabled(a.review.Kind == "workspace").Selected(!a.review.Net).OnClick(func() { a.review.Net = false })
			})
			ui.Text(c, "共同祖先模式用于查看目标分支引入的改动；直接比较展示两个版本最终内容之差。工作区包含未跟踪文件。多个仓库分别解析相同引用，失败单独报告。").FontSize(12)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(func() { a.review.Dialog = false })
				ui.Button(c, "开始审查").Disabled((a.review.Kind == "commits" && len(a.activeCommits) == 0) || (a.review.Kind != "commits" && strings.TrimSpace(a.review.Base) == "") || ((a.review.Kind == "direct" || a.review.Kind == "merge-base") && strings.TrimSpace(a.review.Target) == "")).OnClick(func() {
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
			ui.Textf(c, "Review · 已读 %d / %d", read, len(a.review.Items)).Bold()
			ui.Button(c, "只看未读").Selected(a.review.UnreadOnly).OnClick(a.toggleUnreadOnly)
			if a.review.Request.Kind != "workspace" && !a.compare.Active {
				ui.Button(c, "净差异").Selected(a.review.Request.Net).OnClick(func() { r := a.review.Request; r.Net = true; a.beginReview(r) })
				ui.Button(c, "逐提交").Selected(!a.review.Request.Net).OnClick(func() { r := a.review.Request; r.Net = false; a.beginReview(r) })
			}
			ui.Spacer(c)
			ui.Button(c, "刷新 Review").OnClick(func() {
				if a.compare.Active {
					a.beginReview(a.compare.Request)
				} else {
					a.beginReview(a.review.Request)
				}
			})
			ui.Button(c, "结束 Review").OnClick(a.closeReview)
		})
		if a.review.Request.Kind == "commits" && a.review.Request.Net {
			ui.Text(c, "净差异包含所选最早提交的第一父提交到最晚提交之间的全部变化，包括未选中的中间提交。").FontSize(11)
		}
		if (a.review.Request.Kind == "direct" || a.review.Request.Kind == "merge-base") && !a.review.Request.Net {
			ui.Text(c, "逐提交模式展示目标可达、基准不可达的提交，每条与第一父提交比较；与两个版本的净差异含义不同。").FontSize(11)
		}
		if a.review.Loading {
			ui.Text(c, "正在读取比较范围和文件内容…")
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
			ui.Text(c, "当前范围没有待审查文件")
		}
		ui.Scroll(c).Grow(1).Children(func() {
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
					treeContentAction(ui.TreeItem(c.Key("repository"), repos[path].Name, open, func() { a.reviewNodes(c, workingHierarchy(byRepo[path]), path, lookup) }).Label("Review 仓库 "+repos[path].Name), nil, func() { *open = !*open })
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
			treeContentAction(ui.TreeItem(c.Key(repo+child.Path), child.Name, open, func() { a.reviewNodes(c, child, repo, lookup) }).Label(child.Name), nil, func() { *open = !*open })
			continue
		}
		item := lookup[repo+"\x00"+child.File.Path]
		ui.Row(c.Key(item.File.Key())).Gap(8).Children(func() {
			label := "未读"
			if a.isReviewed(item) {
				label = "已读"
			}
			ui.Button(c, label).Selected(a.isReviewed(item)).Disabled(item.Fingerprint == "").Label("标记已读 " + item.File.Path).OnClick(func() { a.toggleReviewMark(item) })
			ui.Button(c, child.Name).Selected(a.selectedFile == item.File.Key()).Label("Review 文件 " + item.File.Path).Tooltip(item.File.Path).OnClick(func() { a.selectReviewItem(item) })
		})
	}
}

func reviewAfterLabel(hash string) string {
	if hash == "" {
		return "工作区"
	}
	return shortHash(hash)
}
