package main

import (
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func referenceTestClient(t *testing.T) *client {
	t.Helper()
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "old\n")
	mustGit(t, path, "branch", "alpha")
	mustGit(t, path, "tag", "alpha")
	writeCommit(t, path, "note.txt", "new\n")
	mustGit(t, path, "branch", "beta")
	mustGit(t, path, "branch", "gamma")
	a := testClient([]Repository{inspect(Entry{Path: path, Name: "app"})})
	a.readRunner = func(job func() func()) { job()() }
	a.writeRunner = func(job func() func()) { job()() }
	a.switchTab(2)
	return a
}
func TestReferenceSelectionAndComparisonUI(t *testing.T) {
	a := referenceTestClient(t)
	tester := ui.NewTester(a.view, 1560, 940)
	head, _ := tester.Find("HEAD (Current Branch)")
	local, _ := tester.Find("Local")
	if head.X != local.X || head.H != local.H {
		t.Fatalf("HEAD row does not share tree alignment: %+v %+v", head, local)
	}
	if err := tester.Click("alpha  (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.RightClick("alpha  (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("与 HEAD 比较"); err != nil {
		t.Fatal(err)
	}
	if a.review.Request.Kind != "direct" || a.review.Request.Base != "refs/heads/alpha" || a.review.Request.Target != "HEAD" || a.review.Error != "" || len(a.review.Items) != 1 {
		t.Fatalf("%+v", a.review)
	}
	if err := tester.RightClick("HEAD (Current Branch)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("与 Worktree 比较"); err != nil {
		t.Fatal(err)
	}
	if a.review.Request.Kind != "workspace" || a.review.Request.Base != "HEAD" || a.review.Request.Target != "" {
		t.Fatalf("%+v", a.review.Request)
	}
	if err := tester.Click("alpha  (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ClickWith(ui.Ctrl, "beta  (1)"); err != nil {
		t.Fatal(err)
	}
	if len(a.refSelection) != 2 {
		t.Fatalf("Ctrl multiselect failed: %+v", a.refSelection)
	}
	if len(a.selectedReferenceSources()) != 1 || len(a.selectedReferenceSources()[0].Refs) != 2 {
		t.Fatal("multi-selection history scope missing")
	}
	if err := tester.RightClick("alpha  (1)"); err != nil {
		t.Fatal(err)
	}
	if len(a.refSelection) != 2 {
		t.Fatal("context menu lost multiselection")
	}
	if err := tester.ChooseMenuItem("比较所选两个引用"); err != nil {
		t.Fatal(err)
	}
	if a.review.Request.Base != "refs/heads/alpha" || a.review.Request.Target != "refs/heads/beta" || len(a.review.Items) != 1 {
		t.Fatalf("%+v", a.review.Request)
	}
	if err := tester.RightClick("beta  (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("反向比较所选两个引用"); err != nil {
		t.Fatal(err)
	}
	if a.review.Request.Base != "refs/heads/beta" || a.review.Request.Target != "refs/heads/alpha" {
		t.Fatal("reverse endpoints wrong")
	}
	if err := tester.ClickWith(ui.Ctrl, "gamma  (1)"); err != nil {
		t.Fatal(err)
	}
	if len(a.refSelection) != 3 {
		t.Fatalf("%+v", a.refSelection)
	}
	if err := tester.RightClick("gamma  (1)"); err != nil {
		t.Fatal(err)
	}
	for _, item := range tester.Menu() {
		if strings.Contains(item, "比较") || strings.Contains(item, "checkout") || strings.Contains(item, "rebase") {
			t.Fatalf("invalid multiselect operation: %+v", item)
		}
	}
	if err := tester.ChooseMenuItem("删除所选引用…"); err != nil {
		t.Fatal(err)
	}
	if !a.refDialog.Open || len(a.refDialog.Choices) != 3 {
		t.Fatal("batch confirmation missing")
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if len(mustGit(t, a.repos[0].Path, "branch", "--list", "alpha", "beta", "gamma")) == 0 {
		t.Fatal("cancel wrote refs")
	}
	// Single Tag shares a name with a branch; its endpoint must stay qualified.
	if err := tester.RightClick("Tag alpha  (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("与指定版本比较…"); err != nil {
		t.Fatal(err)
	}
	if !a.review.Dialog || a.review.Base != "refs/tags/alpha" || a.review.Kind != "direct" {
		t.Fatal("tag comparison is ambiguous")
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("HEAD (Current Branch)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ClickWith(ui.Shift, "beta  (1)"); err != nil {
		t.Fatal(err)
	}
	if len(a.refSelection) != 3 || a.refSelection[0].Kind != "head" {
		t.Fatalf("visible Shift range failed: %+v", a.refSelection)
	}
	if dest := os.Getenv("MYGO_REF_SELECTION_SCREENSHOT"); dest != "" {
		tester.Advance(time.Second)
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		fresh := ui.NewTester(a.view, 1560, 940)
		if err = png.Encode(f, fresh.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
func TestReferenceComparisonsUseRepositoryIntersection(t *testing.T) {
	a := testClient([]Repository{
		{Path: "/one", Name: "one", Branches: []string{"same", "only-one"}, Tags: []string{"same"}},
		{Path: "/two", Name: "two", Branches: []string{"same"}},
	})
	a.refSelection = []ReferenceChoice{{Kind: "local", Name: "same"}, {Kind: "tags", Name: "same"}}
	if repos := a.commonReferenceRepositories(a.refSelection); len(repos) != 1 || repos[0].Path != "/one" {
		t.Fatalf("%+v", repos)
	}
	if refs := a.selectedReferenceSources(); len(refs) != 2 || len(refs[0].Refs) != 2 || refs[0].Refs[0] != "refs/heads/same" || refs[0].Refs[1] != "refs/tags/same" {
		t.Fatalf("%+v", refs)
	}
	a.refSelection = []ReferenceChoice{{Kind: "local", Name: "missing"}, {Kind: "tags", Name: "same"}}
	if len(a.commonReferenceRepositories(a.refSelection)) != 0 {
		t.Fatal("disjoint references compared")
	}
}
func TestReferenceCreateRenameAndBatchDelete(t *testing.T) {
	a := referenceTestClient(t)
	path := a.repos[0].Path
	before := mustGit(t, path, "rev-parse", "HEAD")
	a.runReferenceBatch("create", []ReferenceChoice{{Kind: "tags", Name: "alpha"}}, a.repos, "from-tag")
	if got := mustGit(t, path, "rev-parse", "refs/heads/from-tag"); got != mustGit(t, path, "rev-parse", "refs/tags/alpha") {
		t.Fatal("new branch target wrong")
	}
	if mustGit(t, path, "rev-parse", "HEAD") != before || mustGit(t, path, "branch", "--show-current") != "main" {
		t.Fatal("create switched branch")
	}
	a.refSelection = []ReferenceChoice{{Kind: "local", Name: "from-tag"}}
	a.runReferenceBatch("rename", a.refSelection, a.repos, "main")
	if !a.referenceSelected(ReferenceChoice{Kind: "local", Name: "from-tag"}) || a.referenceSelected(ReferenceChoice{Kind: "local", Name: "main"}) {
		t.Fatal("failed rename changed selection")
	}
	a.runReferenceBatch("rename", a.refSelection, a.repos, "renamed")
	if _, err := git(path, "show-ref", "--verify", "refs/heads/from-tag"); err == nil {
		t.Fatal("rename left old ref")
	}
	if !a.referenceSelected(ReferenceChoice{Kind: "local", Name: "renamed"}) {
		t.Fatal("rename did not update selected ref")
	}
	a.runReferenceBatch("delete", []ReferenceChoice{{Kind: "local", Name: "alpha"}, {Kind: "tags", Name: "alpha"}, {Kind: "local", Name: "renamed"}}, a.repos, "")
	for _, ref := range []string{"refs/heads/alpha", "refs/tags/alpha", "refs/heads/renamed"} {
		if _, err := git(path, "show-ref", "--verify", ref); err == nil {
			t.Fatal("ref not deleted: " + ref)
		}
	}
	if mustGit(t, path, "rev-parse", "HEAD") != before {
		t.Fatal("batch delete moved HEAD")
	}
	if _, err := executeReferenceOperation(a.repos[0], ReferenceChoice{Kind: "head"}, "delete", ""); err == nil {
		t.Fatal("HEAD delete allowed")
	}
	if _, err := executeReferenceOperation(a.repos[0], ReferenceChoice{Kind: "local", Name: "main"}, "delete", ""); err == nil {
		t.Fatal("current branch deleted")
	}
}

func TestReferenceBatchDeleteConfirmationAndPartialFailure(t *testing.T) {
	a := referenceTestClient(t)
	path := a.repos[0].Path
	mustGit(t, path, "switch", "gamma")
	writeCommit(t, path, "unmerged.txt", "keep unmerged\n")
	mustGit(t, path, "switch", "main")
	a.repos = inspectAll(entriesFor(a.repos))
	a.reloadHistory()
	tester := ui.NewTester(a.view, 1560, 940)
	for i, name := range []string{"alpha  (1)", "beta  (1)", "gamma  (1)"} {
		var err error
		if i == 0 {
			err = tester.Click(name)
		} else {
			err = tester.ClickWith(ui.Super, name)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tester.RightClick("beta  (1)"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("删除所选引用…"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("确认批量删除引用"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := git(path, "show-ref", "--verify", "refs/heads/"+name); err == nil {
			t.Fatal("merged branch not deleted: " + name)
		}
	}
	if _, err := git(path, "show-ref", "--verify", "refs/heads/gamma"); err != nil {
		t.Fatal("unmerged branch was removed")
	}
	if !strings.Contains(a.output, "失败") || !strings.Contains(a.output, "gamma") {
		t.Fatal("per-ref error missing: " + a.output)
	}
	if len(a.refSelection) != 1 || a.refSelection[0].Name != "gamma" {
		t.Fatalf("deleted selection not pruned: %+v", a.refSelection)
	}
}

func TestReferenceWorktreeComparisonAndRemoteComparison(t *testing.T) {
	a := referenceTestClient(t)
	path := a.repos[0].Path
	head := mustGit(t, path, "rev-parse", "HEAD")
	mustGit(t, path, "update-ref", "refs/remotes/origin/review", head)
	writeWorkingFile(t, path, "note.txt", "worktree\n")
	writeWorkingFile(t, path, "new.txt", "new\n")
	a.repos = inspectAll(entriesFor(a.repos))
	a.refSelection = []ReferenceChoice{{Kind: "remote", Name: "origin/review"}}
	a.openReferenceComparison("Worktree", false)
	if a.review.Request.Base != "refs/remotes/origin/review" || a.review.Error != "" || len(a.review.Items) != 2 {
		t.Fatalf("%+v", a.review)
	}
	a.refSelection = []ReferenceChoice{{Kind: "tags", Name: "alpha"}, {Kind: "remote", Name: "origin/review"}}
	a.openReferenceComparison("", false)
	if a.review.Request.Base != "refs/tags/alpha" || a.review.Request.Target != "refs/remotes/origin/review" || a.review.Error != "" || len(a.review.Items) != 1 {
		t.Fatalf("%+v", a.review)
	}
}

func TestReferenceDeleteSymbolicTagKeepsTargetBranch(t *testing.T) {
	a := referenceTestClient(t)
	path := a.repos[0].Path
	before := mustGit(t, path, "rev-parse", "refs/heads/main")
	mustGit(t, path, "symbolic-ref", "refs/tags/alias", "refs/heads/main")
	repo := inspect(Entry{Path: path})
	if _, err := executeReferenceOperation(repo, ReferenceChoice{Kind: "tags", Name: "alias"}, "delete", ""); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, path, "rev-parse", "refs/heads/main"); got != before {
		t.Fatal("tag deletion changed target branch")
	}
	if _, err := git(path, "symbolic-ref", "--quiet", "refs/tags/alias"); err == nil {
		t.Fatal("tag alias remains")
	}
}
