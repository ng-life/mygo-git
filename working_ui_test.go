package main

import (
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func workingClient(t *testing.T) *client {
	t.Helper()
	first, second := testRepo(t), testRepo(t)
	writeCommit(t, first, "order.txt", "old order\n")
	writeCommit(t, second, "stock.txt", "old stock\n")
	writeWorkingFile(t, first, "order.txt", "new order\n")
	writeWorkingFile(t, second, "stock.txt", "new stock\n")
	a := testClient([]Repository{inspect(Entry{Path: first, Name: "supplychain"}), inspect(Entry{Path: second, Name: "ordercenter"})})
	a.entries = []Entry{{Path: first, Name: "supplychain"}, {Path: second, Name: "ordercenter"}}
	a.readRunner = func(work func() func()) { work()() }
	a.writeRunner = func(work func() func()) { work()() }
	return a
}
func TestLocalWorkingTreeSingleAndSharedCommitUI(t *testing.T) {
	a := workingClient(t)
	tester := ui.NewTester(a.view, 1560, 940)
	if tester.HasText("Changelists") {
		t.Fatal("working tree must only appear in Local")
	}
	if err := tester.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if len(a.workingTrees) != 2 || !tester.HasText("Changelists") {
		t.Fatal("Local must load repository groups")
	}
	if err := tester.Click("order.txt"); err != nil {
		t.Fatal(err)
	}
	if a.workingComparison.Before != "old order\n" || a.workingComparison.After != "new order\n" {
		t.Fatal("working file comparison not loaded")
	}
	for _, label := range []string{"选择仓库 supplychain", "选择仓库 ordercenter"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.workingRequests("")) != 2 {
		t.Fatal("repository checkboxes must select files in both repositories")
	}
	message := "SCM-N12345 共用提交说明\n\n更新订单与库存处理"
	if err := tester.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tester.Type(message)
	if a.commitMessage != message {
		t.Fatalf("message binding lost edits: %q", a.commitMessage)
	}
	if target := os.Getenv("MYGO_WORKING_SCREENSHOT"); target != "" {
		f, err := os.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	first, second := a.repos[0].Path, a.repos[1].Path
	secondBefore := mustGit(t, second, "rev-parse", "HEAD")
	if err := tester.Click("Commit supplychain"); err != nil {
		t.Fatal(err)
	}
	if len(a.workingResults) != 1 || a.workingResults[0].Err != nil {
		t.Fatalf("single result: %+v", a.workingResults)
	}
	if got := mustGit(t, first, "log", "-1", "--format=%B"); got != message {
		t.Fatal(got)
	}
	if got := mustGit(t, second, "rev-parse", "HEAD"); got != secondBefore {
		t.Fatal("single-repository Commit changed another repository")
	}
	if a.commitMessage != message || len(a.workingRequests("")) != 1 {
		t.Fatal("shared message and remaining repository selection must be retained")
	}
	if err := tester.Click("Commit selected repositories"); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, second, "log", "-1", "--format=%B"); got != message {
		t.Fatal("shared message changed")
	}
	if a.commitMessage != "" || len(a.workingRequests("")) != 0 {
		t.Fatal("completed selections should clear")
	}
	// Now commit two repositories in a single click with one shared message.
	writeWorkingFile(t, first, "order.txt", "batch order\n")
	writeWorkingFile(t, second, "stock.txt", "batch stock\n")
	if err := tester.Click("刷新变更"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"选择仓库 supplychain", "选择仓库 ordercenter"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := tester.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tester.Type("SCM-N12346 一起提交")
	if err := tester.Click("Commit selected repositories"); err != nil {
		t.Fatal(err)
	}
	if len(a.workingResults) != 2 {
		t.Fatal("batch must execute against both repositories")
	}
	for _, result := range a.workingResults {
		if result.Err != nil {
			t.Fatalf("%s: %v", result.Repository.Name, result.Err)
		}
	}
	if mustGit(t, first, "log", "-1", "--format=%B") != mustGit(t, second, "log", "-1", "--format=%B") {
		t.Fatal("batch messages differ")
	}
	if err := tester.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("Changelists") {
		t.Fatal("working tree must disappear outside Local")
	}
}
func TestWorkingCommitFailureRetainsMessageAndOnlyFailedSelection(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	for _, tree := range a.workingTrees {
		a.toggleWorkingRepository(tree, true)
	}
	first := a.repos[0].Path
	hooks := first + "/reject-hooks"
	if err := os.Mkdir(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks+"/commit-msg", []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, first, "config", "core.hooksPath", hooks)
	a.commitMessage = "SCM-N12347 partial batch"
	a.commitWorking("")
	if len(a.workingResults) != 2 || a.workingResults[0].Err == nil || a.workingResults[1].Err != nil {
		t.Fatal(a.workingResults)
	}
	if strings.TrimSpace(a.commitMessage) == "" {
		t.Fatal("failure must retain shared message")
	}
	requests := a.workingRequests("")
	if len(requests) != 1 || requests[0].Tree.Repository.Path != first {
		t.Fatal("only failed repository selection should remain")
	}
}
func TestStaleWorkingTreeResponseIsIgnored(t *testing.T) {
	path := testRepo(t)
	writeWorkingFile(t, path, "first.txt", "first\n")
	a := testClient([]Repository{inspect(Entry{Path: path})})
	a.tab = 1
	var queue []func() func()
	a.readRunner = func(work func() func()) { queue = append(queue, work) }
	a.reloadWorkingTrees()
	old := queue[0]()
	writeWorkingFile(t, path, "second.txt", "second\n")
	a.reloadWorkingTrees()
	queue[1]()()
	old()
	if len(a.workingTrees) != 1 || len(a.workingTrees[0].Files) != 2 {
		t.Fatal("stale working tree response replaced latest data")
	}
	a.selectWorkingFile(a.workingTrees[0], a.workingTrees[0].Files[0])
	fileResponse := queue[2]()
	a.switchTab(2)
	fileResponse()
	if a.workingFileKey != "" || a.workingComparison.After != "" {
		t.Fatal("stale working file response appeared after switching tabs")
	}
}

func TestWorkingSelectionClearsWhenBranchChanges(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	tree := a.workingTrees[0]
	a.toggleWorkingRepository(tree, true)
	mustGit(t, tree.Repository.Path, "switch", "-c", "another-branch")
	a.reloadWorkingTrees()
	if len(a.workingRequests("")) != 0 {
		t.Fatal("refresh must not carry file selection across branch changes")
	}
}

func TestChangesSidebarHidesCleanRepositoriesAndBranches(t *testing.T) {
	dirty := Repository{Path: "/dirty", Name: "dirty-repository", Branch: "main", Branches: []string{"main"}}
	clean := Repository{Path: "/clean", Name: "clean-repository", Branch: "main", Branches: []string{"main"}}
	a := testClient([]Repository{dirty, clean})
	a.tab = 1
	a.workingTrees = []WorkingTree{{Repository: dirty, Files: []WorkingFile{{Status: " M", Path: "note.txt"}}}, {Repository: clean}}
	tester := ui.NewTester(a.view, 1560, 940)
	if _, ok := tester.Find("选择仓库 clean-repository"); ok {
		t.Fatal("clean repository must not appear in working changes")
	}
	if _, ok := tester.Find("选择仓库 dirty-repository"); !ok {
		t.Fatal("changed repository is missing")
	}
	upper, ok := tester.Find("Changelists")
	if !ok {
		t.Fatal("working tree header missing")
	}
	if tester.HasText("本地分支") {
		t.Fatal("Changes must not contain branch navigation")
	}
	if upper.X >= 400 {
		t.Fatalf("Changes header must stay in sidebar: %+v", upper)
	}
	a.workingTrees[0].Files = nil
	tester.Frame()
	if _, ok := tester.Find("选择仓库 dirty-repository"); ok {
		t.Fatal("repository must disappear after its changes are cleared")
	}
	if !tester.HasText("暂无工作区变更") {
		t.Fatal("clean workspace must show empty state")
	}
	tester.SetSize(1100, 760)
	tester.Frame()
}
