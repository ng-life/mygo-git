package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func createTestChangelist(t *testing.T, a *client, name string, activate bool) string {
	t.Helper()
	a.changelistDialog = changelistDialogState{Open: true, Kind: "create", Name: name, Activate: activate}
	a.applyChangelistDialog()
	if a.changelistDialog.Open {
		t.Fatal(a.changelistDialog.Error)
	}
	return a.changelists.Lists[len(a.changelists.Lists)-1].ID
}
func TestChangelistMembershipActiveRenameAndPrune(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "first.txt", "base\n")
	writeWorkingFile(t, path, "first.txt", "changed\n")
	a := testClient([]Repository{inspect(Entry{Path: path})})
	a.workingTrees = []WorkingTree{readWorkingTree(a.repos[0])}
	a.reconcileChangelists()
	original := a.workingTrees[0]
	if a.fileChangelist(original, original.Files[0]) != defaultChangelist {
		t.Fatal("default membership")
	}
	id := createTestChangelist(t, a, "Task B", true)
	a.commitMessage = "message B"
	a.persistChangelists()
	writeWorkingFile(t, path, "second.txt", "second\n")
	a.workingTrees = []WorkingTree{readWorkingTree(a.repos[0])}
	a.reconcileChangelists()
	if len(a.changelistTrees(defaultChangelist)[0].Files) != 1 || len(a.changelistTrees(id)[0].Files) != 1 {
		t.Fatal("activating regrouped old changes")
	}
	requests := changelistRequests(a.changelistTrees(defaultChangelist))
	a.moveToChangelist(requests, id)
	if len(a.changelistTrees(defaultChangelist)) != 0 || len(a.changelistTrees(id)[0].Files) != 2 {
		t.Fatal("move failed")
	}
	a.selectChangelist(defaultChangelist)
	if a.commitMessage != "" {
		t.Fatal("message leaked across lists")
	}
	a.commitMessage = "default draft"
	a.selectChangelist(id)
	if a.commitMessage != "message B" {
		t.Fatal("draft not restored")
	}
	a.changelistDialog = changelistDialogState{Open: true, Kind: "rename", ID: id, Name: "Renamed"}
	a.applyChangelistDialog()
	if a.changelists.list(id).Name != "Renamed" || len(a.changelistTrees(id)[0].Files) != 2 {
		t.Fatal("rename lost changes")
	}
	a.changelistDialog = changelistDialogState{Open: true, Kind: "create", Name: " renamed "}
	a.applyChangelistDialog()
	if !a.changelistDialog.Open || a.changelistDialog.Error == "" {
		t.Fatal("duplicate accepted")
	}
	writeWorkingFile(t, path, "first.txt", "base\n")
	if err := os.Remove(filepath.Join(path, "second.txt")); err != nil {
		t.Fatal(err)
	}
	a.workingTrees = []WorkingTree{readWorkingTree(a.repos[0])}
	a.reconcileChangelists()
	if len(a.changelists.Files) != 0 {
		t.Fatal("cleaned changes retained membership")
	}
	writeWorkingFile(t, path, "first.txt", "changed again\n")
	a.workingTrees = []WorkingTree{readWorkingTree(a.repos[0])}
	a.reconcileChangelists()
	if len(a.changelistTrees(id)) != 1 {
		t.Fatal("new changes not assigned active")
	}
	a.changelistDialog = changelistDialogState{Open: true, Kind: "delete", ID: id}
	a.applyChangelistDialog()
	if a.changelists.Active != defaultChangelist || a.changelists.Selected != defaultChangelist || a.changelists.list(id) != nil || len(a.changelistTrees(defaultChangelist)) != 1 {
		t.Fatal("delete did not return changes")
	}
	if a.commitMessage != "default draft" {
		t.Fatal("default draft lost")
	}
}

func TestChangelistPersistenceAndBranchIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changelists.json")
	a := testClient(nil)
	a.changelistsPath = path
	var jobs []func() func()
	a.readRunner = func(job func() func()) { jobs = append(jobs, job) }
	a.ensureChangelists()
	id := createTestChangelist(t, a, "Feature", true)
	a.commitMessage = "draft\nbody"
	tree := WorkingTree{Repository: Repository{Path: "/repo"}, Ref: "refs/heads/main", Files: []WorkingFile{{Path: "a.txt", Status: " M"}}}
	a.workingTrees = []WorkingTree{tree}
	a.reconcileChangelists()
	a.persistChangelists()
	// Execute old snapshots last: stale writes must not overwrite newer data.
	for i := len(jobs) - 1; i >= 0; i-- {
		jobs[i]()()
	}
	s, err := loadChangelists(path)
	if err != nil || s.Active != id || s.Selected != id || s.list(id).Message != "draft\nbody" || s.Files[changelistFileKey(tree, tree.Files[0])] != id {
		t.Fatalf("%+v %v", s, err)
	}
	a.activateChangelist(defaultChangelist)
	tree.Ref = "refs/heads/other"
	a.workingTrees = []WorkingTree{tree}
	a.reconcileChangelists()
	if a.fileChangelist(tree, tree.Files[0]) != defaultChangelist {
		t.Fatal("branch membership leaked")
	}
	// A transient read error must not discard existing ownership.
	before := len(a.changelists.Files)
	a.workingTrees[0].Error = "read failed"
	a.workingTrees[0].Files = nil
	a.reconcileChangelists()
	if len(a.changelists.Files) != before {
		t.Fatal("read error pruned membership")
	}
}

