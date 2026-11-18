package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func divergentCompareClient(t *testing.T) *client {
	t.Helper()
	path := testRepo(t)
	writeCommit(t, path, "base.txt", "base\n")
	mustGit(t, path, "checkout", "-b", "side-a")
	writeCommit(t, path, "shared.txt", "shared change\n")
	shared := mustGit(t, path, "rev-parse", "HEAD")
	writeCommit(t, path, "a.txt", "only a\n")
	mustGit(t, path, "checkout", "main")
	mustGit(t, path, "checkout", "-b", "side-b")
	mustGit(t, path, "cherry-pick", "--no-commit", shared)
	mustGit(t, path, "commit", "-m", "shared change picked with different message")
	writeCommit(t, path, "b.txt", "only b\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.readRunner = func(job func() func()) { job()() }
	a.writeRunner = func(job func() func()) { job()() }
	return a
}
func TestCompareCommitSetsAndEquivalentPatches(t *testing.T) {
	a := divergentCompareClient(t)
	request := ReviewRequest{Kind: "direct", Base: "refs/heads/side-a", Target: "refs/heads/side-b", Repositories: a.repos, Net: true}
	a.beginReview(request)
	result := a.compare.Result
	if result.Error != "" || len(result.Endpoints) != 1 || result.Endpoints[0].CountA != 2 || result.Endpoints[0].CountB != 2 || len(result.Commits) != 4 || len(result.Endpoints[0].Bases) != 1 {
		t.Fatalf("%+v", result)
	}
	equivalent := 0
	for _, entry := range result.Commits {
		if entry.Equivalent {
			equivalent++
		}
	}
	if equivalent != 2 {
		t.Fatal("patch-equivalent commits not recognized", result.Commits)
	}
	if len(a.review.Items) != 2 {
		t.Fatalf("final file diff should exclude equivalent changes: %+v", a.review.Items)
	}
	pinned := pinCompareEndpoints(context.Background(), request)
	oldA := pinned.Pinned[a.repos[0].Path][0]
	mustGit(t, a.repos[0].Path, "branch", "-f", "side-a", "main")
	stable := readCompare(context.Background(), pinned, 1)
	if stable.Endpoints[0].A != oldA || stable.Endpoints[0].CountA != 2 || len(stable.Commits) != 2 {
		t.Fatal("pinned comparison moved or pagination wrong", stable)
	}
	a.compare.Limit = 1
	a.reloadCompare()
	if a.compare.Result.Endpoints[0].A != oldA || a.compare.Result.Endpoints[0].CountA != 2 {
		t.Fatal("load-more drifted to moving refs")
	}
}
func TestCompareViewCommitReviewAndSwapUI(t *testing.T) {
	a := divergentCompareClient(t)
	a.beginReview(ReviewRequest{Kind: "direct", Base: "refs/heads/side-a", Target: "refs/heads/side-b", Repositories: a.repos, Net: true})
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText("Compare") || !tester.HasText("File Diff") {
		t.Fatal("Compare missing")
	}
	if err := tester.Click("Unique to Each"); err != nil {
		t.Fatal(err)
	}
	rows := a.compareCommits()
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	if err := tester.Click("Compare Commit " + rows[0].Commit.Key()); err != nil {
		t.Fatal(err)
	}
	if len(a.review.Items) != 1 || a.review.Request.Kind != "commits" {
		t.Fatalf("commit files not loaded %+v", a.review)
	}
	item := a.review.Items[0]
	if err := tester.Click("Review file " + item.File.Path); err != nil {
		t.Fatal(err)
	}
	if a.selectedFile == "" || len(a.comparisons) != 1 {
		t.Fatal("comparison not selected")
	}
	if dest := os.Getenv("MYGO_COMPARE_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		fresh := ui.NewTester(a.view, 1560, 940)
		if err = png.Encode(f, fresh.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := tester.Click("Swap Revisions"); err != nil {
		t.Fatal(err)
	}
	if a.compare.Request.Base != "refs/heads/side-b" || a.compare.Request.Target != "refs/heads/side-a" || a.compare.Mode != "files" {
		t.Fatal(a.compare.Request)
	}
	if err := tester.Click("Merge Base"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Merge base: " + a.compare.Result.Endpoints[0].Bases[0]) {
		t.Fatal("ancestor missing")
	}
}
func TestCompareWorktreeReverseWhitespaceAndLargeFiles(t *testing.T) {
	a := divergentCompareClient(t)
	path := a.repos[0].Path
	writeWorkingFile(t, path, "b.txt", "  only b \n")
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: a.repos, Net: true, IgnoreWhitespace: true})
	if a.review.Error != "" || len(a.review.Items) != 1 || a.review.Items[0].Parts[0].Comparison.ReviewHunks != 0 {
		t.Fatal(a.review.Error, a.review.Items)
	}
	writeWorkingFile(t, path, "b.txt", "worktree version\n")
	a.compare.A = "HEAD"
	a.compare.B = "Worktree"
	a.applyCompare(true)
	if !a.review.Request.Reverse || a.compare.A != "Worktree" {
		t.Fatal("reverse not applied")
	}
	f := a.review.Items[0].Parts[0].Comparison
	if f.Before != "worktree version\n" || f.After != "only b\n" {
		t.Fatalf("wrong reversed contents: %+v", f)
	}
	if !strings.Contains(a.review.Summary, "Worktree → HEAD") {
		t.Fatal(a.review.Summary)
	}
	text := strings.Repeat("large text line\n", 80000)
	writeCommit(t, path, "large.txt", text)
	writeWorkingFile(t, path, "large.txt", text+"changed\n")
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: a.repos, Net: true, LargeFiles: true})
	found := false
	for _, item := range a.review.Items {
		if item.File.Path == "large.txt" {
			found = true
			if item.Parts[0].Comparison.Notice != "" {
				t.Fatal(item.Parts[0].Comparison.Notice)
			}
		}
	}
	if !found {
		t.Fatal("large file missing")
	}
}

