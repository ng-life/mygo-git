package main

import (
	"github.com/egoist/mygo/ui"
	"hash/fnv"
)

type logGraphLine struct {
	From, To int
	Y1, Y2   float32
	Color    ui.Color
}
type logGraphRow struct {
	Lane     int
	Color    ui.Color
	Lines    []logGraphLine
	Boundary bool
}
type graphTip struct {
	Key   string
	Color ui.Color
}

var graphColors = []ui.Color{ui.Hex("#6897bb"), ui.Hex("#9876aa"), ui.Hex("#6a8759"), ui.Hex("#cc9c66"), ui.Hex("#7fa6a3"), ui.Hex("#b96e79")}

// Lanes refer to real (repository, SHA) identities. Missing parents are
// boundaries, never guessed links across truncated/filtered history or repos.
func buildLogGraph(commits []Commit) ([]logGraphRow, float32) {
	positions := map[string]int{}
	for i, commit := range commits {
		positions[commit.Key()] = i
	}
	rows := make([]logGraphRow, len(commits))
	var lanes []graphTip
	nextColor, maxLanes := 0, 1
	free := func() int {
		for i, tip := range lanes {
			if tip.Key == "" {
				return i
			}
		}
		lanes = append(lanes, graphTip{})
		return len(lanes) - 1
	}
	for i, commit := range commits {
		key := commit.Key()
		lane := -1
		for j, tip := range lanes {
			if tip.Key == key {
				lane = j
				break
			}
		}
		incoming := lane >= 0
		if lane < 0 {
			lane = free()
			lanes[lane] = graphTip{key, graphColors[nextColor%len(graphColors)]}
			nextColor++
		}
		row := logGraphRow{Lane: lane, Color: lanes[lane].Color}
		for j, tip := range lanes {
			if tip.Key != "" && j != lane {
				row.Lines = append(row.Lines, logGraphLine{j, j, 0, 1, tip.Color})
			}
		}
		if incoming {
			row.Lines = append(row.Lines, logGraphLine{lane, lane, 0, .5, row.Color})
		}
		lanes[lane] = graphTip{}
		for _, parent := range commit.Parents {
			parentKey := commit.Repository.Path + ":" + parent
			at, exists := positions[parentKey]
			if !exists || at <= i {
				row.Boundary = true
				continue
			}
			target := -1
			for j, tip := range lanes {
				if tip.Key == parentKey {
					target = j
					break
				}
			}
			if target < 0 {
				target = free()
				color := row.Color
				if target != lane {
					color = graphColors[nextColor%len(graphColors)]
					nextColor++
				}
				lanes[target] = graphTip{parentKey, color}
			}
			row.Lines = append(row.Lines, logGraphLine{lane, target, .5, 1, lanes[target].Color})
		}
		rows[i] = row
		maxLanes = max(maxLanes, len(lanes))
	}
	return rows, float32(maxLanes*12 + 16)
}
func logGraphCell(c *ui.Context, row logGraphRow) {
	// Capture immutable geometry only; elements expire between UI builds.
	paddingY := c.Theme().Space(1.5)
	ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
		r.Y -= paddingY
		r.H += 2 * paddingY
		x := func(lane int) float32 { return r.X + 8 + float32(lane)*12 }
		for _, line := range row.Lines {
			var path ui.Path
			x1, x2 := x(line.From), x(line.To)
			y1, y2 := r.Y+r.H*line.Y1, r.Y+r.H*line.Y2
			path.MoveTo(x1, y1)
			if x1 == x2 {
				path.LineTo(x2, y2)
			} else {
				path.CubeTo(x1, (y1+y2)/2, x2, (y1+y2)/2, x2, y2)
			}
			p.StrokePath(&path, 1, line.Color)
		}
		if row.Boundary {
			var tail ui.Path
			tail.MoveTo(x(row.Lane), r.Y+r.H/2+4).LineTo(x(row.Lane), r.Y+r.H/2+8)
			p.StrokePath(&tail, 1, row.Color)
		}
		var dot ui.Path
		dot.Circle(x(row.Lane), r.Y+r.H/2, 3)
		p.FillPath(&dot, row.Color)
	})
}

// Repaint and selection changes reuse geometry; no Git I/O or graph layout
// runs in draw callbacks, and only visible table rows create paint elements.
func (a *client) cachedLogGraph(commits []Commit) ([]logGraphRow, float32) {
	h := fnv.New64a()
	for _, commit := range commits {
		h.Write([]byte(commit.Key()))
		h.Write([]byte{0})
		for _, parent := range commit.Parents {
			h.Write([]byte(parent))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	key := h.Sum64()
	if key != a.logGraphKey || a.logGraphRows == nil {
		a.logGraphRows, a.logGraphWidth = buildLogGraph(commits)
		a.logGraphKey = key
	}
	return a.logGraphRows, a.logGraphWidth
}
