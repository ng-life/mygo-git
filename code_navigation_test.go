package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func codeDocument(file, text string) DiffDocument {
	d := prepareDiff(text)
	highlightDocument(&d, file)
	return d
}
func codeReference(t *testing.T, document DiffDocument, line int, symbol string) CodeReference {
	t.Helper()
	column := strings.LastIndex(document.Lines[line-1], symbol)
	ref, ok := document.Analysis.reference(line-1, column)
	if !ok {
		t.Fatalf("reference not found: %d %s", line, symbol)
	}
	return ref
}
func TestCompactWorkingFoldersRetainSelectionsAndBranches(t *testing.T) {
	files := []WorkingFile{{Path: "src/main/java/app/One.java"}, {Path: "src/main/java/app/Two.java"}, {Path: "src/test/Test.java"}}
	root := workingHierarchy(files)
	if root.Children[0].Name != "src" || len(root.Children[0].Children) != 2 {
		t.Fatal(root.Children)
	}
	main := root.Children[0].Children[0]
	if main.Name != "main/java/app" || main.Path != "src/main/java/app" || len(main.Files) != 2 {
		t.Fatalf("%+v", main)
	}
	a := testClient(nil)
	a.toggleWorkingFiles("repo", main.Files, true)
	if a.workingCheckState("repo", files) != ui.CheckMixed {
		t.Fatal("compaction changed file selection scope")
	}
	single := workingHierarchy([]WorkingFile{{Path: "a/b/c/file.go"}})
	if single.Children[0].Name != "a/b/c" || single.Children[0].Children[0].File == nil {
		t.Fatal("single folder chain not compacted")
	}
}
func TestCodeLocalGoLexicalAndReceiverResolution(t *testing.T) {
	text := "package main\ntype Service struct {}\nfunc (s *Service) Run() {}\nfunc main() {\n value := 1\n {\n value := 2\n println(value)\n }\n println(value)\n svc := &Service{}\n svc.Run()\n}\n"
	document := codeDocument("main.go", text)
	for _, test := range []struct {
		line   int
		symbol string
		want   int
	}{{8, "value", 7}, {10, "value", 5}, {12, "Run", 3}} {
		ref := codeReference(t, document, test.line, test.symbol)
		targets := document.Analysis.localTargets(ref)
		if len(targets) != 1 || targets[0].Line != test.want {
			t.Fatalf("%+v -> %+v", ref, targets)
		}
	}
	comment := codeDocument("main.go", "package main\n// Run()\nvar s = \"Run()\"\n")
	if _, ok := comment.Analysis.reference(1, 3); ok {
		t.Fatal("comment produced reference")
	}
	if _, ok := comment.Analysis.reference(2, 9); ok {
		t.Fatal("string produced reference")
	}
}
func TestCodeLocalFirstDoesNotRequireRipgrep(t *testing.T) {
	document := codeDocument("main.go", "package main\nfunc local() {}\nfunc use() { local() }\n")
	t.Setenv("PATH", t.TempDir())
	resolution, err := resolveCode(context.Background(), CodeSource{Repository: t.TempDir(), Path: "main.go"}, document, codeReference(t, document, 3, "local"))
	if err != nil || len(resolution.Targets) != 1 || resolution.Targets[0].Definition.Line != 2 {
		t.Fatal(resolution, err)
	}
}
func TestCodeGoRipgrepFiltersReferencesAndImports(t *testing.T) {
	repo := testRepo(t)
	writeWorkingFile(t, repo, "go.mod", "module example.test/app\n")
	if err := os.MkdirAll(filepath.Join(repo, "helper"), 0700); err != nil {
		t.Fatal(err)
	}
	text := "package main\nimport api \"example.test/app/helper\"\nfunc main() { api.Save(\"x\") }\n"
	writeWorkingFile(t, repo, "main.go", text)
	writeWorkingFile(t, repo, "helper/service.go", "package helper\nfunc Save(value string) {}\n")
	writeWorkingFile(t, repo, "helper/reference.go", "package helper\n// Save(value string)\nfunc unused() { Save(\"x\") }\n")
	writeWorkingFile(t, repo, "unrelated.go", "package main\nfunc Save(value string) {}\n")
	document := codeDocument("main.go", text)
	result, err := resolveCode(context.Background(), CodeSource{Repository: repo, Path: "main.go"}, document, codeReference(t, document, 3, "Save"))
	if err != nil || len(result.Targets) != 1 || result.Targets[0].Source.Path != "helper/service.go" {
		t.Fatal(result, err)
	}
}
func TestCodeHistoricalJumpUsesMatchingRevision(t *testing.T) {
	repo := testRepo(t)
	text := "package main\nfunc use() { Saved() }\n"
	writeCommit(t, repo, "main.go", text)
	writeCommit(t, repo, "target.go", "package main\nfunc Saved() {}\n")
	revision := mustGit(t, repo, "rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(repo, "target.go")); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, repo, "wrong.go", "package main\nfunc Saved() {}\n")
	document := codeDocument("main.go", text)
	result, err := resolveCode(context.Background(), CodeSource{Repository: repo, Path: "main.go", Revision: revision}, document, codeReference(t, document, 2, "Saved"))
	if err != nil || len(result.Targets) != 1 || result.Targets[0].Source.Path != "target.go" || result.Targets[0].Source.Revision != revision {
		t.Fatal(result, err)
	}
}
func TestCodeCSharpOverloadsReturnMultipleSemanticTargets(t *testing.T) {
	text := "class Service {\n public void Run(int value) {}\n public void Run(string value) {}\n public void Use() {\n Service svc = new Service();\n svc.Run(\"one\");\n }\n}\n"
	document := codeDocument("Service.cs", text)
	ref := codeReference(t, document, 6, "Run")
	targets := document.Analysis.localTargets(ref)
	if len(targets) != 2 {
		t.Fatalf("%+v -> %+v defs=%+v", ref, targets, document.Analysis.Definitions)
	}
	a := testClient(nil)
	a.readRunner = func(work func() func()) { work()() }
	a.navigateSymbol(CodeSource{Repository: t.TempDir(), Path: "Service.cs"}, document, ref)
	if len(a.codeNavigation.Targets) != 2 || a.codeNavigation.Preview {
		t.Fatal("multiple targets must request choice")
	}
}
func TestCtrlClickDefinitionPreviewAndClose(t *testing.T) {
	repo := testRepo(t)
	text := "package main\nfunc local() {}\nfunc use() { local() }\n"
	writeWorkingFile(t, repo, "main.go", text)
	document := codeDocument("main.go", text)
	source := CodeSource{Repository: repo, Path: "main.go"}
	a := testClient(nil)
	a.readRunner = func(work func() func()) { work()() }
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			codeLineWithNavigation(c, document, 2, c.Theme().Text, &CodeNavigation{Client: a, Source: source})
			a.codeNavigationPanel(c)
		})
	}, 1300, 800)
	label := "跳转 main.go:3:14 local"
	if err := tester.Click(label); err != nil {
		t.Fatal(err)
	}
	if a.codeNavigation.Open {
		t.Fatal("ordinary click triggered navigation")
	}
	if err := tester.ClickWith(ui.Ctrl, label); err != nil {
		t.Fatal(err)
	}
	if !a.codeNavigation.Open || !a.codeNavigation.Preview || a.codeNavigation.Target.Definition.Line != 2 {
		t.Fatalf("Ctrl+click failed: %+v", a.codeNavigation)
	}
	if output := os.Getenv("MYGO_CODE_JUMP_SCREENSHOT"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, tester.Image()); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	if err := tester.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if a.codeNavigation.Open {
		t.Fatal("preview did not close")
	}
}

