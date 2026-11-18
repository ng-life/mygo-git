package main

import (
	"image/png"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func testClient(repos []Repository) *client {
	return &client{repos: repos, selected: map[string]bool{}, expanded: map[string]*bool{}, logIndex: -1, fileIndex: -1, workspaceSplit: 55, commitAnchor: -1, output: "Ready"}
}
func TestNavigationTabsSelectionAndMenus(t *testing.T) {
	a := testClient([]Repository{
		{Name: "supplychain", Path: "/workspace/supplychain", Branch: "main", Branches: []string{"main", "feature/order/export"}, RemoteBranches: []string{"origin/feature/order/export"}},
		{Name: "ordercenter", Path: "/workspace/ordercenter", Branch: "release", Branches: []string{"release", "feature/order/export"}},
	})
	tester := ui.NewTester(a.view, 1560, 940)
	for _, label := range []string{"Repositories", "Changes", "Logs", "Log"} {
		if !tester.HasText(label) {
			t.Fatalf("missing UI: %s", label)
		}
	}
	if tester.HasText("Changed Files") || tester.HasText("Before") {
		t.Fatal("unselected views must remain hidden")
	}
	if tester.HasText("Group Same-Name Branches") || tester.HasText("Branch Hierarchy") {
		t.Fatal("old mode switch remains")
	}
	if err := tester.Click("supplychain"); err != nil {
		t.Fatal(err)
	}
	if len(a.visible()) != 1 || len(a.targets()) != 1 {
		t.Fatal("repository selection must isolate one repository")
	}
	if err := tester.RightClick("supplychain"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "supplychain · This repository") {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	if err := tester.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if a.tab != 1 || a.repositoryPath != "" {
		t.Fatal("tab must change navigation scope")
	}
	if err := tester.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("export  (2)"); err != nil {
		t.Fatal(err)
	}
	if a.group != "/feature/order/export" || a.branch != "feature/order/export" {
		t.Fatal("branch selection failed")
	}
	if a.expanded["logs:local/feature/order/export"] != nil {
		t.Fatal("leaf must not have disclosure state")
	}
	if err := tester.RightClick("export  (2)"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "feature/order/export · All 2 repositories") {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	a.busy = true
	tester.Frame()
	tester.RightClick("export  (2)")
	if err := tester.ChooseMenuItem("Checkout Branch"); err == nil {
		t.Fatal("busy operation must disable actions")
	}
	tester.CloseMenu()
	a.busy = false
	tester.Frame()
	if err := tester.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if a.tab != 2 || !tester.HasText("export  (2)") || !tester.HasText("export  (1)") {
		t.Fatal("Logs must contain local and remote branches")
	}
	if err := tester.Click("Remote export  (1)"); err != nil {
		t.Fatal(err)
	}
	if a.group != "/origin/feature/order/export" {
		t.Fatal("remote branch selection failed")
	}
	if err := tester.RightClick("Remote export  (1)"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "origin/feature/order/export · All 1 repositories") {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	a.repos = nil
	tester.Frame()
	a.repos = []Repository{{Name: "new", Path: "/new", Branches: []string{"main"}}}
	tester.Frame()
	tester.SetSize(1100, 760)
	tester.Frame()
}
func TestCommitFileComparisonUI(t *testing.T) {
	first, second := featureRepo(t), featureRepo(t)
	for _, r := range []Repository{first, second} {
		writeCommit(t, r.Path, "visible.txt", "before\n")
		writeCommit(t, r.Path, "visible.txt", "after\n")
		mustGit(t, r.Path, "commit", "--amend", "-m", "更新订单导出逻辑")
	}
	first = inspect(Entry{Path: first.Path, Name: "supplychain"})
	second = inspect(Entry{Path: second.Path, Name: "ordercenter"})
	a := testClient([]Repository{first, second})
	a.readRunner = func(work func() func()) { work()() }
	a.reloadHistory()
	tester := ui.NewTester(a.view, 1560, 940)
	if len(a.commits) != 6 {
		t.Fatalf("expected combined histories: %d", len(a.commits))
	}
	if err := tester.Click("更新订单导出逻辑"); err != nil {
		t.Fatal(err)
	}
	if a.selectedCommit == "" || len(a.files) != 1 {
		t.Fatal("commit click did not load related files")
	}
	if err := tester.Click("visible.txt"); err != nil {
		t.Fatal(err)
	}
	if a.selectedFile != a.files[0].Key() {
		t.Fatal("file click did not select file")
	}
	if len(a.comparisons) != 1 || a.comparisons[0].Comparison.Before != "before\n" || a.comparisons[0].Comparison.After != "after\n" {
		t.Fatal("comparison not loaded")
	}
	if os.Getenv("MYGO_SCREENSHOT") != "" {
		f, err := os.Create(os.Getenv("MYGO_SCREENSHOT"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
	tester.SetSize(1100, 760)
	tester.Frame()
}

func TestControlShiftMultiSelectionUI(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "one\n")
	mustGit(t, path, "commit", "--amend", "-m", "First selected change")
	writeCommit(t, path, "gap.txt", "gap\n")
	mustGit(t, path, "commit", "--amend", "-m", "Middle change")
	writeCommit(t, path, "note.txt", "two\n")
	mustGit(t, path, "commit", "--amend", "-m", "Last selected change")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.readRunner = func(work func() func()) { work()() }
	a.reloadHistory()
	tester := ui.NewTester(a.view, 1560, 940)
	if tester.HasText("Changed Files") {
		t.Fatal("file subview must be hidden with no selection")
	}
	if err := tester.Click("Last selected change"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Changed Files") || len(a.activeCommits) != 1 {
		t.Fatal("selecting a commit must reveal Log file subview")
	}
	if a.selectedFile != "" || tester.HasText("Before") {
		t.Fatal("file area must remain blank until a file is selected")
	}
	if err := tester.ClickWith(ui.Ctrl, "First selected change"); err != nil {
		t.Fatal(err)
	}
	if len(a.activeCommits) != 2 || len(a.files) != 1 || len(a.files[0].Changes) != 2 {
		t.Fatalf("Control selection: commits=%d files=%+v", len(a.activeCommits), a.files)
	}
	if err := tester.Click("note.txt"); err != nil {
		t.Fatal(err)
	}
	if len(a.comparisons) != 2 {
		t.Fatal("comparison area must combine both selected commit changes")
	}
	if target := os.Getenv("MYGO_MULTI_SCREENSHOT"); target != "" {
		f, err := os.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := tester.ClickWith(ui.Shift, shortHash(mustGit(t, path, "rev-parse", "HEAD"))); err != nil {
		t.Fatal(err)
	}
	if len(a.activeCommits) != 3 || len(a.files) != 2 || a.selectedFile != "" {
		t.Fatalf("Shift must select range and clear stale comparison: commits=%d files=%d selectedFile=%q anchor=%d index=%d", len(a.activeCommits), len(a.files), a.selectedFile, a.commitAnchor, a.logIndex)
	}
	if err := tester.Click("Deselect All"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("Changed Files") || len(a.activeCommits) != 0 || len(a.editorTabs) == 0 {
		t.Fatal("clearing commit selection must hide the Log subview and preserve editor tabs")
	}
}
