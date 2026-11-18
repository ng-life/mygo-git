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

func TestIndependentEditorTabsReuseCloseAndAsyncReads(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "one.go", "package main\nconst One=1\n")
	writeCommit(t, repo, "two.go", "package main\nconst Two=2\n")
	r := inspect(Entry{Path: repo, Name: "app"})
	tree := readRepositoryFiles(context.Background(), r).Tree
	a := testClient([]Repository{r})
	var queue []func() func()
	a.readRunner = func(work func() func()) { queue = append(queue, work) }
	a.selectWorkingFile(tree, WorkingFile{Path: "one.go"})
	first := a.editorActive
	a.selectWorkingFile(tree, WorkingFile{Path: "two.go"})
	second := a.editorActive
	if first == second || len(a.editorTabs) != 2 {
		t.Fatal("opening another file replaced the first tab")
	}
	queue[1]()()
	queue[0]()()
	if a.activeEditor().Comparison.After != "package main\nconst Two=2\n" || a.editorByID(first).Comparison.After != "package main\nconst One=1\n" {
		t.Fatal("out-of-order readers replaced another tab")
	}
	a.activateEditor(first)
	if a.activeEditor().Comparison.After != "package main\nconst One=1\n" {
		t.Fatal("switching tabs lost file contents")
	}
	a.selectWorkingFile(tree, WorkingFile{Path: "one.go"})
	if len(a.editorTabs) != 2 {
		t.Fatal("same file opened a duplicate tab")
	}
	a.closeEditor(first)
	queue[2]()()
	if a.editorByID(first) != nil || a.editorActive != second {
		t.Fatal("closed tab reopened after its reader completed")
	}
	a.closeEditor(second)
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText("Open a file from Repositories or Changes") || tester.HasText("Inline") {
		t.Fatal("empty editor did not return to welcome hints", tester.Texts())
	}
}
func TestIndexAndWorktreeDiffsRemainIndependent(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "file.txt", "HEAD\n")
	writeWorkingFile(t, repo, "file.txt", "INDEX\n")
	mustGit(t, repo, "add", "file.txt")
	writeWorkingFile(t, repo, "file.txt", "WORKTREE\n")
	r := inspect(Entry{Path: repo, Name: "app"})
	tree := readWorkingTree(r)
	file := tree.Files[0]
	staged, err := readIndexComparison(context.Background(), tree, file, "index")
	if err != nil || staged.Before != "HEAD\n" || staged.After != "INDEX\n" {
		t.Fatalf("staged diff: %+v %v", staged, err)
	}
	unstaged, err := readIndexComparison(context.Background(), tree, file, "worktree")
	if err != nil || unstaged.Before != "INDEX\n" || unstaged.After != "WORKTREE\n" {
		t.Fatalf("unstaged diff: %+v %v", unstaged, err)
	}
	a := testClient([]Repository{r})
	a.tab = 1
	a.readRunner = func(work func() func()) { work()() }
	a.openScopedChange(tree, file, "index")
	indexID := a.editorActive
	a.openScopedChange(tree, file, "worktree")
	workID := a.editorActive
	if indexID == workID || len(a.editorTabs) != 2 || a.editorByID(indexID).Comparison.After != "INDEX\n" {
		t.Fatal("diff scopes replaced each other")
	}
	a.activateEditor(indexID)
	if a.activeEditor().Mode != "index" || a.activeEditor().Comparison.After != "INDEX\n" {
		t.Fatal("index tab state was not restored")
	}
	if got := mustGit(t, repo, "show", ":file.txt"); got != "INDEX" {
		t.Fatal("viewing changes modified the index")
	}
}
func TestScopedChangesIncludeNetZeroIndexChanges(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "file.txt", "HEAD\n")
	writeWorkingFile(t, repo, "file.txt", "INDEX\n")
	mustGit(t, repo, "add", "file.txt")
	writeWorkingFile(t, repo, "file.txt", "HEAD\n")
	r := inspect(Entry{Path: repo, Name: "app"})
	if tree := readWorkingTree(r); len(tree.Files) != 0 {
		t.Fatal("fixture is not net-zero")
	}
	for _, scope := range []string{"index", "worktree"} {
		trees := readChangeView(context.Background(), []Repository{r}, scope)
		if len(trees) != 1 || len(trees[0].Files) != 1 {
			t.Fatal("index/worktree changes were hidden", scope, trees)
		}
	}
}
func TestCompareTabsRestoreRefsAndShowBothCommitSides(t *testing.T) {
	a := divergentCompareClient(t)
	a.beginReview(ReviewRequest{Kind: "direct", Base: "side-a", Target: "side-b", Repositories: a.repos, Net: true})
	first := a.editorActive
	a.compare.Mode = "sides"
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText("Only A ·") || !tester.HasText("Only B ·") {
		t.Fatal("two-sided commit differences missing", tester.Texts())
	}
	request := ReviewRequest{Kind: "direct", Base: "main", Target: "side-a", Repositories: a.repos, Net: true}
	a.beginReview(request)
	second := a.editorActive
	if first == second || len(a.editorTabs) < 2 {
		t.Fatal("ref comparison replaced an existing comparison tab")
	}
	a.activateEditor(first)
	if a.compare.A != "side-a" && a.compare.A != "refs/heads/side-a" {
		t.Fatal("comparison refs were not restored", a.compare.A)
	}
	if a.compare.Mode != "sides" || !a.review.Open {
		t.Fatal("comparison view mode was not restored")
	}
	if output := os.Getenv("MYGO_EDITOR_TABS_SCREENSHOT"); output != "" {
		tester.Frame()
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
func TestIndexDiffRenameDeletionBinaryAndUnbornHead(t *testing.T) {
	repo := testRepo(t)
	writeWorkingFile(t, repo, "new.txt", "first\n")
	mustGit(t, repo, "add", "new.txt")
	r := Repository{Path: repo, Name: "app"}
	tree := readChangeView(context.Background(), []Repository{r}, "index")[0]
	diff, err := readIndexComparison(context.Background(), tree, tree.Files[0], "index")
	if err != nil || diff.Before != "" || diff.After != "first\n" {
		t.Fatal("unborn staged diff", diff, err)
	}
	mustGit(t, repo, "commit", "-m", "initial")
	mustGit(t, repo, "mv", "new.txt", "renamed.txt")
	tree = readChangeView(context.Background(), []Repository{r}, "index")[0]
	diff, err = readIndexComparison(context.Background(), tree, tree.Files[0], "index")
	if err != nil || diff.Before != "first\n" || diff.After != "first\n" {
		t.Fatal("staged rename", diff, err)
	}
	mustGit(t, repo, "commit", "-m", "rename")
	mustGit(t, repo, "rm", "renamed.txt")
	tree = readChangeView(context.Background(), []Repository{r}, "index")[0]
	diff, err = readIndexComparison(context.Background(), tree, tree.Files[0], "index")
	if err != nil || diff.Before != "first\n" || diff.After != "" {
		t.Fatal("staged deletion", diff, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "binary.dat"), []byte{0, 1, 2}, 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "binary.dat")
	diff, err = readIndexComparison(context.Background(), tree, WorkingFile{Path: "binary.dat", Status: "A "}, "index")
	if err != nil || !strings.Contains(diff.Notice, "binary") {
		t.Fatal("binary index diff", diff.Notice, err)
	}
}

func TestEditorTabKeyboardAndCloseContextMenu(t *testing.T) {
	a := testClient(nil)
	first := a.openEditor("first", "first.txt", "file", "").ID
	second := a.openEditor("second", "second.txt", "file", "").ID
	tester := ui.NewTester(a.view, 1560, 940)
	tester.Key(ui.Ctrl, ui.KeyTab)
	if a.editorActive != first {
		t.Fatal("next tab shortcut failed")
	}
	tester.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	if a.editorActive != second {
		t.Fatal("previous tab shortcut failed")
	}
	if err := tester.RightClick("Editor Tab " + first); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("Close Other Tabs"); err != nil {
		t.Fatal(err)
	}
	if len(a.editorTabs) != 1 || a.editorActive != first {
		t.Fatal("close other tabs failed")
	}
	tester.Key(primaryShortcutModifier(), ui.KeyW)
	if len(a.editorTabs) != 0 {
		t.Fatal("close tab shortcut failed")
	}
}
func TestSameTargetDifferentBasesKeepDistinctFileTabs(t *testing.T) {
	a := divergentCompareClient(t)
	for _, base := range []string{"main", "side-a"} {
		a.beginReview(ReviewRequest{Kind: "direct", Base: base, Target: "side-b", Repositories: a.repos, Net: true})
		for _, item := range a.review.Items {
			if item.File.Path == "b.txt" {
				a.selectReviewItem(item)
				break
			}
		}
	}
	count := 0
	for _, tab := range a.editorTabs {
		if tab.Kind == "commit" && tab.File.Path == "b.txt" {
			count++
		}
	}
	if count != 2 {
		t.Fatal("different comparison bases overwrote the same file tab", count)
	}
}

func TestEditorViewStateSurvivesSwitches(t *testing.T) {
	a := testClient(nil)
	first := a.openEditor("one", "one", "changes", "").ID
	f := FileComparison{Filename: "file.go", Before: "before\n", After: "after\n"}
	f.prepare()
	state := a.editorReviewState(f)
	state.Full = true
	state.Hunk = 7
	state.SideBySide = true
	second := a.openEditor("two", "two", "changes", "").ID
	other := a.editorReviewState(f)
	if other == state {
		t.Fatal("editor view state was shared across tabs")
	}
	other.Full = false
	other.Hunk = 0
	a.activateEditor(first)
	restored := a.editorReviewState(f)
	if restored != state || !restored.Full || restored.Hunk != 7 || !restored.SideBySide {
		t.Fatal("tab lost its diff view state")
	}
	a.activateEditor(second)
	if a.editorReviewState(f).Full {
		t.Fatal("another tab inherited full-file mode")
	}
}
func TestScopedChangesUIAndRepositoryFileTabs(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "first.go", "package app\nconst First=1\n")
	writeCommit(t, repo, "second.go", "package app\nconst Second=2\n")
	writeWorkingFile(t, repo, "first.go", "package app\nconst First=3\n")
	mustGit(t, repo, "add", "first.go")
	writeWorkingFile(t, repo, "first.go", "package app\nconst First=4\n")
	r := inspect(Entry{Path: repo, Name: "app"})
	a := testClient([]Repository{r})
	a.readRunner = func(work func() func()) { work()() }
	a.tab = 1
	a.reloadWorkingTrees()
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Changes View Index"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Scoped Change index app/first.go"); err != nil {
		t.Fatal(err)
	}
	indexID := a.editorActive
	if !strings.Contains(a.activeEditor().Comparison.After, "First=3") {
		t.Fatal("Index view opened Worktree content")
	}
	if err := tester.Click("Changes View Worktree"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Scoped Change worktree app/first.go"); err != nil {
		t.Fatal(err)
	}
	if a.editorActive == indexID || !strings.Contains(a.activeEditor().Comparison.Before, "First=3") || !strings.Contains(a.activeEditor().Comparison.After, "First=4") {
		t.Fatal("Worktree view did not compare against Index")
	}
	state := readRepositoryFiles(context.Background(), r)
	a.repositoryFiles = map[string]repositoryFileState{r.Path: state}
	a.selectWorkingFileView(state.Tree, WorkingFile{Path: "first.go"}, true)
	a.switchTab(2)
	a.moveReviewFile(1)
	if a.activeEditor().Kind != "file" || a.activeEditor().File.Path != "second.go" {
		t.Fatal("file tab navigation changed with the sidebar tab")
	}
	a.activateEditor(indexID)
	tester.Frame()
	if output := os.Getenv("MYGO_INDEX_TABS_SCREENSHOT"); output != "" {
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