func TestChangelistRepositoryCommitScope(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "one.txt", "base one\n")
	writeCommit(t, path, "two.txt", "base two\n")
	writeWorkingFile(t, path, "one.txt", "change one\n")
	writeWorkingFile(t, path, "two.txt", "change two\n")
	a := testClient([]Repository{inspect(Entry{Path: path})})
	a.tab = 1
	a.readRunner = func(job func() func()) { job()() }
	a.writeRunner = func(job func() func()) { job()() }
	a.reloadWorkingTrees()
	id := createTestChangelist(t, a, "Second", false)
	tree := a.workingTrees[0]
	a.moveToChangelist([]CommitRequest{{Tree: tree, Files: []WorkingFile{tree.Files[1]}}}, id)
	a.toggleWorkingFiles(path, tree.Files, true)
	a.commitMessage = "first only"
	a.commitChangelistRepository(a.changelistTrees(defaultChangelist)[0], false)
	if len(a.workingResults) != 1 || a.workingResults[0].Err != nil {
		t.Fatalf("%+v", a.workingResults)
	}
	if got := mustGit(t, path, "show", "HEAD:two.txt"); got != "base two" {
		t.Fatal("committed another list")
	}
	if len(a.changelistTrees(id)) != 1 {
		t.Fatal("remaining list lost")
	}
	if got := mustGit(t, path, "show", "HEAD:one.txt"); got != "change one" {
		t.Fatal("selected list not committed")
	}
}

func TestChangelistManagementUI(t *testing.T) {
	a := workingClient(t)
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("New Changelist"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Changelist name"); err != nil {
		t.Fatal(err)
	}
	tester.Type("Task B")
	if err := tester.Click("Confirm New Changelist"); err != nil {
		t.Fatal(err)
	}
	if a.changelists.list(a.changelists.Active).Name != "Task B" {
		t.Fatal("active not set")
	}
	if err := tester.RightClick("order.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("Move to Changelist:Task B"); err != nil {
		t.Fatal(err)
	}
	if len(a.changelistTrees(a.changelists.Active)) != 1 {
		t.Fatal("context move failed")
	}
	if err := tester.Click("Changelist Task B"); err != nil {
		t.Fatal(err)
	}
	if len(a.workingRequests("")) != 1 {
		t.Fatal("select list should select its changes")
	}
	if err := tester.Click("Commit message"); err != nil {
		t.Fatal(err)
	}
	tester.Type("task B draft")
	if err := tester.Click("Changelist Default"); err != nil {
		t.Fatal(err)
	}
	if a.commitMessage != "" || len(a.workingRequests("")) != 1 {
		t.Fatal("wrong list selection/draft")
	}
	if err := tester.Click("Changelist Task B"); err != nil {
		t.Fatal(err)
	}
	if a.commitMessage != "task B draft" {
		t.Fatal("draft lost")
	}
	if dest := os.Getenv("MYGO_CHANGELIST_SCREENSHOT"); dest != "" {
		tester.Advance(time.Second)
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := tester.RightClick("Changelist Task B"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("Delete Changelist…"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Confirm Delete Changelist"); err != nil {
		t.Fatal(err)
	}
	if a.changelists.Active != defaultChangelist || len(a.changelists.Lists) != 1 {
		t.Fatal("delete failed")
	}
	tester.Frame()
	tester.Frame()
	_ = tester.Image()
}

func TestChangelistInvalidConfigIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changelists.json")
	original := []byte("{invalid")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := loadChangelists(path)
	if err == nil {
		t.Fatal("invalid config accepted")
	}
	a := testClient(nil)
	a.changelists = s
	a.changelistsPath = path
	a.changelistsLoadError = err.Error()
	createTestChangelist(t, a, "Session only", true)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatal("invalid config overwritten")
	}
}

func TestChangelistRenamedFileKeepsMembership(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "before.txt", "base\n")
	writeWorkingFile(t, path, "before.txt", "changed\n")
	a := testClient(nil)
	a.workingTrees = []WorkingTree{readWorkingTree(Repository{Path: path})}
	a.reconcileChangelists()
	id := createTestChangelist(t, a, "Rename task", false)
	a.moveToChangelist(changelistRequests(a.workingTrees), id)
	mustGit(t, path, "mv", "before.txt", "after.txt")
	a.workingTrees = []WorkingTree{readWorkingTree(Repository{Path: path})}
	a.reconcileChangelists()
	if len(a.workingTrees[0].Files) != 1 || a.fileChangelist(a.workingTrees[0], a.workingTrees[0].Files[0]) != id {
		t.Fatalf("%+v", a.workingTrees)
	}
	if len(a.changelists.Files) != 1 {
		t.Fatal("old rename membership retained")
	}
}
