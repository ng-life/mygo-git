package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

type backgroundTask struct {
	ID                         uint64
	Label, Detail, Error       string
	Done, Total                int
	Write, Finished, Cancelled bool
}
type backgroundTasks struct {
	mu    sync.Mutex
	next  uint64
	items map[uint64]backgroundTask
}
type taskReporter func(detail string, done, total int, err error)

func (t *backgroundTasks) begin(label string, write bool) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.items == nil {
		t.items = map[uint64]backgroundTask{}
	}

	t.next++
	t.items[t.next] = backgroundTask{ID: t.next, Label: label, Write: write}
	return t.next
}
func (t *backgroundTasks) report(id uint64, detail string, done, total int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item, exists := t.items[id]
	if !exists || item.Finished {
		return
	}
	item.Detail = detail
	item.Total = max(0, total)
	item.Done = max(0, done)
	if total > 0 {
		item.Done = min(item.Done, total)
	}
	if err != nil {
		item.Error = detail + ": " + err.Error()
	}
	t.items[id] = item
}
func (t *backgroundTasks) finish(id uint64, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item, exists := t.items[id]
	if !exists || item.Finished {
		return
	}
	item.Finished = true
	if err != nil {
		item.Error = err.Error()
		item.Cancelled = err == context.Canceled
	}
	if item.Total > 0 && !item.Cancelled {
		item.Done = item.Total
	}
	t.items[id] = item
	t.pruneLocked()
}
func (t *backgroundTasks) snapshot() []backgroundTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	var result []backgroundTask
	for _, item := range t.items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result
}
func taskProgress(item backgroundTask) float64 {
	if item.Cancelled {
		if item.Total > 0 {
			return float64(item.Done) / float64(item.Total)
		}
		return 0
	}
	if item.Finished {
		return 1
	}
	if item.Total == 0 {
		return -1
	}
	return float64(item.Done) / float64(item.Total)
}
func taskState(item backgroundTask) string {
	if item.Cancelled {
		return "Canceled"
	}
	if item.Finished {
		if item.Error != "" {
			return "Completed with Errors"
		}
		return "Completed"
	}
	if item.Total > 0 {
		return fmt.Sprintf("%d / %d steps", item.Done, item.Total)
	}
	return "In Progress"
}
func readTaskLabel(lane string) string {
	labels := map[string]string{"change-view": "Load Changes", "history": "Load Commit History", "detail": "Load Commit Changes", "file": "Load File Diff", "review": "Compare Code", "compare-commits": "Compare Commit Sets", "working": "Load Working Tree", "working-file": "Load File Content", "symbol": "Go to Definition", "commit-message-history": "Load Commit Messages"}
	if strings.HasPrefix(lane, "preview:") {
		return "Render Document Preview"
	}
	if strings.HasPrefix(lane, "editor:") {
		return "Load Editor File"
	}
	if strings.HasPrefix(lane, "repository-files:") {
		return "Load Repository Files"
	}
	return labels[lane]
}
func inspectWithProgress(entries []Entry, report taskReporter, offset, total int) []Repository {
	var repos []Repository
	for i, entry := range entries {
		report("Read repository status · "+entry.Path, offset+i, total, nil)
		repo := inspect(entry)
		repos = append(repos, repo)
		var err error
		if repo.Error != "" {
			err = fmt.Errorf("%s", repo.Error)
		}
		report("Read repository status · "+repo.Name, offset+i+1, total, err)
	}
	return repos
}
func (a *client) backgroundTaskIndicator(c *ui.Context) {
	tasks := a.tasks.snapshot()
	var current *backgroundTask
	running := 0
	for i := range tasks {
		if !tasks[i].Finished {
			running++
			if current == nil || tasks[i].Write && !current.Write {
				current = &tasks[i]
			}
		}
	}
	ui.Row(c).Gap(6).Width(410).Shrink(0).AlignItems(ui.Center).Children(func() {
		if current != nil {
			item := *current
			ui.Text(c, item.Label+" · "+item.Detail).FontSize(10).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Tooltip(item.Label + "\n" + item.Detail)
			ui.Progress(c, taskProgress(item)).Width(90).Height(5).Label("Background Task Progress")
			ui.Text(c, taskState(item)).FontSize(10).Shrink(0)
		} else {
			ui.Spacer(c)
			if len(tasks) > 0 {
				last := tasks[0]
				for _, item := range tasks {
					if item.Write {
						last = item
						break
					}
				}
				color := c.Theme().TextMuted
				if last.Error != "" && !last.Cancelled {
					color = statusOrange
				}
				ui.Text(c, taskState(last)).FontSize(10).TextColor(color)
			}
		}
		ui.Button(c, fmt.Sprintf("Tasks (%d)", running)).Label("Background Tasks").Height(21).FontSize(10).OnClick(func() { a.taskPanelOpen = true })
	})
}
func (a *client) backgroundTasksPanel(c *ui.Context) {
	if !a.taskPanelOpen {
		return
	}
	ui.Modal(c.Key("background-tasks"), &a.taskPanelOpen, func() {
		ui.Column(c).Width(660).MaxHeight(550).Gap(8).Children(func() {
			ui.Text(c, "Background Tasks").FontSize(16).Bold()
			tasks := a.tasks.snapshot()
			if len(tasks) == 0 {
				ui.Text(c, "No background tasks").FontSize(12)
			}
			ui.Scroll(c).MaxHeight(440).Gap(10).Children(func() {
				for _, item := range tasks {
					ui.Column(c.Key(item.ID)).Gap(3).Children(func() {
						ui.Row(c).Children(func() {
							ui.Text(c, item.Label).Bold().FontSize(12)
							ui.Spacer(c)
							ui.Text(c, taskState(item)).FontSize(11)
						})
						ui.Text(c, item.Detail).FontSize(11).SingleLine().Ellipsis("…").Tooltip(item.Detail)
						ui.Progress(c, taskProgress(item)).Height(5).FillWidth()
						if item.Error != "" && !item.Cancelled {
							ui.Text(c, item.Error).FontSize(11).TextColor(statusOrange)
						}
					})
				}
			})
			ui.Button(c, "Close").OnClick(func() { a.taskPanelOpen = false })
		})
	})
}

// Called with the mutex held; running tasks are never removed.
func (t *backgroundTasks) pruneLocked() {
	var finished []uint64
	for id, item := range t.items {
		if item.Finished {
			finished = append(finished, id)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i] < finished[j] })
	for len(finished) > 20 {
		delete(t.items, finished[0])
		finished = finished[1:]
	}
}
