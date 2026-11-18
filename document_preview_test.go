package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMarkdownAndHTMLDocumentRendering(t *testing.T) {
	markdown := "# Title\n\n**bold** and ~~removed~~\n\n| A | B |\n|---|---|\n| one | two |\n\n- [x] done\n\n```go\nfunc main() {}\n```\n<script>window.bad=true</script>\n"
	rendered, err := renderDocumentPreview(context.Background(), CodeSource{Path: "README.md"}, markdown)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`<h1 id="title">Title</h1>`, `<strong>bold</strong>`, `<table>`, `type="checkbox"`, `language-go`, `script-src &#39;none&#39;`} {
		if !strings.Contains(rendered, expected) {
			t.Fatal("missing rendered Markdown feature", expected, rendered)
		}
	}
	if strings.Contains(rendered, "<script") {
		t.Fatal("document script was retained")
	}
	input := `<!doctype html><html><head><style>.sample{color:red}</style><meta http-equiv="refresh" content="0;url=https://example.com"></head><body><h1 class="sample" onclick="alert(1)">Hello</h1><a href="javascript:alert(1)">bad</a><iframe srcdoc="secret"></iframe><script>alert(1)</script></body></html>`
	rendered, err = renderDocumentPreview(context.Background(), CodeSource{Path: "index.html"}, input)
	if err != nil || !strings.Contains(rendered, `.sample{color:red}`) || !strings.Contains(rendered, `class="sample"`) {
		t.Fatal("HTML content or author CSS was lost", rendered, err)
	}
	for _, blocked := range []string{"<script", "onclick=", "javascript:", "<iframe", "http-equiv=\"refresh\""} {
		if strings.Contains(rendered, blocked) {
			t.Fatal("active document content retained", blocked)
		}
	}
}
func TestPreviewAssetsUseMatchingRevisionAndStayInsideRepository(t *testing.T) {
	repo := testRepo(t)
	for _, dir := range []string{"assets", "docs"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	old := `<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`
	current := `<svg xmlns="http://www.w3.org/2000/svg"><rect fill="blue"/></svg>`
	writeCommit(t, repo, "assets/icon.svg", old)
	writeCommit(t, repo, "docs/theme.css", `.box{background-image:url(../assets/icon.svg)} `)
	writeCommit(t, repo, "docs/index.html", `<link rel="stylesheet" href="theme.css"><div class="box">Hello</div><img src="../assets/icon.svg">`)
	revision := mustGit(t, repo, "rev-parse", "HEAD")
	writeWorkingFile(t, repo, "assets/icon.svg", current)
	for _, test := range []struct{ Revision, Expected string }{{revision, old}, {"", current}} {
		source := CodeSource{Repository: repo, Path: "docs/index.html", Revision: test.Revision}
		rendered, err := renderDocumentPreview(context.Background(), source, `<link rel="stylesheet" href="theme.css"><div class="box">Hello</div><img src="../assets/icon.svg">`)
		if err != nil || !strings.Contains(rendered, base64.StdEncoding.EncodeToString([]byte(test.Expected))) || !strings.Contains(rendered, "background-image:url(") {
			t.Fatal("preview assets did not match the document revision", test.Revision, rendered, err)
		}
	}
	source := CodeSource{Repository: repo, Path: "docs/index.html"}
	if _, _, err := previewAsset(context.Background(), source, "../../outside.txt"); err == nil {
		t.Fatal("preview read outside repository")
	}
	if _, _, err := previewAsset(context.Background(), source, "../.git/config"); err == nil {
		t.Fatal("preview read Git metadata")
	}
	if _, _, err := previewAsset(context.Background(), source, "file:///etc/hosts"); err == nil {
		t.Fatal("preview accepted an absolute file URL")
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "external")); err == nil {
		if _, _, err := previewAsset(context.Background(), source, "../external/secret.txt"); err == nil {
			t.Fatal("preview followed a symlink outside repository")
		}
	}
}
func TestDocumentModesDefaultPreviewAndRemainPerTab(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "README.md", "# Hello\n\n**Markdown**\n")
	writeCommit(t, repo, "page.html", "<h1>HTML</h1>")
	r := inspect(Entry{Path: repo, Name: "app"})
	tree := readRepositoryFiles(context.Background(), r).Tree
	a := testClient([]Repository{r})
	a.readRunner = func(work func() func()) { work()() }
	a.selectWorkingFile(tree, WorkingFile{Path: "README.md"})
	md := a.editorActive
	tester := ui.NewTester(a.view, 1560, 940)
	if a.activeEditor().Document.Mode != "preview" || !strings.Contains(a.activeEditor().Document.AfterHTML, "<h1") || tester.HasText("# Hello") {
		t.Fatal("Markdown did not default to rendered preview", tester.Texts())
	}
	if err := tester.Click("Document Mode split"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("# Hello") || !tester.HasText("Document Preview") {
		t.Fatal("Split must show source and preview", tester.Texts())
	}
	if err := tester.Click("Document Mode source"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("# Hello") || tester.HasText("Document Preview") {
		t.Fatal("Source mode did not hide preview")
	}
	a.selectWorkingFile(tree, WorkingFile{Path: "page.html"})
	tester.Frame()
	if a.activeEditor().Document.Mode != "preview" {
		t.Fatal("HTML did not default to Preview")
	}
	a.activateEditor(md)
	tester.Frame()
	if a.activeEditor().Document.Mode != "source" {
		t.Fatal("document mode was not preserved per tab")
	}
}
func TestDocumentDiffRendersBothVersions(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "README.md", "# Before\n")
	writeWorkingFile(t, repo, "README.md", "# After\n")
	r := inspect(Entry{Path: repo, Name: "app"})
	tree := readWorkingTree(r)
	a := testClient([]Repository{r})
	a.tab = 1
	a.readRunner = func(work func() func()) { work()() }
	a.selectWorkingFile(tree, tree.Files[0])
	tab := a.activeEditor()
	if !strings.Contains(tab.Document.BeforeHTML, ">Before</h1>") || !strings.Contains(tab.Document.AfterHTML, ">After</h1>") {
		t.Fatal("document diff did not render both versions", tab.Document)
	}
	tester := ui.NewTester(a.view, 1560, 940)
	if !tester.HasText("Before") || !tester.HasText("After") {
		t.Fatal("preview sides missing")
	}
	if err := tester.Click("Document Mode source"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("# Before") || !tester.HasText("# After") {
		t.Fatal("document source diff lost a side")
	}
}
func TestConcurrentMarkdownPreviewRendering(t *testing.T) {
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			for n := 0; n < 3; n++ {
				out, err := renderDocumentPreview(context.Background(), CodeSource{Path: "README.md"}, "# Concurrent\n\n- [x] test\n")
				if err != nil || !strings.Contains(out, "Concurrent") {
					t.Error("concurrent render failed", err)
				}
			}
		})
	}
	group.Wait()
}
func TestFileTypeIconsIncludeLanguageAndSpecialFiles(t *testing.T) {
	tests := map[string][]string{"Java": {"App.java", "App.class", "pom.xml", "build.gradle", "settings.gradle.kts", "mvnw"}, "Go": {"main.go", "go.mod", "go.sum", "go.work"}, "Python": {"app.py", "types.pyi", "pyproject.toml", "uv.lock", "requirements-dev.txt", "Pipfile.lock"}, "JavaScript": {"main.js", "Component.jsx", "package.json", "pnpm-lock.yaml", ".eslintrc.json"}, "TypeScript": {"main.ts", "Component.tsx", "tsconfig.json"}, "Markdown": {"README.md"}, "HTML": {"index.html"}}
	for kind, files := range tests {
		for _, file := range files {
			if actual := fileType(file); actual.Kind != kind || actual.Badge == "" {
				t.Errorf("%s: expected %s icon, got %+v", file, kind, actual)
			}
		}
	}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Tree(c, func() {
			for _, file := range []string{"App.java", "main.go", "app.py", "main.js"} {
				fileTreeItem(c.Key(file), file, file)
			}
		})
	}, 600, 300)
	for _, kind := range []string{"Java", "Go", "Python", "JavaScript"} {
		if _, ok := tester.Find(kind + " icon"); !ok {
			t.Fatal("missing tree icon", kind, tester.Texts())
		}
	}
}

