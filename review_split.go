package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
)

type reviewPair struct{ Old, New *ReviewRow }

func pairReviewRows(rows []ReviewRow) []reviewPair {
	var pairs []reviewPair
	for index := 0; index < len(rows); {
		row := rows[index]
		if row.Kind != '-' && row.Kind != '+' {
			copy := row
			pairs = append(pairs, reviewPair{Old: &copy, New: &copy})
			index++
			continue
		}
		var old, new []ReviewRow
		for index < len(rows) && rows[index].Kind == '-' {
			old = append(old, rows[index])
			index++
		}
		for index < len(rows) && rows[index].Kind == '+' {
			new = append(new, rows[index])
			index++
		}
		for i := 0; i < max(len(old), len(new)); i++ {
			pair := reviewPair{}
			if i < len(old) {
				copy := old[i]
				pair.Old = &copy
			}
			if i < len(new) {
				copy := new[i]
				pair.New = &copy
			}
			pairs = append(pairs, pair)
		}
	}
	return pairs
}
func (s *reviewState) syncSideScroll() {
	if s.SideScroll[0].Y != s.SharedY {
		s.SideScroll[1].Y = s.SideScroll[0].Y
	} else if s.SideScroll[1].Y != s.SharedY {
		s.SideScroll[0].Y = s.SideScroll[1].Y
	}
	s.SharedY = s.SideScroll[0].Y
}
func (s *reviewState) expandReview(f FileComparison, row ReviewRow) {
	for i, candidate := range f.ReviewRows {
		if candidate.Kind == 'f' && candidate.OldLine == row.OldLine && candidate.NewLine == row.NewLine {
			s.Expanded[i] = true
			break
		}
	}
	s.rebuildRows(f)
}
func reviewSideBySide(c *ui.Context, f FileComparison, s *reviewState, beforeTitle, afterTitle string, beforeNav, afterNav CodeNavigation) {
	s.syncSideScroll()
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Gap(8).Children(func() {
		for side := 0; side < 2; side++ {
			document, title, nav := f.BeforeDocument, beforeTitle, beforeNav
			if side == 1 {
				document, title, nav = f.AfterDocument, afterTitle, afterNav
			}
			ui.Column(c.Key(side)).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, title).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted)
				ui.List(c.Key("paired-diff"), &s.SideLists[side], len(s.Pairs)).Grow(1).Selectable().TrackScroll(&s.SideScroll[side]).RowWidth(document.Width).Rows(func(item ui.ListRow) {
					pair := s.Pairs[item.Index]
					row := pair.Old
					if side == 1 {
						row = pair.New
					}
					if row == nil {
						ui.Box(item.Context).Height(20)
						return
					}
					if row.Kind == 'f' {
						ui.Button(item.Context, fmt.Sprintf("Expand %d Unchanged Lines", row.OldEnd-row.OldLine)).Height(25).OnClick(func() { s.expandReview(f, *row) })
						return
					}
					if row.Kind == 'h' {
						ui.Text(item.Context, hunkLabel(*row)).Unselectable().Height(25).Font("monospace").FontSize(11).Background(c.Theme().SurfaceHover)
						return
					}
					lineNumber := row.OldLine
					if side == 1 {
						lineNumber = row.NewLine
					}
					base := c.Theme().Text
					line := ui.Row(item.Context).Height(20).Gap(10).Padding(1, 6)
					if row.Kind == '-' {
						line.Background(diffRowColor(c, false))
						base = diffCodeColor(c)
					}
					if row.Kind == '+' {
						line.Background(diffRowColor(c, true))
						base = diffCodeColor(c)
					}
					line.Children(func() {
						ui.Textf(c, "%4d", lineNumber+1).Unselectable().Font("monospace").FontSize(11)
						codeLineWithNavigation(c, document, lineNumber, base, &nav)
					})
				})
			})
		}
	})
}
