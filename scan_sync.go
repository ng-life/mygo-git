package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func scanCanonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		return canonical
	}
	return filepath.Clean(path)
}

// Called by the scanner worker only. Match the same depth and traversal boundaries
// as discover, even when a root is temporarily unreadable.
func pathInScanScope(path string, roots []string) bool {
	path = scanCanonicalPath(path)
	for _, root := range roots {
		root = scanCanonicalPath(root)
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		if rel == "." {
			return true
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) > 3 {
			continue
		}
		current, allowed := root, true
		for _, part := range parts {
			if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
				allowed = false // Stop at a parent repository, as discover does.
				break
			}
			current = filepath.Join(current, part)
			if part == ".git" || part == "node_modules" || part == "vendor" {
				allowed = false
				break
			}
			if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
				allowed = false
				break
			}
		}
		if allowed {
			return true
		}
	}
	return false
}

func reconcileScanEntries(existing, found []Entry, roots []string, incomplete bool) ([]Entry, int, int) {
	previous := map[string]Entry{}
	for _, entry := range existing {
		previous[scanCanonicalPath(entry.Path)] = entry
	}
	next := map[string]Entry{}
	added := 0
	for _, entry := range found {
		if old, ok := previous[entry.Path]; ok {
			entry.Name = old.Name
		} else {
			added++
		}
		next[entry.Path] = entry
	}
	if incomplete {
		for path, entry := range previous {
			if _, found := next[path]; !found && pathInScanScope(path, roots) {
				entry.Path = path
				next[path] = entry
			}
		}
	}
	removed := 0
	for path := range previous {
		if _, ok := next[path]; !ok {
			removed++
		}
	}
	entries := make([]Entry, 0, len(next))
	for _, entry := range next {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, added, removed
}

func (a *client) pruneRemovedRepositories(repos []Repository) {
	valid := map[string]bool{}
	for _, repo := range repos {
		valid[repo.Path] = true
	}
	var closeTabs []string
	for _, tab := range a.editorTabs {
		outside := tab.Tree.Repository.Path != "" && !valid[tab.Tree.Repository.Path]
		for _, repo := range tab.Review.Request.Repositories {
			if !valid[repo.Path] {
				outside = true
			}
		}
		if outside {
			closeTabs = append(closeTabs, tab.ID)
		}
	}
	for _, id := range closeTabs {
		a.closeEditor(id)
	}
	removed := false
	for _, repo := range a.repos {
		if !valid[repo.Path] {
			removed = true
			a.cancelRead("repository-files:" + repo.Path)
			delete(a.repositoryFiles, repo.Path)
			delete(a.selected, repo.Path)
		}
	}
	if !removed {
		return
	}
	if !valid[a.repositoryPath] {
		a.repositoryPath = ""
	}
	if !valid[a.workingFileTree.Repository.Path] {
		a.clearWorkingComparison()
	}
	a.closeCodeNavigation()
	a.closeReview()
	a.clearDetail()
	a.historyCache = nil
	trees := a.workingTrees[:0]
	for _, tree := range a.workingTrees {
		if valid[tree.Repository.Path] {
			trees = append(trees, tree)
		}
	}
	a.workingTrees = trees
	outgoing := a.outgoing[:0]
	for _, source := range a.outgoing {
		if valid[source.Repository.Path] {
			outgoing = append(outgoing, source)
		}
	}
	a.outgoing = outgoing
	for key := range a.workingSelection {
		path, _, _ := strings.Cut(key, "\x00")
		if !valid[path] {
			delete(a.workingSelection, key)
		}
	}
	if len(a.editorTabs) > 0 {
		a.restoreEditorContext()
	}
}
