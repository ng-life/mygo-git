package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestWorkingHierarchySelectionAndDisclosure(t *testing.T) {
	a := workingClient(t)
	path := a.repos[0].Path
	if err := os.MkdirAll(filepath.Join(path, "src", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, path, "src/a.go", "package a\n")
	writeWorkingFile(t, path, "src/nested/b.go", "package b\n")
	a.switchTab(1)
	tester := ui.NewTester(a.view, 1560, 940)
	for _, label := range []string{"src", "nested", "a.go", "b.go"} {
		if !tester.HasText(label) {
			t.Fatal("missing tree node", label)
		}
	}
	if err := tester.Click("选择文件夹 supplychain/src/nested"); err != nil {
		t.Fatal(err)
	}
	tree := a.workingTrees[0]
	if a.workingCheckState(path, tree.Files) != ui.CheckMixed || !a.workingSelection[workingFileKey(path, "src/nested/b.go")] || a.workingSelection[workingFileKey(path, "src/a.go")] {
		t.Fatal("nested selection did not propagate")
	}
	if err := tester.Click("选择文件夹 supplychain/src"); err != nil {
		t.Fatal(err)
	}
	if !a.workingSelection[workingFileKey(path, "src/a.go")] {
		t.Fatal("mixed folder click must select descendants")
	}
	if err := tester.Click("选择文件夹 supplychain/src"); err != nil {
		t.Fatal(err)
	}
	if a.workingSelection[workingFileKey(path, "src/nested/b.go")] {
		t.Fatal("folder deselection failed")
	}
	if err := tester.Click("展开或折叠 src"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("a.go") || tester.HasText("b.go") {
		t.Fatal("folder not collapsed")
	}
	if err := tester.Click("展开或折叠 src"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("b.go") {
		t.Fatal("folder did not reopen")
	}
	if _, exists := a.workingCollapsed[workingFileKey(path, "src/a.go")]; exists {
		t.Fatal("leaf must not have disclosure state")
	}
}
func reviewFixture(t *testing.T) FileComparison {
	t.Helper()
	path := testRepo(t)
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("// unchanged line %d", i))
	}
	before := strings.Join(lines, "\n") + "\n"
	writeCommit(t, path, "main.go", before)
	lines[9] = "func first() string { return \"one\" }"
	lines[79] = "func second() int { return 42 }"
	writeWorkingFile(t, path, "main.go", strings.Join(lines, "\n")+"\n")
	tree := readWorkingTree(inspect(Entry{Path: path}))
	comparison, err := readWorkingComparison(tree, tree.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	return comparison
}
func TestReviewHunksContextExpansionAndNavigation(t *testing.T) {
	f := reviewFixture(t)
	if f.ReviewHunks != 2 {
		t.Fatalf("hunks: %+v", f.Hunks)
	}
	if len(f.ReviewRows) > 30 {
		t.Fatal("unchanged context not folded")
	}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { reviewComparison(c, f, "before", "after") })
	}, 1000, 450)
	if tester.HasText("// unchanged line 30") || !tester.HasText("func first() string { return \"one\" }") {
		t.Fatal("incorrect first hunk view", tester.Texts())
	}
	if err := tester.Click("下一变更块"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("2 / 2") || !tester.HasText("func second() int { return 42 }") {
		t.Fatal("next hunk navigation failed", tester.Texts())
	}
	tester.Key(ui.Shift, ui.KeyF7)
	if !tester.HasText("1 / 2") {
		t.Fatal("previous hunk shortcut failed")
	}
	tester.Scroll(300, 220, 0, -100000)
	if err := tester.Click("展开 6 行未修改内容"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("// unchanged line 1") {
		t.Fatal("fold expansion did not reveal context")
	}
	if err := tester.Click("切换 diff 模式"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("变更块") {
		t.Fatal("full-file mode not entered")
	}
	if err := tester.Click("切换 diff 模式"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if output := os.Getenv("MYGO_REVIEW_SCREENSHOT"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
func TestReviewFileNavigationAcrossRepositoriesAndBinary(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	first := a.workingTrees[0]
	a.selectWorkingFile(first, first.Files[0])
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("下一文件"); err != nil {
		t.Fatal(err)
	}
	if a.workingFileTree.Repository.Path != a.workingTrees[1].Repository.Path {
		t.Fatal("did not navigate across repositories")
	}
	tester.Key(ui.Shift, ui.KeyF8)
	if a.workingFileTree.Repository.Path != first.Repository.Path {
		t.Fatal("previous file shortcut failed")
	}
	a.workingComparison.Notice = "二进制文件"
	tester.Frame()
	if err := tester.Click("下一文件"); err != nil {
		t.Fatal("binary file traps review navigation", err)
	}
	tester.Key(ui.Shift, ui.KeyF8)
	tester.Key(0, ui.KeyF8)
	if a.workingFileTree.Repository.Path != a.workingTrees[1].Repository.Path {
		t.Fatal("repeated shortcuts must survive file changes")
	}
}
func TestSyntaxHighlightPreservesMultilineStateAndPlainText(t *testing.T) {
	f := FileComparison{Filename: "main.go", After: "package main\n/* begin\ncomment remains\n*/\nfunc main() { println(42) }\n"}
	f.prepare()
	if f.AfterDocument.Language != "Go" {
		t.Fatal("Go not detected")
	}
	found := false
	for _, token := range f.AfterDocument.Tokens[2] {
		if token.Kind == editor.TokenComment {
			found = true
		}
	}
	if !found {
		t.Fatal("multiline comment state lost")
	}
	plain := FileComparison{Filename: "unknown.xyzzy", After: "raw plain text"}
	plain.prepare()
	if plain.AfterDocument.Language != "" || len(plain.AfterDocument.Tokens) != 0 {
		t.Fatal("unknown files should stay plain text")
	}
}
func TestReviewInsertionDeletionAlignmentAndFallback(t *testing.T) {
	for _, test := range []struct{ before, after, patch string }{
		{"a\nb\n", "new\na\nb\n", "@@ -0,0 +1 @@"},
		{"a\nb\n", "b\n", "@@ -1 +0,0 @@"},
		{"", "new\n", "@@ -0,0 +1 @@"},
		{"old\n", "", "@@ -1 +0,0 @@"},
	} {
		f := FileComparison{Before: test.before, After: test.after, Hunks: parseDiffHunks(test.patch)}
		f.prepare()
		for _, row := range f.ReviewRows {
			if row.Kind == ' ' || row.Kind == '-' {
				if row.OldLine < 0 || row.OldLine >= len(f.BeforeDocument.Lines) {
					t.Fatal("invalid old row", row)
				}
			}
			if row.Kind == ' ' || row.Kind == '+' {
				if row.NewLine < 0 || row.NewLine >= len(f.AfterDocument.Lines) {
					t.Fatal("invalid new row", row)
				}
			}
		}
		if f.ReviewHunks != 1 {
			t.Fatal("missing insertion/deletion hunk")
		}
	}
	stale := FileComparison{Before: "before", After: "after", Hunks: []DiffHunk{{0, 100, 0, 100}}}
	stale.prepare()
	if len(stale.ReviewRows) > 4 {
		t.Fatal("invalid offsets not bounded")
	}
}
