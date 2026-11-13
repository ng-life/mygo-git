package main

import (
	"context"
	"github.com/egoist/mygo/ui"
	"strings"
)

// Only snapshots enter background readers. Generations reject responses from
// an earlier repository, commit or file selection without blocking navigation.
func (a *client) runRead(work func() func()) {
	if a.readRunner != nil {
		a.readRunner(work)
		return
	}
	if a.win == nil || a.closed {
		return
	}
	win := a.win
	go func() {
		apply := work()
		win.Update(func() {
			if !a.closed {
				apply()
			}
		})
	}()
}
func (a *client) clearComparison() {
	a.cancelRead("file")
	a.fileGeneration++
	a.selectedFile = ""
	a.comparisons = nil
	a.comparisonLoading = false
}
func (a *client) clearDetail() {
	a.cancelRead("detail")
	a.detailGeneration++
	a.selectedCommit = ""
	a.details = nil
	a.files = nil
	a.activeCommits = nil
	a.detailError = ""
	a.detailLoading = false
	a.fileIndex = -1
	a.clearComparison()
}
func (a *client) reloadHistory() {
	if a.tab == 2 && len(a.refSelection) > 0 {
		a.pruneReferenceSelection()
		if len(a.refSelection) == 0 {
			a.group = ""
			a.branch = ""
			a.groupRemote = false
			a.groupTags = false
		}
	}
	a.historyGeneration++
	gen := a.historyGeneration
	a.historyError = ""

	repos := a.visible()
	selected := a.targets()
	if len(selected) > 0 {
		filtered := repos[:0]
		for _, r := range repos {
			if a.selected[r.Path] {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}
	sources := historySources(repos, a.group, a.groupRemote)
	if a.groupTags {
		sources = tagHistorySources(repos, a.group)
	}
	var scope []string
	for _, source := range sources {
		scope = append(scope, source.Repository.Path+"\x00"+strings.Join(source.Refs, "\x00"))
	}
	if a.tab == 2 && len(a.refSelection) > 0 {
		sources = a.selectedReferenceSources()
		scope = nil
		for _, source := range sources {
			scope = append(scope, source.Repository.Path+"\x00"+strings.Join(source.Refs, "\x00"))
		}
	}
	scopeKey := strings.Join(scope, "\n")
	if a.historyScope != scopeKey {
		a.commits = nil
		a.logIndex = -1
		a.commitAnchor = -1
		a.commitSelection.Clear()
		a.clearDetail()
		a.historyScope = scopeKey
	}
	a.cancelRead("history")
	a.historyLoading = len(sources) > 0
	if len(sources) == 0 {
		return
	}
	cache := a.historyCache
	a.startRead("history", func(ctx context.Context) func() {
		commits, err, next := cachedHistory(ctx, sources, cache)
		return func() {
			if gen != a.historyGeneration {
				return
			}
			a.commits = commits
			a.historyCache = next
			valid := map[string]bool{}
			for _, commit := range commits {
				valid[commit.Key()] = true
			}
			vanished := false
			for _, commit := range a.activeCommits {
				if !valid[commit.Key()] {
					a.commitSelection.Remove(commit.Key())
					vanished = true
				}
			}
			if vanished {
				var selected []Commit
				for _, commit := range commits {
					if a.commitSelection.Has(commit.Key()) {
						selected = append(selected, commit)
					}
				}
				a.selectCommits(selected)
			}
			a.logIndex = -1
			a.commitAnchor = -1
			for i, commit := range commits {
				if a.commitSelection.Has(commit.Key()) {
					a.logIndex = i
					a.commitAnchor = i
					break
				}
			}
			a.historyError = err
			a.historyLoading = false
		}
	})
}
func (a *client) selectCommit(commit Commit) {
	a.commitSelection.Clear()
	a.commitSelection.Add(commit.Key())
	a.selectCommits([]Commit{commit})
}
func (a *client) selectCommits(commits []Commit) {
	a.clearWorkingComparison()
	a.clearDetail()
	if len(commits) == 0 {
		return
	}
	a.activeCommits = append([]Commit(nil), commits...)
	a.selectedCommit = commits[0].Key()
	a.detailLoading = true
	gen := a.detailGeneration
	snapshot := append([]Commit(nil), commits...)
	a.startRead("detail", func(ctx context.Context) func() {
		details, files, err := readSelectedCommitsContext(ctx, snapshot)
		return func() {
			if gen != a.detailGeneration {
				return
			}
			a.detailLoading = false
			a.details = details
			a.files = files
			a.detailError = err
		}
	})
}
func (a *client) selectFile(file MergedFile) {
	a.clearWorkingComparison()
	a.clearComparison()
	a.selectedFile = file.Key()
	a.comparisonLoading = true
	gen := a.fileGeneration
	a.startRead("file", func(ctx context.Context) func() {
		comparisons := readMergedComparisonContext(ctx, file)
		return func() {
			if gen != a.fileGeneration {
				return
			}
			a.comparisonLoading = false
			a.comparisons = comparisons
		}
	})
}
func (a *client) clickCommit(index int, mods ui.Modifiers) {
	if index < 0 || index >= len(a.commits) {
		return
	}
	switch {
	case mods&ui.Shift != 0:
		anchor := a.commitAnchor
		if anchor < 0 || anchor >= len(a.commits) {
			anchor = index
		}
		if mods&(ui.Ctrl|ui.Super) == 0 {
			a.commitSelection.Clear()
		}
		lo, hi := anchor, index
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			a.commitSelection.Add(a.commits[i].Key())
		}
	case mods&(ui.Ctrl|ui.Super) != 0:
		key := a.commits[index].Key()
		if a.commitSelection.Has(key) {
			a.commitSelection.Remove(key)
		} else {
			a.commitSelection.Add(key)
		}
		a.commitAnchor = index
	default:
		a.commitSelection.Clear()
		a.commitSelection.Add(a.commits[index].Key())
		a.commitAnchor = index
	}
	a.logIndex = index
	var selected []Commit
	for _, commit := range a.commits {
		if a.commitSelection.Has(commit.Key()) {
			selected = append(selected, commit)
		}
	}
	a.selectCommits(selected)
}
func (a *client) switchTab(tab int) {
	if a.review.Open {
		a.closeReview()
	}
	a.clearWorkingComparison()
	a.refSelection = nil
	a.refAnchor = ""
	a.tab = tab
	a.repositoryPath = ""
	a.group = ""
	a.groupRemote = false
	a.groupTags = false
	a.branch = ""
	a.selected = map[string]bool{}
	a.query = ""
	a.reloadHistory()
	a.reloadWorkingTrees()
}
