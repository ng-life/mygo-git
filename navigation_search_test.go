package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestRepositoryBrowserCompactFilesAndCurrentContents(t *testing.T) {
	repo := testRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src", "main", "go"), 0700); err != nil {
		t.Fatal(err)
	}
	writeCommit(t, repo, "src/main/go/app.go", "package app\nconst Value = 1\n")
	writeWorkingFile(t, repo, "src/main/go/app.go", "package app\nconst Value = 2\n")
	writeWorkingFile(t, repo, "untracked.txt", "new file\n")
	writeWorkingFile(t, repo, ".gitignore", "ignored.txt\n")
	writeWorkingFile(t, repo, "ignored.txt", "hidden")
	state := readRepositoryFiles(context.Background(), inspect(Entry{Path: repo, Name: "app"}))
	if state.Error != "" || state.Root == nil {
		t.Fatal(state.Error)
	}
	names := []string{}
	for _, file := range state.Tree.Files {
		names = append(names, file.Path)
	}
	if !slices.Contains(names, "untracked.txt") || !slices.Contains(names, "src/main/go/app.go") || slices.Contains(names, "ignored.txt") {
		t.Fatal(names)
	}
	a := testClient([]Repository{inspect(Entry{Path: repo, Name: "app"})})
	a.readRunner = func(work func() func()) { work()() }
	tester := ui.NewTester(a.view, 1560, 940)
	if tester.HasText("全部仓库") {
		t.Fatal("all repositories button remains")
	}
	if err := tester.Click("展开仓库 app"); err != nil {
		t.Fatal(err)
	}
	if a.repositoryPath != "" || !a.repositoryFiles[repo].Loaded {
		t.Fatal("disclosure must load files without changing Log scope")
	}
	if err := tester.Click("展开仓库 app"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("app"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tester.Texts(), "src/main/go") {
		t.Fatal("single repository click expanded tree")
	}
	doubleTreeContent(t, tester, "app")
	if !slices.Contains(tester.Texts(), "src/main/go") {
		t.Fatal("compact folder missing", tester.Texts())
	}
	doubleTreeContent(t, tester, "src/main/go")
	if err := tester.Click("工作区文件 app/src/main/go/app.go"); err != nil {
		t.Fatal(err)
	}
	if a.workingComparison.After != "package app\nconst Value = 2\n" || !tester.HasText("当前工作区") || !tester.HasText("const Value = 2") {
		t.Fatal("browser did not show current worktree contents", tester.Texts())
	}
	tester.Advance(time.Second)
	toolbar, _ := tester.Find("刷新全部")
	header, _ := tester.Find("仓库列表")
	if toolbar.Y+toolbar.H > 34.5 || header.Y < 34 {
		t.Fatalf("toolbar overlap: toolbar=%+v header=%+v", toolbar, header)
	}
	if output := os.Getenv("MYGO_REPOSITORY_BROWSER_SCREENSHOT"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		// Verify the full final frame after exercising the existing interaction surface.
		fresh := ui.NewTester(a.view, 1560, 940)
		if err := png.Encode(file, fresh.Image()); err != nil {
			t.Fatal(err)
		}
	}
	// A clean file must still be visible as source, rather than an empty diff.
	writeWorkingFile(t, repo, "src/main/go/app.go", "package app\nconst Value = 1\n")
	a.refreshRepositoryFiles(a.repos)
	tester.Frame()
	if !tester.HasText("const Value = 1") {
		t.Fatal("clean worktree file is not displayed")
	}
	writeWorkingFile(t, repo, "added.go", "package app\n")
	a.refreshRepositoryFiles(a.repos)
	tester.Frame()
	if !tester.HasText("added.go") {
		t.Fatal("browser cache not refreshed")
	}
}
func TestUnifiedLogsSearchPreservesScopeAndAncestors(t *testing.T) {
	a := testClient([]Repository{
		{Path: "/workspace/orders", Name: "Orders", Branches: []string{"main", "feature/order/export", "fix/stock"}, RemoteBranches: []string{"origin/main", "origin/feature/order/export"}, Tags: []string{"release/export/v1"}},
		{Path: "/workspace/stock", Name: "Stock", Branches: []string{"main"}, RemoteBranches: []string{"origin/main"}},
	})
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("搜索仓库"); err != nil {
		t.Fatal(err)
	}
	tester.Type("ORDERS")
	if a.repositoryQuery != "ORDERS" || slices.Contains(tester.Texts(), "Stock") {
		t.Fatal("repository filter failed")
	}
	if err := tester.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("搜索分支或 Tag"); err != nil {
		t.Fatal(err)
	}
	tester.Type("EXPORT")
	if !tester.HasText("export  (1)") || !tester.HasText("feature  (1)") || !tester.HasText("v1  (1)") || tester.HasText("main  (2)") {
		t.Fatal(tester.Texts())
	}
	if err := tester.RightClick("export  (1)"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "feature/order/export · 全部 1 个仓库") {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	if err := tester.Click("Repositories"); err != nil {
		t.Fatal(err)
	}
	if a.repositoryQuery != "ORDERS" || slices.Contains(tester.Texts(), "Stock") {
		t.Fatal("repository search not retained")
	}
	if err := tester.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if a.logsQuery != "EXPORT" {
		t.Fatal("Logs query not retained")
	}
}
func TestRepositoryFileLoadsRejectSupersededResponse(t *testing.T) {
	repo := testRepo(t)
	writeWorkingFile(t, repo, "first.go", "package app\n")
	a := testClient([]Repository{inspect(Entry{Path: repo})})
	var queue []func() func()
	a.readRunner = func(work func() func()) { queue = append(queue, work) }
	a.loadRepositoryFiles(a.repos[0], false)
	old := queue[0]()
	writeWorkingFile(t, repo, "second.go", "package app\n")
	a.loadRepositoryFiles(a.repos[0], true)
	queue[1]()()
	old()
	state := a.repositoryFiles[repo]
	if len(state.Tree.Files) != 2 {
		t.Fatal("stale file list applied", state.Tree.Files)
	}
	if err := os.Remove(filepath.Join(repo, "first.go")); err != nil {
		t.Fatal(err)
	}
	state = readRepositoryFiles(context.Background(), a.repos[0])
	if len(state.Tree.Files) != 1 || state.Tree.Files[0].Path != "second.go" {
		t.Fatal(state.Tree.Files)
	}
}
func TestLocalWorkingTreeIndentation(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	tester := ui.NewTester(a.view, 1560, 940)
	heading, _ := tester.Find("Changelists")
	repository, _ := tester.Find("展开或折叠 supplychain")
	file, _ := tester.Find("选择文件 supplychain/order.txt")
	if repository.X <= heading.X || file.X <= repository.X {
		t.Fatalf("indentation missing: heading=%+v repository=%+v file=%+v", heading, repository, file)
	}
}
