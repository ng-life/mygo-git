package main

import (
	"context"
	"strings"
)

func (a *client) clearWorkingComparison() {
	a.cancelRead("working-file")
	a.workingFileGeneration++
	a.workingFileKey = ""
	a.workingComparison = FileComparison{}
	a.workingComparisonError = ""
	a.workingComparisonLoading = false
}
func (a *client) reloadWorkingTrees() { a.reloadWorkingTreesFor(nil) }
func (a *client) reloadWorkingTreesFor(affected []Repository) {
	a.reloadChangeView()
	a.workingGeneration++
	a.cancelRead("working")
	if a.tab != 1 {
		if a.tab != 0 {
			a.clearWorkingComparison()
		}
		a.workingLoading = false
		return
	}
	if a.workingSelection == nil {
		a.workingSelection = map[string]bool{}
	}
	a.workingLoading = true
	gen := a.workingGeneration
	repos := append([]Repository(nil), a.repos...)
	if affected != nil {
		known := map[string]bool{}
		for _, tree := range a.workingTrees {
			known[tree.Repository.Path] = true
		}
		wanted := map[string]bool{}
		for _, r := range a.workingPending {
			wanted[r.Path] = true
		}
		for _, r := range affected {
			wanted[r.Path] = true
		}
		repos = nil
		for _, r := range a.repos {
			if wanted[r.Path] || !known[r.Path] {
				repos = append(repos, r)
			}
		}
	}
	a.workingPending = append([]Repository(nil), repos...)
	a.startRead("working", func(ctx context.Context) func() {
		trees := readWorkingTreesContext(ctx, repos)
		outgoing := readOutgoingRepositories(ctx, repos)
		return func() {
			if gen != a.workingGeneration || a.tab != 1 {
				return
			}
			a.workingPending = nil
			previous := map[string]WorkingTree{}
			for _, tree := range a.workingTrees {
				previous[tree.Repository.Path] = tree
			}
			for _, tree := range trees {
				if old, ok := previous[tree.Repository.Path]; ok && (old.Head != tree.Head || old.Ref != tree.Ref) {
					for _, file := range old.Files {
						delete(a.workingSelection, workingFileKey(tree.Repository.Path, file.Path))
					}
				}
			}
			for _, tree := range trees {
				previous[tree.Repository.Path] = tree
			}
			trees = nil
			for _, r := range a.repos {
				if tree, ok := previous[r.Path]; ok {
					trees = append(trees, tree)
				}
			}
			a.workingTrees = trees
			a.reconcileChangelists()
			a.updateOutgoing(outgoing)
			if a.workingFileKey != "" {
				found := false
				for _, tree := range trees {
					for _, file := range tree.Files {
						if workingFileKey(tree.Repository.Path, file.Path) == a.workingFileKey {
							found = true
							for _, r := range repos {
								if r.Path == tree.Repository.Path {
									a.selectWorkingFile(tree, file)
									break
								}
							}
						}
					}
				}
				if !found {
					a.clearWorkingComparison()
				}
			}
			a.workingLoading = false
			valid := map[string]bool{}
			for _, tree := range trees {
				for _, file := range tree.Files {
					valid[workingFileKey(tree.Repository.Path, file.Path)] = true
				}
			}
			for key := range a.workingSelection {
				if !valid[key] {
					delete(a.workingSelection, key)
				}
			}
		}
	})
}
func (a *client) selectWorkingFile(tree WorkingTree, file WorkingFile) {
	a.selectWorkingFileView(tree, file, a.tab == 0)
}
func (a *client) selectWorkingFileView(tree WorkingTree, file WorkingFile, fileView bool) {
	editor := a.openWorkingEditor(tree, file, fileView)
	editorID := editor.ID
	if a.review.Open {
		a.closeReview()
	}
	a.clearWorkingComparison()
	a.clearComparison()
	a.workingFileKey = workingFileKey(tree.Repository.Path, file.Path)
	a.workingFileTree = tree
	a.workingFile = file
	a.workingComparisonLoading = true
	gen := a.workingFileGeneration
	snapshot := tree
	// The repository browser shows current contents, independent of HEAD blob limits or encoding.
	if fileView {
		snapshot.Head = ""
	}
	a.startRead("editor:"+editorID, func(ctx context.Context) func() {
		comparison, err := readWorkingComparisonContext(ctx, snapshot, file)
		return func() {
			if editor := a.editorByID(editorID); editor != nil {
				editor.Loading = false
				editor.setComparison(comparison)
				a.refreshEditorPreview(editorID)
				if err != nil {
					editor.Error = err.Error()
				}
			}
			if gen != a.workingFileGeneration || a.editorActive != editorID {
				return
			}
			a.workingComparisonLoading = false
			if err != nil {
				a.workingComparisonError = err.Error()
			} else {
				a.workingComparison = comparison
			}
		}
	})
}
func (a *client) toggleWorkingRepository(tree WorkingTree, on bool) {
	a.toggleWorkingFiles(tree.Repository.Path, tree.Files, on)
}

func (a *client) workingRequests(onlyRepository string) []CommitRequest {
	var requests []CommitRequest
	for _, tree := range a.workingTrees {
		if onlyRepository != "" && tree.Repository.Path != onlyRepository {
			continue
		}
		request := CommitRequest{Tree: tree, Amend: a.workingAmend}
		for _, file := range tree.Files {
			if a.workingSelection[workingFileKey(tree.Repository.Path, file.Path)] {
				request.Files = append(request.Files, file)
			}
		}
		if len(request.Files) > 0 {
			requests = append(requests, request)
		}
	}
	return requests
}
func (a *client) commitWorking(onlyRepository string) {
	if a.busy || a.workingLoading || strings.TrimSpace(a.commitMessage) == "" {
		return
	}
	requests := a.workingRequests(onlyRepository)
	if len(requests) == 0 {
		return
	}
	a.beginWorkingCommit(requests, a.commitMessage, nil)
}
