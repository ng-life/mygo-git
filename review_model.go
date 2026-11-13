package main

import (
	"fmt"
	"strconv"
)

// Ranges are zero-based, end-exclusive; an insertion has an empty old range.
type DiffHunk struct{ OldStart, OldEnd, NewStart, NewEnd int }
type ReviewRow struct {
	OldLine, NewLine int
	Kind             byte
	Hunk             int
	OldEnd, NewEnd   int
}

func parseDiffHunks(patch string) []DiffHunk {
	var hunks []DiffHunk
	for _, match := range hunkPattern.FindAllStringSubmatch(patch, -1) {
		values := [4]int{}
		for side := 0; side < 2; side++ {
			start, _ := strconv.Atoi(match[side*2+1])
			count := 1
			if match[side*2+2] != "" {
				count, _ = strconv.Atoi(match[side*2+2])
			}
			if count > 0 {
				start--
			}
			values[side*2] = start
			values[side*2+1] = start + count
		}
		hunks = append(hunks, DiffHunk{values[0], values[1], values[2], values[3]})
	}
	return hunks
}
func (f *FileComparison) prepareReview() {
	f.ReviewRows = nil
	f.ReviewHunks = 0
	if len(f.Hunks) == 0 && f.Before != f.After && !f.IgnoreWhitespace {
		f.Hunks = []DiffHunk{{0, len(f.BeforeDocument.Lines), 0, len(f.AfterDocument.Lines)}}
	}
	oldN, newN := len(f.BeforeDocument.Lines), len(f.AfterDocument.Lines)
	for _, h := range f.Hunks {
		if h.OldStart < 0 || h.NewStart < 0 || h.OldEnd > oldN || h.NewEnd > newN || h.OldStart > h.OldEnd || h.NewStart > h.NewEnd {
			f.Hunks = []DiffHunk{{0, oldN, 0, newN}}
			break
		}
	}
	var blocks []DiffHunk
	for _, h := range f.Hunks {
		h.OldStart = max(0, h.OldStart-3)
		h.NewStart = max(0, h.NewStart-3)
		h.OldEnd = min(oldN, h.OldEnd+3)
		h.NewEnd = min(newN, h.NewEnd+3)
		if len(blocks) > 0 {
			last := &blocks[len(blocks)-1]
			if h.OldStart <= last.OldEnd && h.NewStart <= last.NewEnd {
				last.OldEnd = max(last.OldEnd, h.OldEnd)
				last.NewEnd = max(last.NewEnd, h.NewEnd)
				continue
			}
		}
		blocks = append(blocks, h)
	}
	old, new, nextHunk := 0, 0, 0
	for i, block := range blocks {
		if block.OldStart > old || block.NewStart > new {
			f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: 'f', OldLine: old, NewLine: new, OldEnd: block.OldStart, NewEnd: block.NewStart, Hunk: -1})
		}
		f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: 'h', OldLine: block.OldStart, NewLine: block.NewStart, OldEnd: block.OldEnd, NewEnd: block.NewEnd, Hunk: i})
		old, new = block.OldStart, block.NewStart
		for nextHunk < len(f.Hunks) {
			h := f.Hunks[nextHunk]
			if h.OldEnd > block.OldEnd || h.NewEnd > block.NewEnd {
				break
			}
			nextHunk++
			for old < h.OldStart && new < h.NewStart {
				f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: ' ', OldLine: old, NewLine: new, Hunk: i})
				old++
				new++
			}
			for old < h.OldEnd {
				f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: '-', OldLine: old, NewLine: -1, Hunk: i})
				old++
			}
			for new < h.NewEnd {
				f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: '+', OldLine: -1, NewLine: new, Hunk: i})
				new++
			}
		}
		for old < block.OldEnd && new < block.NewEnd {
			f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: ' ', OldLine: old, NewLine: new, Hunk: i})
			old++
			new++
		}
	}
	if old < oldN || new < newN {
		f.ReviewRows = append(f.ReviewRows, ReviewRow{Kind: 'f', OldLine: old, NewLine: new, OldEnd: oldN, NewEnd: newN, Hunk: -1})
	}
	f.ReviewHunks = len(blocks)
}
func reviewRows(f FileComparison, expanded map[int]bool) []ReviewRow {
	rows := make([]ReviewRow, 0, len(f.ReviewRows))
	for index, row := range f.ReviewRows {
		if row.Kind != 'f' || !expanded[index] {
			rows = append(rows, row)
			continue
		}
		for old, new := row.OldLine, row.NewLine; old < row.OldEnd && new < row.NewEnd; old, new = old+1, new+1 {
			rows = append(rows, ReviewRow{Kind: ' ', OldLine: old, NewLine: new, Hunk: -1})
		}
	}
	return rows
}
func hunkLabel(row ReviewRow) string {
	old, new := row.OldLine+1, row.NewLine+1
	if row.OldEnd == row.OldLine {
		old = row.OldLine
	}
	if row.NewEnd == row.NewLine {
		new = row.NewLine
	}
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", old, row.OldEnd-row.OldLine, new, row.NewEnd-row.NewLine)
}
