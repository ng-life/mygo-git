package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLogGraphUsesParentsAndRepositoryIdentity(t *testing.T) {
	repo := Repository{Path: "/one"}
	commits := []Commit{
		{Repository: repo, Hash: "merge", Parents: []string{"left", "right"}},
		{Repository: repo, Hash: "left", Parents: []string{"base"}},
		{Repository: Repository{Path: "/two"}, Hash: "base"},
		{Repository: repo, Hash: "right", Parents: []string{"base"}},
		{Repository: repo, Hash: "base", Parents: []string{"outside"}},
	}
	rows, width := buildLogGraph(commits)
	if len(rows) != 5 || width < 40 || len(rows[0].Lines) != 2 || rows[0].Boundary {
		t.Fatalf("%+v width %v", rows, width)
	}
	if rows[2].Lane == rows[4].Lane || !rows[4].Boundary {
		t.Fatal("cross-repository hashes linked or missing boundary")
	}
	if len(rows[3].Lines) < 2 {
		t.Fatal("merge lane not joined")
	}
}
func TestIDEAStyleLogFilterSelectionAndFileTree(t *testing.T) {
	path := testRepo(t)
	for _, dir := range []string{"src/main/java/app", "docs"} {
		if err := os.MkdirAll(filepath.Join(path, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeCommit(t, path, "src/main/java/app/OrderService.java", "class OrderService { int total = 1; }\n")
	mustGit(t, path, "checkout", "-b", "feature/review")
	writeCommit(t, path, "src/main/java/app/OrderService.java", "class OrderService { int total = 2; }\n")
	mustGit(t, path, "checkout", "main")
	writeCommit(t, path, "docs/review.md", "review notes\n")
	mustGit(t, path, "merge", "--no-ff", "feature/review", "-m", "SCM-N31137 合并订单审查优化")
	a := testClient([]Repository{inspect(Entry{Path: path, Name: "supplychain-stockorder"})})
	a.readRunner = func(job func() func()) { job()() }
	a.switchTab(2)
	tester := ui.NewTester(a.view, 1560, 940)
	if len(a.commits) == 0 || len(a.commits[0].Parents) != 2 {
		t.Fatalf("parents not loaded: %+v", a.commits)
	}
	if err := tester.Click(a.commits[0].Subject); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("变更文件 · 1 个提交") {
		t.Fatal("file tree missing")
	}
	if err := tester.Click("src/main/java/app/OrderService.java"); err != nil {
		t.Fatal(err)
	}
	if a.selectedFile == "" || len(a.comparisons) != 1 {
		t.Fatal("file tree selection did not load diff")
	}
	if dest := os.Getenv("MYGO_IDEA_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := tester.Click("搜索提交"); err != nil {
		t.Fatal(err)
	}
	tester.Type("SCM-N31137")
	visible := logVisibleIndices(a.commits, a.logQuery)
	if len(visible) != 1 || !strings.Contains(a.commits[visible[0]].Subject, "SCM-N31137") {
		t.Fatal("log filtering failed")
	}
	// Range selection in a filtered view must skip hidden commits.
	a.commits = []Commit{{Hash: "a", Subject: "keep"}, {Hash: "b", Subject: "hide"}, {Hash: "c", Subject: "keep"}}
	a.readRunner = func(job func() func()) {} // Selection behavior needs no Git reads.
	a.commitAnchor = -1
	a.clickLogCommit(0, []int{0, 2}, 0)
	a.clickLogCommit(1, []int{0, 2}, ui.Shift)
	if len(a.activeCommits) != 2 || a.commitSelection.Has(a.commits[1].Key()) {
		t.Fatal("hidden commit included in selection")
	}
}
