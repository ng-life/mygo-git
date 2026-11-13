package main

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTagCreationHistoryAndProtectedDeletion(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "one\n")
	repo := inspect(Entry{Path: path, Name: "app"})
	request := tagDialogState{Action: "create", Name: "release/v1", Ref: "HEAD"}
	if out, err := executeTagOperation(repo, request); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := executeTagOperation(repo, request); err == nil {
		t.Fatal("existing tag overwritten")
	}
	request.Name = "release/v2"
	request.Annotated = true
	request.Message = "version two"
	if out, err := executeTagOperation(repo, request); err != nil {
		t.Fatalf("annotated: %s %v", out, err)
	}
	repo = inspect(Entry{Path: path, Name: "app"})
	if len(repo.Tags) != 2 || len(repo.TagObjects) != 2 {
		t.Fatalf("tags not loaded: %+v", repo)
	}
	if kind := mustGit(t, path, "cat-file", "-t", repo.TagObjects["release/v2"]); kind != "tag" {
		t.Fatal("annotation not stored")
	}
	sources := tagHistorySources([]Repository{repo}, "/release")
	if len(sources) != 1 || len(sources[0].Refs) != 2 {
		t.Fatal(sources)
	}
	commits, problem := readHistory(sources)
	if problem != "" || len(commits) != 2 {
		t.Fatalf("history %v %s", commits, problem)
	}
	// A branch and a tag may have identical names; refs stay fully qualified.
	mustGit(t, path, "branch", "release/v1", "HEAD~1")
	a := testClient([]Repository{inspect(Entry{Path: path, Name: "app"})})
	a.readRunner = func(w func() func()) { w()() }
	a.selectReference("/release/v1", "release/v1", "tags")
	if len(a.commits) != 2 || !a.groupTags || len(a.visible()) != 1 {
		t.Fatal("tag selected branch history")
	}
	a.selectReference("/release/v1", "release/v1", "local")
	if a.groupTags || len(a.commits) != 1 {
		t.Fatal("branch selected same-name tag history")
	}
	a.tab = 2
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Review…"); err != nil {
		t.Fatal(err)
	}
	if a.review.Target != "refs/heads/release/v1" {
		t.Fatal("Review used an ambiguous branch/tag name", a.review.Target)
	}

	request = tagDialogState{Action: "delete", Name: "release/v1"}
	mustGit(t, path, "tag", "-f", "release/v1", "HEAD~1")
	if _, err := executeTagOperation(repo, request); err == nil {
		t.Fatal("changed tag deleted")
	}
	repo = inspect(Entry{Path: path})
	if out, err := executeTagOperation(repo, request); err != nil {
		t.Fatalf("delete: %s %v", out, err)
	}
	if _, err := git(path, "show-ref", "--verify", "refs/heads/release/v1"); err != nil {
		t.Fatal("same-name branch deleted")
	}
}
func TestTagCheckoutAndPush(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "one\n")
	mustGit(t, path, "tag", "-a", "-m", "version", "v1")
	repo := inspect(Entry{Path: path})
	writeWorkingFile(t, path, "file.txt", "dirty\n")
	request := tagDialogState{Action: "checkout", Name: "v1"}
	if _, err := executeTagOperation(repo, request); err == nil {
		t.Fatal("dirty checkout accepted")
	}
	mustGit(t, path, "checkout", "--", "file.txt")
	if out, err := executeTagOperation(repo, request); err != nil {
		t.Fatalf("checkout %s %v", out, err)
	}
	if _, err := git(path, "symbolic-ref", "HEAD"); err == nil {
		t.Fatal("checkout not detached")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, path, "init", "--bare", remote)
	mustGit(t, path, "remote", "add", "origin", remote)
	request.Action = "push"
	request.Remote = "origin"
	if out, err := executeTagOperation(repo, request); err != nil {
		t.Fatalf("push %s %v", out, err)
	}
	if got := mustGit(t, path, "ls-remote", "origin", "refs/tags/v1"); !strings.HasPrefix(got, repo.TagObjects["v1"]) {
		t.Fatal(got)
	}
	// A different remote tag must never be forced over.
	mustGit(t, path, "tag", "-f", "v1", "HEAD~1")
	repo = inspect(Entry{Path: path})
	if _, err := executeTagOperation(repo, request); err == nil {
		t.Fatal("remote tag overwritten")
	}
	request.Remote = "--all"
	if _, err := executeTagOperation(repo, request); err == nil {
		t.Fatal("non-remote argument accepted")
	}
	mustGit(t, remote, "update-ref", "refs/tags/remote/v2", mustGit(t, path, "rev-parse", "HEAD"))
	mustGit(t, path, "tag", "-f", "v1", mustGit(t, remote, "rev-parse", "refs/tags/v1"))
	a := testClient([]Repository{inspect(Entry{Path: path})})
	a.readRunner = func(w func() func()) { w()() }
	a.writeRunner = func(w func() func()) { w()() }
	a.fetchTags(a.repos)
	if !slices.Contains(a.repos[0].Tags, "remote/v2") {
		t.Fatal("remote tags not fetched", a.output)
	}

}
func TestLogsCombinedNavigationAndTagDialogUI(t *testing.T) {
	first, second := testRepo(t), testRepo(t)
	for _, path := range []string{first, second} {
		mustGit(t, path, "branch", "feature/review")
		mustGit(t, path, "tag", "release/v1")
		mustGit(t, path, "update-ref", "refs/remotes/origin/feature/review", "HEAD")
	}
	repos := []Repository{inspect(Entry{Path: first, Name: "app"}), inspect(Entry{Path: second, Name: "api"})}
	a := testClient(repos)
	a.readRunner = func(w func() func()) { w()() }
	a.writeRunner = func(w func() func()) { w()() }
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("搜索分支或 Tag") {
		t.Fatal("Logs navigation in Changes")
	}
	if err := tester.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"HEAD (Current Branch)", "Local", "Remote", "Tags", "review  (2)", "Remote review  (2)", "Tag v1  (2)"} {
		if !tester.HasText(text) {
			t.Fatal(text, tester.Texts())
		}
	}
	if err := tester.Click("Tag v1  (2)"); err != nil {
		t.Fatal(err)
	}
	if !a.groupTags || len(a.commits) != 2 {
		t.Fatal("tag scope not selected")
	}
	if err := tester.RightClick("Tag v1  (2)"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "删除本地 Tag…") {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	if dest := os.Getenv("MYGO_LOGS_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tester.Click("新建 Tag…"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Tag 名称"); err != nil {
		t.Fatal(err)
	}
	tester.Type("release/v2")
	if err := tester.Click("Tag 仓库 api"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("确认新建 Tag"); err != nil {
		t.Fatal(err)
	}
	if len(a.repos[0].Tags) != 2 || len(a.repos[1].Tags) != 1 {
		t.Fatal("Tag repository selection ignored", a.output)
	}
	a.openTagDialog("delete", "release/v1", a.repos)
	tester.Frame()
	if !tester.HasText("保留远程 Tag") {
		t.Fatal("delete scope not explained")
	}
	if err := tester.Click("确认删除本地 Tag"); err != nil {
		t.Fatal(err)
	}
	if len(referenceTargets(a.repos, "release/v1", "tags")) != 0 || a.groupTags {
		t.Fatal("deleted tag scope not cleared")
	}
	if err := tester.Click("HEAD (Current Branch)"); err != nil {
		t.Fatal(err)
	}
	if a.group != "" || a.groupTags || a.groupRemote {
		t.Fatal("HEAD did not reset scope")
	}
	tester.Frame()
}
