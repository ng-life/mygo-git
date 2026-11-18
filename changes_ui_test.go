package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func changesTestClient(t *testing.T, path string) *client {
	t.Helper()
	repo := inspect(Entry{Path: path, Name: "app"})
	a := testClient([]Repository{repo})
	a.entries = entriesFor(a.repos)
	a.readRunner = func(w func() func()) { w()() }
	a.writeRunner = func(w func() func()) { w()() }
	a.switchTab(1)
	return a
}
func editChangesText(t *testing.T, tester *ui.Tester, label, text string) {
	t.Helper()
	if err := tester.Click(label); err != nil {
		t.Fatal(err)
	}
	tester.Key(ui.Cmd, ui.KeyA)
	tester.Type(text)
}
func TestChangesSplitRevertSquashAndPushUI(t *testing.T) {
	path, remote := publishedChangesRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeCommit(t, path, "other.txt", "other\n")
	writeWorkingFile(t, path, "file.txt", "dirty\n")
	writeWorkingFile(t, path, "new.txt", "untracked\n")
	a := changesTestClient(t, path)
	tester := ui.NewTester(a.view, 1560, 940)
	upper, _ := tester.Find("Changelists")
	lower, ok := tester.Find("Unpushed Commits")
	if !ok || lower.Y <= upper.Y || lower.Y < 450 {
		t.Fatalf("Changes split missing: %+v %+v", upper, lower)
	}
	if len(a.outgoing) != 1 || len(a.outgoing[0].Commits) != 2 {
		t.Fatal("pending commits missing")
	}
	if dest := os.Getenv("MYGO_CHANGES_SPLIT_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tester.RightClick("file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("Revert This File…"); err != nil {
		t.Fatal(err)
	}
	if len(a.changesDialog.Requests) != 1 || len(a.changesDialog.Requests[0].Files) != 1 {
		t.Fatal("single-file revert scope wrong")
	}
	if err := tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Select repository app"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Revert Selected Files"); err != nil {
		t.Fatal(err)
	}
	if len(a.changesDialog.Requests[0].Files) != 2 || !tester.HasText("Added and untracked files will be deleted") {
		t.Fatal("revert confirmation scope missing")
	}
	if err := tester.Click("Confirm Revert Files"); err != nil {
		t.Fatal(err)
	}
	if mustGit(t, path, "status", "--porcelain") != "" {
		t.Fatal(a.output)
	}
	oldTree := mustGit(t, path, "rev-parse", "HEAD^{tree}")
	if err := tester.Click("Select unpushed repository app"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Squash Unpushed Commits app"); err != nil {
		t.Fatal(err)
	}
	editChangesText(t, tester, "Changes operation message", "squashed from Changes")
	if err := tester.Click("Confirm Squash Unpushed Commits"); err != nil {
		t.Fatal(err)
	}
	if len(a.outgoing[0].Commits) != 1 || a.outgoing[0].Commits[0].Subject != "squashed from Changes" || mustGit(t, path, "rev-parse", "HEAD^{tree}") != oldTree {
		t.Fatal(a.output)
	}
	if err := tester.Click("Push app"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Confirm Push Branch"); err != nil {
		t.Fatal(err)
	}
	if len(a.outgoing[0].Commits) != 0 || mustGit(t, remote, "rev-parse", "refs/heads/main") != mustGit(t, path, "rev-parse", "HEAD") {
		t.Fatal(a.output)
	}
	tester.Frame()
}
func TestChangesCommitPushAndAmendUI(t *testing.T) {
	path, remote := publishedChangesRepo(t)
	writeCommit(t, path, "file.txt", "base\n")
	writeWorkingFile(t, path, "file.txt", "new\n")
	a := changesTestClient(t, path)
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Select file app/file.txt"); err != nil {
		t.Fatal(err)
	}
	editChangesText(t, tester, "Commit message", "commit and push from Changes")
	if err := tester.Click("Commit and push selected repositories"); err != nil {
		t.Fatal(err)
	}
	if len(a.changesDialog.PushTargets) != 1 || a.changesDialog.PushTargets[0].Remote != "origin" {
		t.Fatal("push targets missing")
	}
	if err := tester.Click("Confirm Commit & Push"); err != nil {
		t.Fatal(err)
	}
	if len(a.workingResults) != 1 || a.workingResults[0].Err != nil || mustGit(t, remote, "rev-parse", "refs/heads/main") != mustGit(t, path, "rev-parse", "HEAD") {
		t.Fatal(a.output)
	}
	// Amend a new unpublished commit, including a selected working file.
	writeCommit(t, path, "other.txt", "other\n")
	writeWorkingFile(t, path, "file.txt", "amend content\n")
	a.repos = inspectAll(a.entries)
	a.reloadWorkingTrees()
	tester.Frame()
	beforeParent := mustGit(t, path, "show", "-s", "--format=%P", "HEAD")
	if err := tester.Click("Select file app/file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Amend"); err != nil {
		t.Fatal(err)
	}
	editChangesText(t, tester, "Commit message", "amended selected file")
	if err := tester.Click("Commit selected repositories"); err != nil {
		t.Fatal(err)
	}
	if a.workingResults[0].Err != nil || mustGit(t, path, "show", "HEAD:file.txt") != "amend content" || mustGit(t, path, "show", "-s", "--format=%P", "HEAD") != beforeParent {
		t.Fatal(a.output)
	}
	source := a.outgoing[0]
	head := source.Head
	if err := tester.RightClick("Unpushed commit app/" + shortHash(head)); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("Amend Last Commit…"); err != nil {
		t.Fatal(err)
	}
	editChangesText(t, tester, "Changes operation message", "message-only amend")
	if err := tester.Click("Confirm Amend Last Commit"); err != nil {
		t.Fatal(err)
	}
	if mustGit(t, path, "show", "-s", "--format=%s", "HEAD") != "message-only amend" || mustGit(t, path, "status", "--porcelain") != "" {
		t.Fatal(a.output)
	}
}
func TestCommitPushFailureKeepsVerifiedLocalCommit(t *testing.T) {
	path, remote := publishedChangesRepo(t)
	hooks := filepath.Join(remote, "hooks")
	mustGit(t, remote, "config", "core.hooksPath", hooks)
	if err := os.WriteFile(filepath.Join(hooks, "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, path, "new.txt", "new\n")
	a := changesTestClient(t, path)
	source := a.outgoing[0]
	before := source.Head
	request := requestFor(t, path, "new.txt")
	a.commitMessage = "commit preserved on rejected push"
	a.beginWorkingCommit([]CommitRequest{request}, a.commitMessage, []PushTarget{{Source: source, Remote: "origin", Destination: "main"}})
	if len(a.workingResults) != 1 || a.workingResults[0].Err == nil || !strings.Contains(a.workingResults[0].Err.Error(), "Commit succeeded") {
		t.Fatal(a.output)
	}
	head := mustGit(t, path, "rev-parse", "HEAD")
	if head == before || mustGit(t, path, "status", "--porcelain") != "" || mustGit(t, remote, "rev-parse", "refs/heads/main") != before || len(a.outgoing[0].Commits) != 1 {
		t.Fatal("local commit lost or wrong remote state")
	}
	if err := os.Remove(filepath.Join(hooks, "pre-receive")); err != nil {
		t.Fatal(err)
	}
	if out, err := pushOutgoing(PushTarget{Source: a.outgoing[0], Remote: "origin", Destination: "main"}, head); err != nil {
		t.Fatalf("retry push %s %v", out, err)
	}
	if mustGit(t, path, "rev-parse", "HEAD") != head {
		t.Fatal("push retry created duplicate commit")
	}
}
func TestNewBranchPushConfiguresUpstream(t *testing.T) {
	path := testRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, path, "init", "--bare", remote)
	mustGit(t, path, "remote", "add", "origin", remote)
	source := readOutgoing(context.Background(), inspect(Entry{Path: path}))
	if source.Base != "" {
		t.Fatal(source.Base)
	}
	if out, err := pushOutgoing(PushTarget{Source: source, Remote: "origin", Destination: "main"}, source.Head); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, path, "rev-parse", "--symbolic-full-name", "@{upstream}"); got != "refs/remotes/origin/main" {
		t.Fatal(got)
	}
}
func TestOutgoingCollectionIsVirtualized(t *testing.T) {
	repo := Repository{Path: "/not-a-repository", Name: "large", Branch: "main"}
	source := Outgoing{Repository: repo, Ref: "refs/heads/main", Head: "latest"}
	for i := 0; i < 1000; i++ {
		source.Commits = append(source.Commits, Commit{Repository: repo, Hash: fmt.Sprintf("%040d", i), Subject: fmt.Sprintf("outgoing subject %d", i)})
	}
	a := testClient([]Repository{repo})
	a.tab = 1
	a.outgoing = []Outgoing{source}
	tester := ui.NewTester(a.view, 1560, 940)
	count := 0
	for _, text := range tester.Texts() {
		if strings.HasPrefix(text, "outgoing subject ") {
			count++
		}
	}
	if count > 100 || !tester.HasText("outgoing subject 999") {
		t.Fatalf("built %d rows", count)
	}
}
