package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRepositoryPushAndWorkingState(t *testing.T) {
	path := testRepo(t)
	unborn := t.TempDir()
	mustGit(t, path, "init", unborn)
	empty := inspect(Entry{Path: unborn})
	if empty.Ahead != 0 || empty.PushError != "" {
		t.Fatalf("unborn: %+v", empty)
	}
	writeCommit(t, path, "file.txt", "base\n")
	r := inspect(Entry{Path: path})
	if r.Ahead != 2 || r.PushBase != "" {
		t.Fatalf("no remote: %+v", r)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, path, "init", "--bare", remote)
	mustGit(t, path, "remote", "add", "origin", remote)
	branch := mustGit(t, path, "branch", "--show-current")
	mustGit(t, path, "push", "-u", "origin", branch)
	r = inspect(Entry{Path: path})
	if r.Ahead != 0 || !strings.Contains(r.PushBase, "origin/") {
		t.Fatalf("pushed: %+v", r)
	}
	writeCommit(t, path, "file.txt", "next\n")
	r = inspect(Entry{Path: path})
	if r.Ahead != 1 || r.Status != "" || repositoryStateColor(r, ui.RGB(0, 0, 0)) != statusBlue {
		t.Fatalf("pending: %+v", r)
	}
	writeWorkingFile(t, path, "file.txt", "dirty\n")
	r = inspect(Entry{Path: path})
	if r.Ahead != 1 || r.Status == "" || repositoryStateColor(r, ui.RGB(0, 0, 0)) != statusOrange {
		t.Fatalf("dirty: %+v", r)
	}
	if tip := repositoryStateTooltip(r); !strings.Contains(tip, "Unpushed: 1") || !strings.Contains(tip, "Uncommitted changes") {
		t.Fatal(tip)
	}
	// Without an upstream, already published commits remain published.
	mustGit(t, path, "push", "origin", branch)
	mustGit(t, path, "branch", "--unset-upstream")
	r = inspect(Entry{Path: path})
	if r.Ahead != 0 {
		t.Fatalf("published without upstream: %+v", r)
	}
	mustGit(t, path, "checkout", "--detach", "HEAD")
	r = inspect(Entry{Path: path})
	if !strings.HasPrefix(r.Branch, "detached ") || r.Ahead != 0 {
		t.Fatalf("detached: %+v", r)
	}
}
func TestWorkingStatusColors(t *testing.T) {
	gray := ui.RGB(120, 120, 120)
	for _, test := range []struct {
		status string
		color  ui.Color
	}{{"??", statusGreen}, {"A ", statusGreen}, {"AM", statusGreen}, {" M", statusBlue}, {"M ", statusBlue}, {"R ", statusBlue}, {" D", gray}, {"D ", gray}, {"AD", gray}, {"D?", statusBlue}, {"UU", statusOrange}, {"AA", statusOrange}} {
		if got := workingStatusColor(WorkingFile{Status: test.status}, gray); got != test.color {
			t.Errorf("%q: %v", test.status, got)
		}
	}
}
func TestRepositoryBranchAndColoredWorkingTreeUI(t *testing.T) {
	path := testRepo(t)
	if err := os.MkdirAll(filepath.Join(path, "src", "feature"), 0700); err != nil {
		t.Fatal(err)
	}
	writeCommit(t, path, "src/feature/modified.go", "package app\nconst Value=1\n")
	writeCommit(t, path, "src/feature/deleted.go", "package app\n")
	writeWorkingFile(t, path, "src/feature/modified.go", "package app\nconst Value=2\n")
	writeWorkingFile(t, path, "src/feature/added.go", "package app\nconst New=true\n")
	if err := os.Remove(filepath.Join(path, "src/feature/deleted.go")); err != nil {
		t.Fatal(err)
	}
	repo := inspect(Entry{Path: path, Name: "sample/app"})
	a := testClient([]Repository{repo})
	a.readRunner = func(w func() func()) { w()() }
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText(repo.Branch) || !tester.HasText("↑ 3") {
		t.Fatal(tester.Texts())
	}
	save := func(name string) {
		if dir := os.Getenv("MYGO_STATUS_SCREENSHOTS"); dir != "" {
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err = png.Encode(f, tester.Image()); err != nil {
				t.Fatal(err)
			}
		}
	}
	row, _ := tester.Find(repo.Name)
	branchLabel, _ := tester.Find(repo.Branch)
	if row.H > 28 || branchLabel.Y < row.Y || branchLabel.Y+branchLabel.H > row.Y+row.H {
		t.Fatalf("branch wrapped below repository: row=%+v branch=%+v", row, branchLabel)
	}
	save("mygo-repository-status.png")
	if err := tester.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("src/feature") {
		t.Fatal(tester.Texts())
	}
	for _, text := range []string{"3 files", "1 added", "1 modified", "1 deleted", "Commit Message"} {
		if !tester.HasText(text) {
			t.Fatalf("missing summary: %s", text)
		}
	}
	if err := tester.Click("Collapse"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("added.go") {
		t.Fatal("collapse all left file nodes visible")
	}
	if err := tester.Click("Expand"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("added.go") {
		t.Fatal("expand all did not restore file nodes")
	}

	for _, name := range []string{"added.go", "modified.go", "deleted.go"} {
		if !tester.HasText(name) {
			t.Fatal(name)
		}
	}
	folder, _ := tester.Find("Select directory sample/app/src/feature")
	file, _ := tester.Find("Select file sample/app/src/feature/added.go")
	if delta := file.X - folder.X; delta < 20 || delta > 22 {
		t.Fatalf("unexpected tree indent: folder=%+v file=%+v", folder, file)
	}
	// Folder selection remains independent from expansion and selects descendants.
	if err := tester.Click("Select directory sample/app/src/feature"); err != nil {
		t.Fatal(err)
	}
	if len(a.workingRequests("")) != 1 || len(a.workingRequests("")[0].Files) != 3 {
		t.Fatal("folder selection lost descendants")
	}
	save("mygo-local-status.png")
	doubleTreeContent(t, tester, "Directory src/feature")
	if tester.HasText("added.go") {
		t.Fatal("folder name did not collapse subtree")
	}
	doubleTreeContent(t, tester, "Directory src/feature")
	tester.Frame()
}

func TestRepositoryInlineBranchStaysInSingleRow(t *testing.T) {
	repo := Repository{Path: "/example/project", Name: "supplychain-microservice-stockorder", Branch: "feature/SCM-N31137/南京自动分波优化", Ahead: 1, Status: " M file.go"}
	a := testClient([]Repository{repo})
	tester := ui.NewTester(a.view, 1560, 940)
	check := func() {
		row, found := tester.Find(repo.Name)
		if !found {
			t.Fatal("missing repository")
		}
		branch, found := tester.Find(repo.Branch)
		if !found {
			t.Fatal("missing branch")
		}
		if row.H > 28 || branch.Y < row.Y || branch.Y+branch.H > row.Y+row.H || branch.X+branch.W > row.X+row.W {
			t.Fatalf("branch overflow/wrap: row=%+v branch=%+v", row, branch)
		}
	}
	check()
	a.sidebarWidths[0] = 650
	tester.Frame()
	check()
}
