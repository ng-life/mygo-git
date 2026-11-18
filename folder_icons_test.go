package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFolderIconsPreserveCompactTreeAndDisclosure(t *testing.T) {
	repo := testRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src/main/java"), 0700); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, repo, "src/main/java/App.java", "class App {}\n")
	r := inspect(Entry{Path: repo, Name: "app"})
	a := testClient([]Repository{r})
	a.readRunner = func(work func() func()) { work()() }
	a.repositoryFiles = map[string]repositoryFileState{repo: readRepositoryFiles(context.Background(), r)}
	*a.browserOpen("repository:" + repo) = true
	*a.browserOpen("repository-folder:" + repo + "\x00src/main/java") = true
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText("src/main/java") || !tester.HasText("App.java") {
		t.Fatal("compact directory tree missing", tester.Texts(), a.repositoryFiles[repo].Error)
	}
	if _, ok := tester.Find("Folder icon"); !ok {
		t.Fatal("repository folder icon missing")
	}
	doubleTreeContent(t, tester, "src/main/java")
	if tester.HasText("App.java") {
		t.Fatal("double-click did not collapse directory with an icon")
	}
	doubleTreeContent(t, tester, "src/main/java")
	if !tester.HasText("App.java") {
		t.Fatal("double-click did not expand directory with an icon")
	}
	a.switchTab(1)
	tester.Frame()
	if _, ok := tester.Find("Folder icon"); !ok {
		t.Fatal("Changelists folder icon missing")
	}
	a.changeViewMode = "worktree"
	a.reloadChangeView()
	tester.Frame()
	if _, ok := tester.Find("Folder icon"); !ok {
		t.Fatal("Worktree folder icon missing")
	}
}
func TestBranchPrefixFoldersHaveIconsAndLeafRefsStayLeaves(t *testing.T) {
	a := testClient([]Repository{{Path: "/example", Name: "app", Branch: "main", Branches: []string{"main", "feature/review"}, RemoteBranches: []string{"origin/main"}}})
	a.tab = 2
	tester := ui.NewTester(a.view, 1560, 940)
	if _, ok := tester.Find("Folder icon"); !ok {
		t.Fatal("branch prefix icon missing")
	}
	if _, ok := tester.Find("Ref icon"); !ok {
		t.Fatal("leaf ref icon missing")
	}
	if err := tester.Click("review  (1)"); err != nil {
		t.Fatal(err)
	}
	if len(a.refSelection) != 1 || a.refSelection[0].Name != "feature/review" {
		t.Fatal("branch icon changed ref selection")
	}
}
