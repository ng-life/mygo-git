package main

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSameFileDefinitionLocatesCurrentEditorWithoutTabOrModal(t *testing.T) {
	repo := testRepo(t)
	text := "package main\nfunc local() {}\n" + strings.Repeat("// context\n", 90) + "func use() { local() }\n"
	writeWorkingFile(t, repo, "main.go", text)
	r := inspect(Entry{Path: repo, Name: "app"})
	tree := readRepositoryFiles(context.Background(), r).Tree
	a := testClient([]Repository{r})
	a.readRunner = func(work func() func()) { work()() }
	a.selectWorkingFile(tree, WorkingFile{Path: "main.go"})
	id := a.editorActive
	document := a.activeEditor().Comparison.AfterDocument
	a.navigateSymbol(CodeSource{Repository: repo, Path: "main.go"}, document, codeReference(t, document, 93, "local"))
	if a.codeNavigation.Open || a.editorActive != id || len(a.editorTabs) != 1 {
		t.Fatal("local definition opened another view")
	}
	tab := a.activeEditor()
	if tab.Code == nil || tab.Code.Cursor() != documentOffset(document, 2, 6) || tab.Caret.Line != 2 {
		t.Fatal("local caret did not move", tab.Caret)
	}
	tester := ui.NewTester(a.view, 1560, 940)
	tester.Frame()
	if !tab.Code.Focused() {
		t.Fatal("destination editor did not receive keyboard focus")
	}
	if tester.HasText("Go to Definition") {
		t.Fatal("unique local target opened a modal")
	}
}
func TestSameFileDiffDefinitionExpandsHiddenContextOnBothLayouts(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(map[bool]string{false: "Inline", true: "Side-by-side"}[split], func(t *testing.T) {
			repo := testRepo(t)
			before := "package main\nfunc local() {}\n" + strings.Repeat("// context\n", 90) + "func use() {}\n"
			writeCommit(t, repo, "main.go", before)
			after := strings.Replace(before, "func use() {}", "func use() { local() }", 1)
			writeWorkingFile(t, repo, "main.go", after)
			r := inspect(Entry{Path: repo, Name: "app"})
			tree := readWorkingTree(r)
			a := testClient([]Repository{r})
			a.tab = 1
			a.diffSideBySide = split
			a.readRunner = func(work func() func()) { work()() }
			a.selectWorkingFile(tree, tree.Files[0])
			id := a.editorActive
			f := a.activeEditor().Comparison
			state := a.editorReviewState(f)
			tester := ui.NewTester(a.view, 1560, 940)
			if err := tester.ClickWith(primaryShortcutModifier(), "Go to main.go:93:14 local"); err != nil {
				t.Fatal(err)
			}
			if a.editorActive != id || len(a.editorTabs) != 1 || a.codeNavigation.Open || a.activeEditor().Caret.Line != 2 {
				t.Fatal("same-file diff jump replaced the editor")
			}
			found := false
			for _, row := range state.Rows {
				if row.Kind != 'f' && row.Kind != 'h' && row.NewLine == 1 {
					found = true
				}
			}
			if !found {
				t.Fatal("declaration remained hidden in unchanged context")
			}
			tester.Frame()
			if !tester.HasText("local") || !tester.HasText("Ln 2, Col 6") {
				t.Fatal("definition was not revealed in the current diff", tester.Texts())
			}
		})
	}
}
func TestCrossFileUniqueDefinitionUsesRevisionAndReusesDestinationTab(t *testing.T) {
	repo := testRepo(t)
	writeWorkingFile(t, repo, "main.go", "package app\nfunc use() { target() }\n")
	writeWorkingFile(t, repo, "target.go", "package app\nfunc target() {}\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "navigation fixture")
	sha := mustGit(t, repo, "rev-parse", "HEAD")
	// The worktree definition is deliberately at another location.
	writeWorkingFile(t, repo, "target.go", "package app\n\n\nfunc target() {}\n")
	source := CodeSource{Repository: repo, Path: "main.go", Revision: sha}
	document, err := readCodeSource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	a := testClient([]Repository{{Path: repo, Name: "app"}})
	a.readRunner = func(work func() func()) { work()() }
	a.openDefinitionEditor(CodeTarget{Source: source, Definition: CodeDefinition{Line: 1, Column: 1}}, document)
	origin := a.editorActive
	a.navigateSymbol(source, document, codeReference(t, document, 2, "target"))
	tab := a.activeEditor()
	if tab.ID == origin || len(a.editorTabs) != 2 || a.codeNavigation.Open || tab.CodeSource.Path != "target.go" || tab.CodeSource.Revision != sha || tab.Caret.Line != 2 {
		t.Fatal("cross-file jump did not retain historical source", tab.CodeSource, tab.Caret)
	}
	destination := tab.ID
	a.loadEditorWorkingMode(destination, "file")
	if tab.CodeSource.Revision != sha || strings.Contains(tab.Code.Buffer.String(), "\n\n") {
		t.Fatal("refresh drifted to Worktree")
	}
	a.activateEditor(origin)
	a.navigateSymbol(source, document, codeReference(t, document, 2, "target"))
	if a.editorActive != destination || len(a.editorTabs) != 2 {
		t.Fatal("destination tab was duplicated")
	}
}
func TestDefinitionLookupDoesNotStealFocusAfterSwitchOrClose(t *testing.T) {
	repo := testRepo(t)
	text := "package main\nfunc local() {}\nfunc use() { local() }\n"
	writeWorkingFile(t, repo, "main.go", text)
	document := codeDocument("main.go", text)
	source := CodeSource{Repository: repo, Path: "main.go"}
	a := testClient(nil)
	a.openDefinitionEditor(CodeTarget{Source: source, Definition: CodeDefinition{Line: 1, Column: 1}}, document)
	origin := a.editorActive
	other := a.openEditor("other", "other", "file", "").ID
	a.activateEditor(origin)
	var queue []func() func()
	a.readRunner = func(work func() func()) { queue = append(queue, work) }
	a.navigateSymbol(source, document, codeReference(t, document, 3, "local"))
	result := queue[0]()
	a.activateEditor(other)
	result()
	if a.editorActive != other || a.codeNavigation.Open {
		t.Fatal("stale lookup stole focus")
	}
	a.activateEditor(origin)
	a.navigateSymbol(source, document, codeReference(t, document, 3, "local"))
	result = queue[1]()
	a.closeEditor(origin)
	result()
	if a.editorByID(origin) != nil || a.codeNavigation.Open {
		t.Fatal("closed editor was reopened by lookup")
	}
}

func TestMultipleCrossFileTargetsChooseFileAndPositionWithKeyboard(t *testing.T) {
	repo := testRepo(t)
	writeWorkingFile(t, repo, "main.go", "package app\nfunc use() { target() }\n")
	writeWorkingFile(t, repo, "target_linux.go", "//go:build linux\n\npackage app\nfunc target() {}\n")
	writeWorkingFile(t, repo, "target_windows.go", "//go:build windows\n\npackage app\nfunc target() {}\n")
	source := CodeSource{Repository: repo, Path: "main.go"}
	document, err := readCodeSource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	a := testClient([]Repository{{Path: repo, Name: "app"}})
	a.readRunner = func(work func() func()) { work()() }
	a.openDefinitionEditor(CodeTarget{Source: source, Definition: CodeDefinition{Line: 1, Column: 1}}, document)
	origin := a.editorActive
	a.navigateSymbol(source, document, codeReference(t, document, 2, "target"))
	tester := ui.NewTester(a.view, 1560, 940)
	tester.Frame()
	if len(a.codeNavigation.Targets) != 2 || !a.codeNavigation.Open || a.editorActive != origin {
		t.Fatal("ambiguous reference navigated before selection", a.codeNavigation.Targets)
	}
	if !tester.HasText("target_linux.go:4:6") || !tester.HasText("target_windows.go:4:6") {
		t.Fatal("chooser omitted files or line/column positions", tester.Texts())
	}
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyEnter)
	if a.codeNavigation.Open || a.activeEditor().CodeSource.Path != "target_windows.go" || a.activeEditor().Caret.Line != 4 || len(a.editorTabs) != 2 {
		t.Fatal("keyboard target selection did not navigate", a.editorActive, a.codeNavigation.Open, a.activeEditor().Caret)
	}
	before := a.activeEditor().Code.Buffer.String()
	tester.Type("X")
	if a.activeEditor().Code.Buffer.String() != before {
		t.Fatal("navigation source must be read-only")
	}
}
func TestSameFileAmbiguousSelectionStaysInCurrentTab(t *testing.T) {
	repo := testRepo(t)
	text := "class Service {\n public void Run(int x) {}\n public void Run(string x) {}\n public void Use() {\n Service svc = new Service();\n svc.Run(unknown);\n }\n}\n"
	writeWorkingFile(t, repo, "Service.cs", text)
	source := CodeSource{Repository: repo, Path: "Service.cs"}
	document := codeDocument("Service.cs", text)
	a := testClient(nil)
	a.readRunner = func(work func() func()) { work()() }
	a.openDefinitionEditor(CodeTarget{Source: source, Definition: CodeDefinition{Line: 1, Column: 1}}, document)
	id := a.editorActive
	a.navigateSymbol(source, document, codeReference(t, document, 6, "Run"))
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Run · Service · Service.cs:3:14"); err != nil {
		t.Fatal(err)
	}
	if a.codeNavigation.Open || a.editorActive != id || len(a.editorTabs) != 1 || a.activeEditor().Code.Cursor() != documentOffset(document, 3, 14) {
		t.Fatal("same-file target choice created a new tab or missed its caret")
	}
}
