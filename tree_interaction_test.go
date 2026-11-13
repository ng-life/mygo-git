package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func doubleTreeContent(t *testing.T, tester *ui.Tester, label string) {
	t.Helper()
	tester.Advance(time.Second)
	if err := tester.Click(label); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click(label); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
}
func clickNativeTreeArrow(t *testing.T, tester *ui.Tester, label string, depth int) {
	t.Helper()
	row, found := tester.Find(label)
	if !found {
		t.Fatalf("missing tree item %s", label)
	}
	tester.Advance(time.Second)
	tester.ClickAt(row.X+ideaTheme.Space(3)+float32(depth)*ideaTheme.Space(4), row.Y+row.H/2)
}
func TestUnifiedRepositoryTreeInteraction(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.readRunner = func(w func() func()) { w()() }
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("app"); err != nil {
		t.Fatal(err)
	}
	if *a.browserOpen("repository:" + path) || a.repositoryPath != path {
		t.Fatal("single click should select without expanding")
	}
	doubleTreeContent(t, tester, "app")
	if !*a.browserOpen("repository:" + path) || !a.repositoryFiles[path].Loaded {
		t.Fatal("double click did not expand and lazy load")
	}
	doubleTreeContent(t, tester, "app")
	if *a.browserOpen("repository:" + path) {
		t.Fatal("double click did not collapse")
	}
	if err := tester.Click("展开仓库 app"); err != nil {
		t.Fatal(err)
	}
	if !*a.browserOpen("repository:" + path) {
		t.Fatal("arrow failed")
	}
	tester.Frame()
	tester.Frame()
	if !*a.browserOpen("repository:" + path) {
		t.Fatal("extra frames repeated gesture")
	}
}
func TestUnifiedChangesTreeInteraction(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	a.workingTrees[0].Files[0].Path = "src/deep/order.txt"
	tester := ui.NewTester(a.view, 1560, 940)
	// Root content selects history without changing submission checkboxes.
	doubleTreeContent(t, tester, "仓库名 supplychain")
	if !a.workingCollapsed[a.repos[0].Path] {
		t.Fatal("Changes root did not collapse")
	}
	if len(a.workingRequests("")) != 0 {
		t.Fatal("double-clicking root changed commit checkboxes")
	}

	if err := tester.Click("展开或折叠 supplychain"); err != nil {
		t.Fatal(err)
	}
	if a.workingCollapsed[a.repos[0].Path] {
		t.Fatal("Changes arrow did not expand")
	}
	key := workingFileKey(a.repos[0].Path, "src/deep")
	if err := tester.Click("文件夹 src/deep"); err != nil {
		t.Fatal(err)
	}
	if a.workingCollapsed[key] {
		t.Fatal("single folder click changed expansion")
	}
	doubleTreeContent(t, tester, "文件夹 src/deep")
	if !a.workingCollapsed[key] {
		t.Fatal("Changes folder double click failed")
	}
	if err := tester.Click("展开或折叠 src/deep"); err != nil {
		t.Fatal(err)
	}
	if a.workingCollapsed[key] {
		t.Fatal("Changes folder arrow failed")
	}

}
func TestUnifiedLogsTreeInteraction(t *testing.T) {
	a := testClient([]Repository{{Name: "app", Path: "/app", Branches: []string{"feature/review"}, RemoteBranches: []string{"origin/feature/review"}, Tags: []string{"release/v1"}}})
	a.switchTab(2)
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Local"); err != nil {
		t.Fatal(err)
	}
	if !*a.referenceOpen("logs-section:local") {
		t.Fatal("single section click collapsed tree")
	}
	doubleTreeContent(t, tester, "Local")
	if *a.referenceOpen("logs-section:local") {
		t.Fatal("section double click failed")
	}
	clickNativeTreeArrow(t, tester, "Local", 0)
	if !*a.referenceOpen("logs-section:local") {
		t.Fatal("section arrow failed")
	}
	if err := tester.Click("feature  (1)"); err != nil {
		t.Fatal(err)
	}
	if !*a.referenceOpen("logs:local/feature") || a.group != "/feature" {
		t.Fatal("folder selection changed expansion")
	}
	doubleTreeContent(t, tester, "feature  (1)")
	if *a.referenceOpen("logs:local/feature") {
		t.Fatal("folder double click failed")
	}
	a.selectReference("", "", "")
	tester.Frame()
	clickNativeTreeArrow(t, tester, "feature  (1)", 1)
	if a.group != "" {
		t.Fatal("arrow changed selected Log scope")
	}
	if !*a.referenceOpen("logs:local/feature") {
		t.Fatal("folder arrow failed")
	}
	doubleTreeContent(t, tester, "Remote")
	if *a.referenceOpen("logs-section:remote") {
		t.Fatal("Remote double click failed")
	}
	doubleTreeContent(t, tester, "Tags")
	if *a.referenceOpen("logs-section:tags") {
		t.Fatal("Tags double click failed")
	}
}

func TestUnifiedReviewTreeInteraction(t *testing.T) {
	path := testRepo(t)
	if err := os.MkdirAll(filepath.Join(path, "src", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	writeCommit(t, path, "src/deep/file.txt", "base\n")
	writeWorkingFile(t, path, "src/deep/file.txt", "changed\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.readRunner = func(w func() func()) { w()() }
	a.beginReview(ReviewRequest{Kind: "workspace", Base: "HEAD", Repositories: []Repository{repo}})
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Review 仓库 app"); err != nil {
		t.Fatal(err)
	}
	if !*a.referenceOpen("review-repository:" + path) {
		t.Fatal("single Review root click collapsed tree")
	}
	doubleTreeContent(t, tester, "Review 仓库 app")
	if *a.referenceOpen("review-repository:" + path) {
		t.Fatal("Review root double click failed")
	}
	clickNativeTreeArrow(t, tester, "Review 仓库 app", 0)
	if !*a.referenceOpen("review-repository:" + path) {
		t.Fatal("Review root arrow failed")
	}
	folderKey := "review-folder:" + path + "/src/deep"
	if err := tester.Click("src/deep"); err != nil {
		t.Fatal(err)
	}
	if !*a.expanded[folderKey] {
		t.Fatal("single Review folder click collapsed tree")
	}
	doubleTreeContent(t, tester, "src/deep")
	if *a.expanded[folderKey] {
		t.Fatal("Review folder double click failed")
	}
	clickNativeTreeArrow(t, tester, "src/deep", 1)
	if !*a.expanded[folderKey] {
		t.Fatal("Review folder arrow failed")
	}
	if err := tester.Click("Review 文件 src/deep/file.txt"); err != nil {
		t.Fatal(err)
	}
	if a.selectedFile == "" {
		t.Fatal("file selection stopped working")
	}
}
