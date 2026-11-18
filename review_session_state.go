package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func (a *client) beginReview(request ReviewRequest) {
	a.openReviewEditor(request)
	request.Repositories = append([]Repository(nil), request.Repositories...)
	request.Commits = append([]Commit(nil), request.Commits...)
	a.initializeCompare(request)
	previousFile := a.selectedFile
	a.clearWorkingComparison()
	a.clearComparison()
	a.review.Generation++
	gen := a.review.Generation
	a.review.Open = true
	a.review.Dialog = false
	a.review.Loading = true
	a.review.Request = request
	a.review.Scope = reviewScope(request)
	a.review.Error = ""
	a.review.Items = nil
	a.saveEditorContext()
	compareLimit := a.compare.Limit
	a.startRead("review", func(ctx context.Context) func() {
		snapshot := pinCompareEndpoints(ctx, request)
		items, summary, problem := readReview(ctx, snapshot)
		var comparison CompareResult
		if request.Kind != "commits" && request.Kind != "workspace" {
			comparison = readCompare(ctx, snapshot, compareLimit)
		}
		return func() {
			if gen != a.review.Generation || !a.review.Open {
				return
			}
			if request.Kind != "commits" {
				a.compare.Result = comparison
				a.compare.Loading = false
			}
			a.review.Loading = false
			a.review.Items = items
			a.review.Summary = summary
			a.review.Error = problem
			a.saveEditorContext()
			changed := false
			for _, item := range items {
				key := a.reviewMarkKey(item)
				if old, ok := a.reviewMarks[key]; ok && old != item.Fingerprint {
					delete(a.reviewMarks, key)
					changed = true
				}
			}
			if problem == "" {
				present := map[string]bool{}
				for _, item := range items {
					present[a.reviewMarkKey(item)] = true
				}
				for key := range a.reviewMarks {
					if strings.HasPrefix(key, a.review.Scope+"/") && !present[key] {
						delete(a.reviewMarks, key)
						changed = true
					}
				}
			}
			if changed {
				a.persistReviewMarks()
			}
			for _, item := range a.reviewVisibleItems() {
				if item.File.Key() == previousFile {
					a.selectReviewItem(item)
					break
				}
			}
		}
	})
}
func (a *client) closeReview() {
	a.compare.Active = false
	a.compare.Generation++
	a.cancelRead("compare-commits")
	a.cancelRead("review")
	a.review.Generation++
	a.review.Open = false
	a.clearComparison()
}
func (a *client) openReviewDialog(repos []Repository, target string) {
	a.review.Dialog = true
	a.review.Net = true
	a.review.DialogRepositories = append([]Repository(nil), repos...)
	if a.review.Base == "" {
		a.review.Base = "HEAD"
	}
	a.review.Target = target
	if target == "" {
		a.review.Kind = "workspace"
	} else {
		a.review.Kind = "merge-base"
	}
}
func (a *client) reviewMarkKey(item ReviewItem) string { return a.review.Scope + "/" + item.File.Key() }
func (a *client) isReviewed(item ReviewItem) bool {
	return item.Fingerprint != "" && a.reviewMarks[a.reviewMarkKey(item)] == item.Fingerprint
}
func (a *client) reviewVisibleItems() []ReviewItem {
	var out []ReviewItem
	for _, item := range a.review.Items {
		if a.compare.Active {
			if q := strings.ToLower(strings.TrimSpace(a.compare.FileQuery)); q != "" && !strings.Contains(strings.ToLower(item.File.Path+" "+item.File.Repository.Name), q) {
				continue
			}
			if a.compare.Status != "" {
				found := false
				for _, change := range item.File.Changes {
					if strings.HasPrefix(change.File.Status, a.compare.Status) {
						found = true
					}
				}
				if !found {
					continue
				}
			}
		}
		if !a.review.UnreadOnly || !a.isReviewed(item) {
			out = append(out, item)
		}
	}
	return out
}
func (a *client) selectReviewItem(item ReviewItem) {
	parent := a.editorActive
	if current := a.activeEditor(); current != nil && current.Parent != "" {
		parent = current.Parent
	}
	editor := a.openCommitEditor(item.File, parent)
	editor.setParts(item.Parts)
	editor.Loading = false
	a.refreshEditorPreview(editor.ID)
	a.clearWorkingComparison()
	a.clearComparison()
	a.selectedFile = item.File.Key()
	a.comparisons = item.Parts
}
func (a *client) toggleReviewMark(item ReviewItem) {
	if item.Fingerprint == "" {
		return
	}
	if a.reviewMarks == nil {
		a.reviewMarks = map[string]string{}
	}
	key := a.reviewMarkKey(item)
	if a.isReviewed(item) {
		delete(a.reviewMarks, key)
	} else {
		a.reviewMarks[key] = item.Fingerprint
	}
	a.persistReviewMarks()
	if a.review.UnreadOnly && a.isReviewed(item) && a.selectedFile == item.File.Key() {
		visible := a.reviewVisibleItems()
		if len(visible) > 0 {
			a.selectReviewItem(visible[0])
		} else {
			a.clearComparison()
		}
	}
}
func (a *client) persistReviewMarks() {
	snapshot := map[string]string{}
	for k, v := range a.reviewMarks {
		snapshot[k] = v
	}
	path := a.reviewMarksPath()
	version := a.reviewSaveVersion.Add(1)
	a.startRead("review-marks", func(ctx context.Context) func() {
		a.reviewSaveMu.Lock()
		var err error
		if a.reviewSaveVersion.Load() == version {
			err = saveReviewMarks(path, snapshot)
		}
		a.reviewSaveMu.Unlock()
		return func() {
			if err != nil {
				a.review.Error = "Failed to save review progress: " + err.Error()
			}
		}
	})
}
func (a *client) reviewMarksPath() string {
	if a.reviewProgressPath != "" {
		return a.reviewProgressPath
	}
	return filepath.Join(filepath.Dir(configFile()), "review-progress.json")
}
func loadReviewMarks(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var marks map[string]string
	err = json.Unmarshal(data, &marks)
	return marks, err
}
func saveReviewMarks(path string, marks map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(marks)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "review-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (a *client) toggleUnreadOnly() {
	a.review.UnreadOnly = !a.review.UnreadOnly
	if !a.review.UnreadOnly {
		return
	}
	visible := a.reviewVisibleItems()
	for _, item := range visible {
		if item.File.Key() == a.selectedFile {
			return
		}
	}
	if len(visible) > 0 {
		a.selectReviewItem(visible[0])
	} else {
		a.clearComparison()
	}
}
