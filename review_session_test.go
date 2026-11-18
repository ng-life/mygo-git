package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestReviewMergeBaseDirectAndWorkspace(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "shared.txt", "base\n")
	base := mustGit(t, path, "rev-parse", "HEAD")
	mustGit(t, path, "checkout", "-b", "feature/review")
	writeCommit(t, path, "feature.txt", "feature\n")
	target := mustGit(t, path, "rev-parse", "HEAD")
	mustGit(t, path, "checkout", "-b", "baseline", base)
	writeCommit(t, path, "baseline.txt", "baseline\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	request := ReviewRequest{Kind: "merge-base", Base: "baseline", Target: "feature/review", Repositories: []Repository{repo}, Net: true}
	items, summary, problem := readReview(context.Background(), request)
	if problem != "" || len(items) != 1 || items[0].File.Path != "feature.txt" || !strings.Contains(summary, base) || !strings.Contains(summary, target) {
		t.Fatalf("%+v %s %s", items, summary, problem)
	}
	request.Kind = "direct"
	items, _, problem = readReview(context.Background(), request)
	if problem != "" || len(items) != 2 {
		t.Fatalf("direct %d %s", len(items), problem)
	}
	writeWorkingFile(t, path, "shared.txt", "local\n")
	writeWorkingFile(t, path, "new.txt", "new\n")
	request.Kind = "workspace"
	request.Base = "HEAD"
	items, _, problem = readReview(context.Background(), request)
	if problem != "" || len(items) != 2 {
		t.Fatalf("workspace %d %s", len(items), problem)
	}
	for _, item := range items {
		if item.Fingerprint == "" || len(item.Parts) != 1 || item.Parts[0].Error != "" || item.Parts[0].Comparison.After == "" {
			t.Fatalf("%+v", item)
		}
	}
	// A different base must include committed differences as well as dirty files.
	request.Base = "feature/review"
	items, _, problem = readReview(context.Background(), request)
	if problem != "" || len(items) != 4 {
		t.Fatalf("branch to workspace %d %s", len(items), problem)
	}
}
func TestReviewNetAncestryAndRevertedChanges(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "zero\n")
	writeCommit(t, path, "file.txt", "one\n")
	first := mustGit(t, path, "rev-parse", "HEAD")
	writeCommit(t, path, "middle.txt", "included even if unselected\n")
	writeCommit(t, path, "file.txt", "zero\n")
	last := mustGit(t, path, "rev-parse", "HEAD")
	repo := inspect(Entry{Path: path, Name: "app"})
	commits := []Commit{{Repository: repo, Hash: last}, {Repository: repo, Hash: first}}
	r := ReviewRequest{Kind: "commits", Repositories: []Repository{repo}, Commits: commits, Net: true}
	items, _, problem := readReview(context.Background(), r)
	if problem != "" || len(items) != 1 || items[0].File.Path != "middle.txt" {
		t.Fatalf("net %v %s", items, problem)
	}
	r.Net = false
	items, _, problem = readReview(context.Background(), r)
	if problem != "" || len(items) != 1 || items[0].File.Path != "file.txt" || len(items[0].Parts) != 2 {
		t.Fatalf("per commit %v %s", items, problem)
	}
	mustGit(t, path, "checkout", "-b", "other", first)
	writeCommit(t, path, "other.txt", "other\n")
	r.Net = true
	r.Commits = append(r.Commits, Commit{Repository: repo, Hash: mustGit(t, path, "rev-parse", "HEAD")})
	items, _, problem = readReview(context.Background(), r)
	if len(items) != 0 || !strings.Contains(problem, "ancestor chain") {
		t.Fatalf("unrelated branches %v %s", items, problem)
	}
}
func TestReviewInitialCommitAndUnbornWorkspace(t *testing.T) {
	path := testRepo(t)
	repo := inspect(Entry{Path: path, Name: "new"})
	writeWorkingFile(t, path, "new.txt", "first\n")
	r := ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}}
	items, _, problem := readReview(context.Background(), r)
	if problem != "" || len(items) != 1 || items[0].Parts[0].Comparison.After != "first\n" {
		t.Fatalf("unborn %v %s", items, problem)
	}
	writeCommit(t, path, "new.txt", "first\n")
	r.Kind = "commits"
	r.Net = true
	r.Commits = []Commit{{Repository: repo, Hash: mustGit(t, path, "rev-parse", "HEAD")}}
	items, _, problem = readReview(context.Background(), r)
	if problem != "" || len(items) != 1 || items[0].Parts[0].Error != "" {
		t.Fatalf("initial net %v %s", items, problem)
	}
}
func TestReviewProgressPersistsAndInvalidatesBinaryAndText(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeWorkingFile(t, path, "file.txt", "one\n")
	writeWorkingFile(t, path, "new.bin", "\x00one")
	repo := inspect(Entry{Path: path, Name: "app"})
	r := ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}}
	a := testClient([]Repository{repo})
	a.reviewProgressPath = filepath.Join(t.TempDir(), "progress.json")
	a.readRunner = func(work func() func()) { work()() }
	a.beginReview(r)
	if len(a.review.Items) != 2 {
		t.Fatal(a.review.Error)
	}
	for _, item := range a.review.Items {
		a.toggleReviewMark(item)
	}
	marks, err := loadReviewMarks(a.reviewProgressPath)
	if err != nil || len(marks) != 2 {
		t.Fatalf("%v %v", marks, err)
	}
	b := testClient([]Repository{repo})
	b.reviewMarks = marks
	b.review.Scope = reviewScope(r)
	for _, item := range a.review.Items {
		if !b.isReviewed(item) {
			t.Fatal("mark not restored")
		}
	}
	a.review.UnreadOnly = true
	if len(a.reviewVisibleItems()) != 0 {
		t.Fatal("read files visible")
	}
	writeWorkingFile(t, path, "file.txt", "two\n")
	writeWorkingFile(t, path, "new.bin", "\x00two")
	a.beginReview(r)
	if len(a.reviewVisibleItems()) != 2 {
		t.Fatal("changed content retained read state")
	}
	other := r
	other.Base = "HEAD~1"
	if reviewScope(other) == reviewScope(r) {
		t.Fatal("scope collision")
	}
}
func TestReviewPartialFailureAndStaleRead(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeWorkingFile(t, path, "file.txt", "local\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	r := ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo, {Path: t.TempDir(), Name: "bad"}}}
	items, _, problem := readReview(context.Background(), r)
	if len(items) != 1 || !strings.Contains(problem, "bad") {
		t.Fatalf("%d %s", len(items), problem)
	}
	a := testClient([]Repository{repo})
	var queue []func() func()
	a.readRunner = func(w func() func()) { queue = append(queue, w) }
	r.Repositories = []Repository{repo}
	a.beginReview(r)
	old := queue[0]()
	a.closeReview()
	old()
	if a.review.Open || len(a.review.Items) != 0 {
		t.Fatal("closed review received stale state")
	}
}
func TestReviewUnifiedUIAndNavigation(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "one.txt", "base\n")
	writeWorkingFile(t, path, "one.txt", "local\n")
	writeWorkingFile(t, path, "two.txt", "new\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.reviewProgressPath = filepath.Join(t.TempDir(), "progress.json")
	a.readRunner = func(w func() func()) { w()() }
	tester := ui.NewTester(a.view, 1560, 940)
	chooseApplicationAction(t, a, "review")
	tester.Frame()
	if !tester.HasText("Start Review") {
		t.Fatal(tester.Texts())
	}
	if err := tester.Click("Start Review"); err != nil {
		t.Fatal(err)
	}
	if !a.review.Open || len(a.review.Items) != 2 {
		t.Fatal(a.review.Error)
	}
	if err := tester.Click("Review file one.txt"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("local") {
		t.Fatal(tester.Texts())
	}
	if err := tester.Click("Mark as Reviewed one.txt"); err != nil {
		t.Fatal(err)
	}
	if !a.isReviewed(a.review.Items[0]) {
		t.Fatal("not marked")
	}
	a.moveReviewFile(1)
	tester.Frame()
	if !strings.HasSuffix(a.selectedFile, "two.txt") {
		t.Fatal(a.selectedFile)
	}
	if dest := os.Getenv("MYGO_REVIEW_SESSION_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tester.Click("Close Review"); err != nil {
		t.Fatal(err)
	}
	if a.review.Open || a.selectedFile != "" {
		t.Fatal("review not closed")
	}
	tester.Frame()
}

func TestReviewReadMarkDoesNotReturnAfterContentReverts(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeWorkingFile(t, path, "file.txt", "one\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.reviewProgressPath = filepath.Join(t.TempDir(), "progress.json")
	a.readRunner = func(w func() func()) { w()() }
	r := ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}}
	a.beginReview(r)
	a.toggleReviewMark(a.review.Items[0])
	writeWorkingFile(t, path, "file.txt", "two\n")
	a.beginReview(r)
	writeWorkingFile(t, path, "file.txt", "one\n")
	a.beginReview(r)
	if a.isReviewed(a.review.Items[0]) {
		t.Fatal("invalidated read mark returned after content reverted")
	}
	a.toggleReviewMark(a.review.Items[0])
	writeWorkingFile(t, path, "file.txt", "base\n")
	a.beginReview(r)
	writeWorkingFile(t, path, "file.txt", "one\n")
	a.beginReview(r)
	if a.isReviewed(a.review.Items[0]) {
		t.Fatal("removed change retained its read mark")
	}

}
func TestReviewDialogCancelKeepsActiveScopeAndLatestSaveWins(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeWorkingFile(t, path, "file.txt", "local\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.reviewProgressPath = filepath.Join(t.TempDir(), "progress.json")
	a.readRunner = func(w func() func()) { w()() }
	r := ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}}
	a.beginReview(r)
	a.openReviewDialog(nil, "")
	a.review.Dialog = false
	if len(a.review.Request.Repositories) != 1 {
		t.Fatal("dialog changed active request")
	}
	var queue []func() func()
	a.readRunner = func(w func() func()) { queue = append(queue, w) }
	a.toggleReviewMark(a.review.Items[0])
	a.toggleReviewMark(a.review.Items[0])
	// Complete workers in reverse order; an old snapshot must not overwrite new marks.
	queue[1]()()
	queue[0]()()
	marks, err := loadReviewMarks(a.reviewProgressPath)
	if err != nil || len(marks) != 0 {
		t.Fatalf("stale save won: %v %v", marks, err)
	}
}
func TestReviewCompactTreeAndCommitModeSwitchUI(t *testing.T) {
	path := testRepo(t)
	if err := os.MkdirAll(filepath.Join(path, "src", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	writeCommit(t, path, "src/deep/file.txt", "base\n")
	writeCommit(t, path, "src/deep/file.txt", "one\n")
	first := mustGit(t, path, "rev-parse", "HEAD")
	writeCommit(t, path, "src/deep/file.txt", "two\n")
	last := mustGit(t, path, "rev-parse", "HEAD")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.readRunner = func(w func() func()) { w()() }
	a.beginReview(ReviewRequest{Kind: "commits", Net: true, Repositories: []Repository{repo}, Commits: []Commit{{Repository: repo, Hash: last}, {Repository: repo, Hash: first}}})
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText("src/deep") {
		t.Fatal(tester.Texts())
	}
	if err := tester.Click("Review file src/deep/file.txt"); err != nil {
		t.Fatal(err)
	}
	if len(a.comparisons) != 1 || a.comparisons[0].Comparison.Before != "base\n" || a.comparisons[0].Comparison.After != "two\n" {
		t.Fatal("wrong net contents")
	}
	if err := tester.Click("Per Commit"); err != nil {
		t.Fatal(err)
	}
	if len(a.comparisons) != 2 {
		t.Fatal("mode switch did not refresh selected file")
	}
	if err := tester.Click("Net Diff"); err != nil {
		t.Fatal(err)
	}
	if len(a.comparisons) != 1 {
		t.Fatal("net mode not restored")
	}
	tester.Frame()
}

func TestReviewBranchPerCommitAndResolvedBaseline(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	base := mustGit(t, path, "rev-parse", "HEAD")
	mustGit(t, path, "checkout", "-b", "feature/review")
	writeCommit(t, path, "file.txt", "one\n")
	writeCommit(t, path, "file.txt", "two\n")
	mustGit(t, path, "checkout", "-b", "baseline", base)
	writeCommit(t, path, "baseline.txt", "baseline\n")
	baseline := mustGit(t, path, "rev-parse", "HEAD")
	repo := inspect(Entry{Path: path, Name: "app"})
	r := ReviewRequest{Kind: "merge-base", Base: "baseline", Target: "feature/review", Repositories: []Repository{repo}}
	items, summary, problem := readReview(context.Background(), r)
	if problem != "" || len(items) != 1 || len(items[0].Parts) != 2 || !strings.Contains(summary, baseline) || !strings.Contains(summary, "Merge bases ["+base+"]") {
		t.Fatalf("%v %s %s", items, summary, problem)
	}
	r.Net = true
	net, _, problem := readReview(context.Background(), r)
	if problem != "" || len(net) != 1 || len(net[0].Parts) != 1 || net[0].Parts[0].Comparison.Before != "base\n" || net[0].Parts[0].Comparison.After != "two\n" {
		t.Fatalf("%v %s", net, problem)
	}
	if reviewScope(r) == reviewScope(ReviewRequest{Kind: r.Kind, Base: r.Base, Target: r.Target, Repositories: r.Repositories}) {
		t.Fatal("modes share progress scope")
	}
}

func TestReviewYieldsToWorkingFileAndRejectsCanceledWorker(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeWorkingFile(t, path, "file.txt", "local\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.tab = 1
	a.readRunner = func(w func() func()) { w()() }
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}})
	tree := readWorkingTree(repo)
	a.selectWorkingFile(tree, tree.Files[0])
	if a.review.Open || a.workingFileKey == "" {
		t.Fatal("working tree click did not leave Review")
	}
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}})
	var queue []func() func()
	a.readRunner = func(w func() func()) { queue = append(queue, w) }
	a.beginReview(a.review.Request)
	old := queue[0]()
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "missing-ref", Repositories: []Repository{repo}})
	queue[1]()()
	old()
	if a.review.Error == "" || len(a.review.Items) != 0 {
		t.Fatal("stale response replaced new range")
	}
}
