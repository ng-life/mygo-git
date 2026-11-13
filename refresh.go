package main

import (
	"context"
	"sort"
	"strings"
)

type historyCacheEntry struct {
	Tips    string
	Commits []Commit
}

// Cache by resolved object IDs, never by mutable branch names alone.
func cachedHistory(ctx context.Context, sources []HistorySource, previous map[string]historyCacheEntry) ([]Commit, string, map[string]historyCacheEntry) {
	next := make(map[string]historyCacheEntry, len(sources))
	var commits []Commit
	var errors []string
	for _, source := range sources {
		if ctx.Err() != nil {
			break
		}
		key := source.Repository.Path + "\x00" + strings.Join(source.Refs, "\x00")
		var tips []string
		failed := false
		for _, ref := range source.Refs {
			tip, err := gitContext(ctx, source.Repository.Path, "rev-parse", "--verify", ref+"^{commit}")
			if err != nil {
				errors = append(errors, source.Repository.Name+": "+err.Error())
				failed = true
				break
			}
			tips = append(tips, tip)
		}
		if failed {
			continue
		}
		stamp := strings.Join(tips, "\x00")
		entry, ok := previous[key]
		if !ok || entry.Tips != stamp {
			rows, err := readHistoryContext(ctx, []HistorySource{{Repository: source.Repository, Refs: tips}})
			if err != "" {
				errors = append(errors, err)
				continue
			}
			entry = historyCacheEntry{Tips: stamp, Commits: rows}
		}
		next[key] = entry
		for _, row := range entry.Commits {
			row.Repository = source.Repository
			commits = append(commits, row)
		}
	}
	sort.SliceStable(commits, func(i, j int) bool { return commits[i].Timestamp > commits[j].Timestamp })
	return commits, strings.Join(errors, "\n"), next
}
func (a *client) cancelRead(lane string) {
	if id := a.readTaskIDs[lane]; id != 0 {
		a.tasks.finish(id, context.Canceled)
		delete(a.readTaskIDs, lane)
	}
	if cancel := a.readCancels[lane]; cancel != nil {
		cancel()
		delete(a.readCancels, lane)
	}
}
func (a *client) startRead(lane string, work func(context.Context) func()) {
	a.cancelRead(lane)
	if a.readCancels == nil {
		a.readCancels = map[string]context.CancelFunc{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.readCancels[lane] = cancel
	var taskID uint64
	if label := readTaskLabel(lane); label != "" && (a.win != nil || a.readRunner != nil) {
		taskID = a.tasks.begin(label, false)
		if a.readTaskIDs == nil {
			a.readTaskIDs = map[string]uint64{}
		}
		a.readTaskIDs[lane] = taskID
	}
	store := &a.tasks
	a.runRead(func() func() {
		apply := work(ctx)
		store.finish(taskID, ctx.Err())
		return func() {
			if ctx.Err() == nil {
				if a.readTaskIDs[lane] == taskID {
					delete(a.readTaskIDs, lane)
				}
				apply()
			}
		}
	})
}
func (a *client) mergeRepositories(updated []Repository) {
	for _, fresh := range updated {
		for i, old := range a.repos {
			if old.Path == fresh.Path {
				a.repos[i] = fresh
				break
			}
		}
	}
	a.refreshRepositoryFiles(updated)
	a.reloadWorkingTreesFor(updated)
}
func entriesFor(repos []Repository) []Entry {
	entries := make([]Entry, 0, len(repos))
	for _, r := range repos {
		entries = append(entries, Entry{Path: r.Path, Name: r.Name})
	}
	return entries
}