func TestCompareSupersededReadsAndInlineHighlight(t *testing.T) {
	a := divergentCompareClient(t)
	var jobs []func() func()
	a.readRunner = func(job func() func()) { jobs = append(jobs, job) }
	request := ReviewRequest{Kind: "direct", Base: "refs/heads/side-a", Target: "refs/heads/side-b", Repositories: a.repos, Net: true}
	a.beginReview(request)
	request.Base, request.Target = request.Target, request.Base
	a.beginReview(request)
	if len(jobs) != 2 {
		t.Fatal("comparison should share the Review read lane")
	}
	jobs[0]()()
	if !a.review.Loading || len(a.compare.Result.Endpoints) != 0 {
		t.Fatal("stale result published")
	}
	jobs[1]()()
	if a.review.Loading || a.compare.Result.Endpoints[0].A != mustGit(t, a.repos[0].Path, "rev-parse", "refs/heads/side-b") {
		t.Fatal("latest comparison missing")
	}
	a.compare.Mode = "both"
	a.selectCompareCommit(0, 0)
	last := jobs[len(jobs)-1]
	a.clearCompareCommitSelection()
	last()()
	if len(a.compare.Selected) != 0 || a.selectedFile != "" || a.review.Loading {
		t.Fatal("cleared commit selection was resurrected")
	}
	old, new := lineChangeRange("名字=one", "名字=two")
	if "名字=one"[old.Start:old.End] != "one" || "名字=two"[new.Start:new.End] != "two" {
		t.Fatal(old, new)
	}
	f := FileComparison{Filename: "app.go", Before: "value := oldName\n", After: "value := newName\n"}
	f.prepare()
	if len(f.BeforeDocument.Changes) != 1 || len(f.AfterDocument.Changes) != 1 {
		t.Fatal("inline highlights not prepared")
	}
}

func TestCompareScopeKeepsExistingReadMarks(t *testing.T) {
	request := ReviewRequest{Kind: "direct", Base: "A", Target: "B", Net: true, Repositories: []Repository{{Path: "/repo"}}}
	previous := `{"Kind":"direct","Base":"A","Target":"B","Net":true,"Repositories":["/repo"],"Commits":null}`
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(previous)))
	if got := reviewScope(request); got != expected {
		t.Fatal("existing progress scope changed", got, expected)
	}
	request.IgnoreWhitespace = true
	if reviewScope(request) == expected {
		t.Fatal("display policy must get a separate read scope")
	}
}