func TestLanguageIconsAppearInRepositoryChangesReviewAndHistoryTrees(t *testing.T) {
	repo := testRepo(t)
	mustGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	for name, text := range map[string]string{"App.java": "class App {}\n", "main.go": "package app\n", "app.py": "print('hello')\n", "main.js": "console.log('hello')\n"} {
		writeWorkingFile(t, repo, name, text)
	}
	r := inspect(Entry{Path: repo, Name: "app"})
	a := testClient([]Repository{r})
	a.readRunner = func(work func() func()) { work()() }
	a.writeRunner = func(work func() func()) { work()() }
	a.repositoryFiles = map[string]repositoryFileState{repo: readRepositoryFiles(context.Background(), r)}
	*a.browserOpen("repository:" + repo) = true
	tester := ui.NewTester(a.view, 1560, 940)
	assertIcons := func(view string) {
		t.Helper()
		for _, kind := range []string{"Java", "Go", "Python", "JavaScript"} {
			if _, ok := tester.Find(kind + " icon"); !ok {
				t.Fatalf("%s: missing %s icon", view, kind)
			}
		}
	}
	assertIcons("Repositories")
	a.switchTab(1)
	tester.Frame()
	assertIcons("Changelists")
	mustGit(t, repo, "add", ".")
	a.changeViewMode = "index"
	a.reloadChangeView()
	tester.Frame()
	assertIcons("Index")
	a.tab = 2
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "HEAD", Net: true, Repositories: a.repos})
	tester.Frame()
	assertIcons("Review")
	a.closeReviewEditors()
	mustGit(t, repo, "commit", "-m", "add language files")
	a.changeViewMode = ""
	a.repos = inspectAll([]Entry{{Path: repo, Name: "app"}})
	a.reloadHistory()
	a.selectCommit(a.commits[0])
	tester.Frame()
	assertIcons("Commit files")
}
func TestPreviewRawStyleCannotBreakOutOfStyleElement(t *testing.T) {
	repo := testRepo(t)
	writeWorkingFile(t, repo, "theme.css", `body{color:red}</style><script>BAD()</script><style>`)
	out, err := renderDocumentPreview(context.Background(), CodeSource{Repository: repo, Path: "index.html"}, `<link rel="stylesheet" href="theme.css"><h1>Test</h1>`)
	if err != nil || strings.Contains(out, "<script>") {
		t.Fatal("stylesheet escaped preview isolation", out, err)
	}
}