func TestCodeJavaScriptRelativeImportAlias(t *testing.T) {
	repo := testRepo(t)
	text := "import { save as persist } from './store';\nfunction use() { persist(\"text\"); }\n"
	writeWorkingFile(t, repo, "main.ts", text)
	writeWorkingFile(t, repo, "store.ts", "export function save(value: string) {}\n")
	writeWorkingFile(t, repo, "wrong.ts", "export function save(value: string) {}\n")
	document := codeDocument("main.ts", text)
	ref := codeReference(t, document, 2, "persist")
	result, err := resolveCode(context.Background(), CodeSource{Repository: repo, Path: "main.ts"}, document, ref)
	if err != nil || len(result.Targets) != 1 || result.Targets[0].Source.Path != "store.ts" {
		t.Fatalf("%+v %v imports=%+v", result, err, document.Analysis.Imports)
	}
}
func TestCodePythonLocalVariablesAndMethods(t *testing.T) {
	text := "class Service:\n    def run(self, value):\n        return value\n\ndef use():\n    svc = Service()\n    svc.run(1)\n"
	document := codeDocument("main.py", text)
	for _, test := range []struct {
		line   int
		symbol string
		want   int
	}{{3, "value", 2}, {7, "run", 2}} {
		ref := codeReference(t, document, test.line, test.symbol)
		targets := document.Analysis.localTargets(ref)
		if len(targets) != 1 || targets[0].Line != test.want {
			t.Fatalf("%+v => %+v defs=%+v", ref, targets, document.Analysis.Definitions)
		}
	}
}
func TestCodeMultipleTargetsChooserAndStaleResponse(t *testing.T) {
	repo := testRepo(t)
	text := "class Service {\n public void Run(int x) {}\n public void Run(string x) {}\n public void Use() {\n Service svc = new Service();\n svc.Run(unknown);\n }\n}\n"
	writeWorkingFile(t, repo, "Service.cs", text)
	document := codeDocument("Service.cs", text)
	source := CodeSource{Repository: repo, Path: "Service.cs"}
	a := testClient(nil)
	a.readRunner = func(work func() func()) { work()() }
	a.navigateSymbol(source, document, codeReference(t, document, 6, "Run"))
	tester := ui.NewTester(func(c *ui.Context) { a.codeNavigationPanel(c) }, 1300, 800)
	if !tester.HasText("找到 2 个目标，请选择") {
		t.Fatal(tester.Texts())
	}
	if err := tester.Click("Run · Service · Service.cs:3"); err != nil {
		t.Fatal(err)
	}
	if !a.codeNavigation.Preview || a.codeNavigation.Target.Definition.Line != 3 {
		t.Fatal("target choice not applied")
	}
	if err := tester.Click("返回目标列表"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("找到 2 个目标，请选择") {
		t.Fatal("could not return to targets")
	}
	var queue []func() func()
	a.readRunner = func(work func() func()) { queue = append(queue, work) }
	a.navigateSymbol(source, document, codeReference(t, document, 6, "Run"))
	apply := queue[0]()
	a.closeCodeNavigation()
	apply()
	if a.codeNavigation.Open || a.codeNavigation.Loading == false {
		t.Fatal("closed lookup result was applied")
	}
}

func TestCompactWorkingTreeCheckboxUI(t *testing.T) {
	repo := Repository{Path: "/repo/app", Name: "app", Branch: "main"}
	files := []WorkingFile{{Path: "src/main/java/One.java"}, {Path: "src/main/java/Two.java"}}
	a := testClient([]Repository{repo})
	a.tab = 1
	a.workingTrees = []WorkingTree{{Repository: repo, Files: files}}
	tester := ui.NewTester(a.view, 1560, 940)
	if !slices.Contains(tester.Texts(), "src/main/java") || slices.Contains(tester.Texts(), "src") {
		t.Fatal("compact directory label missing", tester.Texts())
	}
	if err := tester.Click("选择文件夹 app/src/main/java"); err != nil {
		t.Fatal(err)
	}
	if a.workingCheckState(repo.Path, files) != ui.CheckOn {
		t.Fatal("compact checkbox did not select descendants")
	}
	if output := os.Getenv("MYGO_COMPACT_TREE_SCREENSHOT"); output != "" {
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
func TestJavaScriptArrowFunctionLocalJump(t *testing.T) {
	document := codeDocument("main.ts", "const save = (value: string) => { return value; };\nfunction use() { save(\"one\"); }\n")
	ref := codeReference(t, document, 2, "save")
	targets := document.Analysis.localTargets(ref)
	if len(targets) != 1 || targets[0].Line != 1 {
		t.Fatal(targets)
	}
}
