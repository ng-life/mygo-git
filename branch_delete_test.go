package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDeleteLocalBranchProtectsCurrentUnmergedAndWorktree(t *testing.T) {
	repo := testRepo(t)
	mustGit(t, repo, "branch", "feature/merged")
	if _, err := runBranchOperation(repo, "delete", "feature/merged", false); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "show-ref", "--verify", "refs/heads/feature/merged"); err == nil {
		t.Fatal("local branch still exists")
	}
	if _, err := runBranchOperation(repo, "delete", "main", false); err == nil {
		t.Fatal("deleted current branch")
	}
	mustGit(t, repo, "switch", "-c", "feature/unmerged")
	writeCommit(t, repo, "new.txt", "unique\n")
	mustGit(t, repo, "switch", "main")
	if _, err := runBranchOperation(repo, "delete", "feature/unmerged", false); err == nil {
		t.Fatal("deleted unmerged branch")
	}
	mustGit(t, repo, "branch", "feature/worktree")
	mustGit(t, repo, "worktree", "add", filepath.Join(t.TempDir(), "linked"), "feature/worktree")
	if _, err := runBranchOperation(repo, "delete", "feature/worktree", false); err == nil {
		t.Fatal("deleted branch used by linked worktree")
	}
}
func remoteDeletionRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := testRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, repo, "init", "--bare", remote)
	mustGit(t, repo, "remote", "add", "origin", remote)
	mustGit(t, repo, "branch", "feature/cleanup")
	mustGit(t, repo, "push", "origin", "main", "feature/cleanup")
	return repo, remote
}
func TestDeleteRemoteBranchLeavesLocalBranchAndOtherRemote(t *testing.T) {
	repo, remote := remoteDeletionRepo(t)
	mustGit(t, repo, "remote", "add", "upstream", remote)
	mustGit(t, repo, "fetch", "upstream")
	out, err := runBranchOperation(repo, "delete", "origin/feature/cleanup", true)
	if err != nil {
		t.Fatal(out, err)
	}
	if refs := mustGit(t, repo, "ls-remote", "--refs", "origin", "refs/heads/feature/cleanup"); refs != "" {
		t.Fatal(refs)
	}
	mustGit(t, repo, "show-ref", "--verify", "refs/heads/feature/cleanup")
	if _, err := git(repo, "show-ref", "--verify", "refs/remotes/origin/feature/cleanup"); err == nil {
		t.Fatal("tracking ref remains")
	}
	mustGit(t, repo, "show-ref", "--verify", "refs/remotes/upstream/feature/cleanup")
}
func TestDeleteRemoteRejectsNewCommitsSinceFetch(t *testing.T) {
	repo, remote := remoteDeletionRepo(t)
	other := testRepo(t)
	mustGit(t, other, "remote", "add", "origin", remote)
	mustGit(t, other, "fetch", "origin")
	mustGit(t, other, "switch", "-c", "feature/cleanup", "--track", "origin/feature/cleanup")
	writeCommit(t, other, "new.txt", "remote advanced\n")
	mustGit(t, other, "push", "origin", "feature/cleanup")
	expected := mustGit(t, other, "rev-parse", "HEAD")
	if out, err := runBranchOperation(repo, "delete", "origin/feature/cleanup", true); err == nil {
		t.Fatal("deleted changed remote", out)
	}
	if got := mustGit(t, repo, "ls-remote", "--refs", "origin", "refs/heads/feature/cleanup"); !strings.HasPrefix(got, expected) {
		t.Fatal(got)
	}
	mustGit(t, repo, "show-ref", "--verify", "refs/remotes/origin/feature/cleanup")
}
func TestDeleteRemoteProtectedByHook(t *testing.T) {
	repo, remote := remoteDeletionRepo(t)
	mustGit(t, remote, "config", "core.hooksPath", filepath.Join(remote, "hooks"))
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runBranchOperation(repo, "delete", "origin/feature/cleanup", true); err == nil {
		t.Fatal("remote hook was bypassed")
	}
	if got := mustGit(t, repo, "ls-remote", "--refs", "origin", "refs/heads/feature/cleanup"); got == "" {
		t.Fatal("protected branch deleted")
	}
}
func TestDeleteBranchBatchContinuesAfterFailure(t *testing.T) {
	first, second := testRepo(t), testRepo(t)
	mustGit(t, first, "switch", "-c", "cleanup")
	mustGit(t, second, "branch", "cleanup")
	repos := []Repository{inspect(Entry{Path: first}), inspect(Entry{Path: second})}
	results := executeBranchBatch(repos, "delete", "cleanup", false)
	if len(results) != 2 || results[0].Err == nil || results[1].Err != nil {
		t.Fatal(results)
	}
	mustGit(t, first, "show-ref", "--verify", "refs/heads/cleanup")
	if _, err := git(second, "show-ref", "--verify", "refs/heads/cleanup"); err == nil {
		t.Fatal("second repository was skipped")
	}
}
func TestDeleteBranchMenuConfirmationAndRefresh(t *testing.T) {
	repo := testRepo(t)
	mustGit(t, repo, "branch", "cleanup")
	a := testClient([]Repository{inspect(Entry{Path: repo, Name: "app"})})
	a.readRunner = func(work func() func()) { work()() }
	a.writeRunner = func(work func() func()) { work()() }
	a.switchTab(2)
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.RightClick("cleanup  (1)"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, label := range tester.Menu() {
		if label == "Delete Local Branch…" {
			found = true
		}
	}
	if !found {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	a.requestBranchDeletion("cleanup", false, a.repos)
	tester.Frame()
	mustGit(t, repo, "show-ref", "--verify", "refs/heads/cleanup")
	if err := tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "show-ref", "--verify", "refs/heads/cleanup")
	a.requestBranchDeletion("cleanup", false, a.repos)
	tester.Frame()
	if err := tester.Click("Confirm Delete Local Branch"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if len(branchTargets(a.repos, "cleanup", false)) != 0 || tester.HasText("cleanup  (1)") {
		t.Fatal("deleted branch remains in navigation")
	}
	if !strings.Contains(a.output, "Succeeded") {
		t.Fatal(a.output)
	}
}

func TestDeleteRemoteUsesTrackingEndpointInsteadOfDifferentPushURL(t *testing.T) {
	repo, remote := remoteDeletionRepo(t)
	fork := filepath.Join(t.TempDir(), "fork.git")
	mustGit(t, repo, "init", "--bare", fork)
	mustGit(t, repo, "push", fork, "feature/cleanup")
	mustGit(t, repo, "remote", "set-url", "--push", "origin", fork)
	if out, err := runBranchOperation(repo, "delete", "origin/feature/cleanup", true); err != nil {
		t.Fatal(out, err)
	}
	if refs := mustGit(t, repo, "ls-remote", "--refs", remote, "refs/heads/feature/cleanup"); refs != "" {
		t.Fatal("tracking endpoint still has branch", refs)
	}
	if refs := mustGit(t, repo, "ls-remote", "--refs", fork, "refs/heads/feature/cleanup"); refs == "" {
		t.Fatal("deleted a different push endpoint")
	}
}

func TestRemoteBranchDeleteMenuConfirmationAndRefresh(t *testing.T) {
	repo, _ := remoteDeletionRepo(t)
	a := testClient([]Repository{inspect(Entry{Path: repo, Name: "app"})})
	a.readRunner = func(work func() func()) { work()() }
	a.writeRunner = func(work func() func()) { work()() }
	a.switchTab(2)
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.RightClick("Remote cleanup  (1)"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, label := range tester.Menu() {
		if label == "Delete Remote Branch…" {
			found = true
		}
	}
	if !found {
		t.Fatal(tester.Menu())
	}
	tester.CloseMenu()
	a.requestBranchDeletion("origin/feature/cleanup", true, a.repos)
	tester.Frame()
	if !tester.HasText("preserving local branches") || !tester.HasText(repo) {
		t.Fatal("confirmation scope missing", tester.Texts())
	}
	if output := os.Getenv("MYGO_DELETE_SCREENSHOT"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, tester.Image()); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	if err := tester.Click("Confirm Delete Remote Branch"); err != nil {
		t.Fatal(err)
	}
	if len(branchTargets(a.repos, "origin/feature/cleanup", true)) != 0 {
		t.Fatal("remote branch still shown")
	}
	mustGit(t, repo, "show-ref", "--verify", "refs/heads/feature/cleanup")
	tester.Frame()
	tester.Frame()
}

func TestCurrentLocalBranchProtection(t *testing.T) {
	targets := []Repository{{Path: "/one", Branch: "main"}, {Path: "/two", Branch: "feature/review"}}
	if !currentLocalBranch("main", targets) || currentLocalBranch("other", targets) || currentLocalBranch("", targets) {
		t.Fatal("grouped current branch detection failed")
	}
	a := testClient(targets)
	a.requestBranchDeletion("main", false, targets)
	if a.branchDeletion.Open {
		t.Fatal("current branch deletion dialog opened")
	}
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "content\n")
	branch := mustGit(t, path, "branch", "--show-current")
	if _, err := deleteBranch(path, branch, false); err == nil {
		t.Fatal("current branch deletion was accepted")
	}
	if got := mustGit(t, path, "branch", "--show-current"); got != branch {
		t.Fatal("current branch changed", got)
	}
}
