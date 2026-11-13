package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

type reviewState struct {
	Pairs      []reviewPair
	SideLists  [2]ui.ListState
	SideScroll [2]ui.ScrollState
	SharedY    float32
	SideBySide bool
	List       ui.ListState
	Expanded   map[int]bool
	Rows       []ReviewRow
	Hunk       int
	Full       bool
}

func newReviewState(f FileComparison) reviewState {
	s := reviewState{Expanded: map[int]bool{}}
	s.rebuildRows(f)
	return s
}
func (s *reviewState) moveHunk(direction int, total int) {
	next := s.Hunk + direction
	if next < 0 || next >= total {
		return
	}
	s.Hunk = next
	if s.SideBySide {
		for i, pair := range s.Pairs {
			if pair.Old != nil && pair.Old.Kind == 'h' && pair.Old.Hunk == next {
				s.SideLists[0].ScrollTo(i, ui.Start)
				s.SideLists[1].ScrollTo(i, ui.Start)
				return
			}
		}
	}
	for index, row := range s.Rows {
		if row.Kind == 'h' && row.Hunk == next {
			s.List.ScrollTo(index, ui.Start)
			return
		}
	}
}
func reviewComparison(c *ui.Context, f FileComparison, beforeTitle, afterTitle string, clients ...*client) {
	var beforeNav, afterNav CodeNavigation
	if len(clients) > 0 {
		beforeNav = CodeNavigation{Client: clients[0], Source: f.BeforeSource}
		afterNav = CodeNavigation{Client: clients[0], Source: f.AfterSource}
	}
	container := ui.Column(c).Grow(1).MinHeight(0)
	state := ui.Local(container, f.Version, func() reviewState { return newReviewState(f) })
	if len(clients) > 0 {
		state.SideBySide = clients[0].diffSideBySide
	}
	container.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			toggle := "完整文件"
			if state.Full {
				toggle = "变更块"
			}
			inline := ui.Button(c.Key("inline-mode"), "Inline").Selected(!state.SideBySide).OnClick(func() {
				state.SideBySide = false
				if len(clients) > 0 {
					clients[0].diffSideBySide = false
				}
			})
			side := ui.Button(c.Key("side-by-side-mode"), "Side-by-side").Selected(state.SideBySide).OnClick(func() {
				state.SideBySide = true
				if len(clients) > 0 {
					clients[0].diffSideBySide = true
				}
			})
			if state.SideBySide {
				side.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
			} else {
				inline.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
			}

			ui.Button(c.Key("diff-mode"), toggle).Label("切换 diff 模式").OnClick(func() { state.Full = !state.Full; state.rebuildRows(f) })
			language := f.AfterDocument.Language
			if language == "" {
				language = f.BeforeDocument.Language
			}
			if language == "" {
				language = "纯文本"
			}
			ui.Text(c, language).FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
			ui.Button(c.Key("previous-hunk"), "上一变更块").Disabled(state.Hunk <= 0 || f.ReviewHunks == 0).Tooltip("Shift+F7").OnClick(func() { state.moveHunk(-1, f.ReviewHunks) })
			ui.Textf(c, "%d / %d", min(state.Hunk+1, f.ReviewHunks), f.ReviewHunks).FontSize(11)
			ui.Button(c.Key("next-hunk"), "下一变更块").Disabled(state.Hunk+1 >= f.ReviewHunks).Tooltip("F7").OnClick(func() { state.moveHunk(1, f.ReviewHunks) })
		})
		container.OnShortcut(0, ui.KeyF7, func() { state.moveHunk(1, f.ReviewHunks) }).OnShortcut(ui.Shift, ui.KeyF7, func() { state.moveHunk(-1, f.ReviewHunks) })
		if state.SideBySide {
			reviewSideBySide(c, f, state, beforeTitle, afterTitle, beforeNav, afterNav)
			return
		}
		ui.Text(c, beforeTitle+" → "+afterTitle).FontSize(11).TextColor(c.Theme().TextMuted)
		if f.ReviewHunks == 0 && !state.Full {
			ui.Text(c, "无文本变更（可能仅文件名、权限或换行结尾变化）").TextColor(c.Theme().TextMuted)
			return
		}

		ui.List(c.Key("review-lines"), &state.List, len(state.Rows)).Grow(1).Selectable().RowWidth(max(f.BeforeDocument.Width, f.AfterDocument.Width) + 55).Rows(func(item ui.ListRow) {
			row := state.Rows[item.Index]
			if row.Kind == 'f' {
				source := -1
				for i, candidate := range f.ReviewRows {
					if candidate.Kind == 'f' && candidate.OldLine == row.OldLine && candidate.NewLine == row.NewLine {
						source = i
						break
					}
				}
				ui.Button(item.Context, fmt.Sprintf("展开 %d 行未修改内容", row.OldEnd-row.OldLine)).Height(25).OnClick(func() { state.Expanded[source] = true; state.rebuildRows(f) })
				return
			}
			if row.Kind == 'h' {
				ui.Text(item.Context, hunkLabel(row)).Unselectable().Height(25).Font("monospace").FontSize(11).Background(c.Theme().SurfaceHover)
				return
			}
			line := ui.Row(item.Context).Height(20).Gap(8).Padding(1, 6)
			switch row.Kind {
			case '+':
				line.Background(diffRowColor(c, true))
			case '-':
				line.Background(diffRowColor(c, false))
			}
			line.Children(func() {
				old, new := "", ""
				if row.OldLine >= 0 {
					old = fmt.Sprint(row.OldLine + 1)
				}
				if row.NewLine >= 0 {
					new = fmt.Sprint(row.NewLine + 1)
				}
				ui.Text(c, old).Unselectable().Width(42).Font("monospace").FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, new).Unselectable().Width(42).Font("monospace").FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, string(row.Kind)).Unselectable().Width(10).Font("monospace").FontSize(12)
				base := c.Theme().Text
				if row.Kind == '-' || row.Kind == '+' {
					base = diffCodeColor(c)
				}
				if row.Kind == '-' {
					codeLineWithNavigation(c, f.BeforeDocument, row.OldLine, base, &beforeNav)
				} else {
					codeLineWithNavigation(c, f.AfterDocument, row.NewLine, base, &afterNav)
				}
			})
		})
	})
}
func (a *client) reviewFilePosition() (index, total int) {
	index = -1
	if a.review.Open {
		items := a.reviewVisibleItems()
		for i, item := range items {
			if item.File.Key() == a.selectedFile {
				index = i
			}
		}
		return index, len(items)
	}
	if a.workingFileKey != "" {
		for _, tree := range a.workingReviewTrees() {
			for _, file := range orderedWorkingFiles(tree.Files) {
				if workingFileKey(tree.Repository.Path, file.Path) == a.workingFileKey {
					index = total
				}
				total++
			}
		}
	} else {
		total = len(a.files)
		for i, file := range a.files {
			if file.Key() == a.selectedFile {
				index = i
				break
			}
		}
	}
	return
}
func orderedWorkingFiles(files []WorkingFile) []WorkingFile {
	var out []WorkingFile
	var visit func(*workingNode)
	visit = func(node *workingNode) {
		if node.File != nil {
			out = append(out, *node.File)
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(workingHierarchy(files))
	return out
}
func (a *client) moveReviewFile(direction int) {
	index, total := a.reviewFilePosition()
	next := index + direction
	if index < 0 || next < 0 || next >= total {
		return
	}
	if a.review.Open {
		a.selectReviewItem(a.reviewVisibleItems()[next])
		return
	}
	if a.workingFileKey != "" {
		offset := 0
		for _, tree := range a.workingReviewTrees() {
			files := orderedWorkingFiles(tree.Files)
			if next < offset+len(files) {
				a.selectWorkingFile(tree, files[next-offset])
				return
			}
			offset += len(files)
		}
	} else {
		a.selectFile(a.files[next])
		a.fileIndex = next
		a.fileRows.ScrollIntoView(next)
	}
}
func (a *client) reviewFileNavigation(c *ui.Context) {
	index, total := a.reviewFilePosition()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Button(c.Key("previous-review-file"), "上一文件").Disabled(index <= 0).Tooltip("Shift+F8").OnClick(func() { a.moveReviewFile(-1) })
		ui.Textf(c, "文件 %d / %d", index+1, total).FontSize(11)
		if a.review.Open {
			for _, item := range a.review.Items {
				if item.File.Key() == a.selectedFile {
					label := "标记已读"
					if a.isReviewed(item) {
						label = "标记未读"
					}
					ui.Button(c.Key("review-mark-current"), label).Disabled(item.Fingerprint == "").OnClick(func() { a.toggleReviewMark(item) })
				}
			}
		}
		ui.Button(c.Key("next-review-file"), "下一文件").Disabled(index < 0 || index+1 >= total).Tooltip("F8").OnClick(func() { a.moveReviewFile(1) })
	})
	c.OnWindowShortcut(0, ui.KeyF8, func() { a.moveReviewFile(1) })
	c.OnWindowShortcut(ui.Shift, ui.KeyF8, func() { a.moveReviewFile(-1) })
}

func (s *reviewState) rebuildRows(f FileComparison) {
	defer func() { s.Pairs = pairReviewRows(s.Rows) }()
	if !s.Full {
		s.Rows = reviewRows(f, s.Expanded)
		return
	}
	expanded := map[int]bool{}
	for i, row := range f.ReviewRows {
		if row.Kind == 'f' {
			expanded[i] = true
		}
	}
	s.Rows = reviewRows(f, expanded)
}
