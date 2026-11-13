package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func publishedChangesRepo(t *testing.T) (string, string) {
	t.Helper()
	path := testRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, path, "init", "--bare", remote)
	mustGit(t, path, "remote", "add", "origin", remote)
	mustGit(t, path, "push", "-u", "origin", "main")
	return path, remote
}
func TestAmendSelectedFilesPreservesOtherStagedChanges(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "selected.txt", "old\n")
	writeCommit(t, path, "other.txt", "old other\n")
	before := mustGit(t, path, "rev-parse", "HEAD")
	parents := mustGit(t, path, "show", "-s", "--format=%P", "HEAD")
	writeWorkingFile(t, path, "selected.txt", "new\n")
	writeWorkingFile(t, path, "other.txt", "staged\n")
	mustGit(t, path, "add", "other.txt")
	request := requestFor(t, path, "selected.txt")
	request.Amend = true
	out, err := commitWorkingTree(request, "amended message")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if mustGit(t, path, "rev-parse", "HEAD") == before || mustGit(t, path, "show", "-s", "--format=%P", "HEAD") != parents {
		t.Fatal("amend ancestry wrong")
	}
	if mustGit(t, path, "show", "HEAD:selected.txt") != "new" || mustGit(t, path, "show", "HEAD:other.txt") != "old other" || mustGit(t, path, "show", ":other.txt") != "staged" {
		t.Fatal("amend mixed unrelated staged changes")
	}
	if verifiedCommitHash(out) != mustGit(t, path, "rev-parse", "HEAD") {
		t.Fatal("verified amend SHA unavailable")
	}
	tree := readWorkingTree(inspect(Entry{Path: path}))
	out, err = commitWorkingTree(CommitRequest{Tree: tree, Amend: true}, "message only")
	if err != nil || mustGit(t, path, "show", "HEAD:other.txt") != "old other" || mustGit(t, path, "show", ":other.txt") != "staged" {
		t.Fatalf("message-only amend %s %v", out, err)
	}
	mustGit(t, path, "update-ref", "refs/remotes/origin/main", "HEAD")
	tree = readWorkingTree(inspect(Entry{Path: path}))
	head := tree.Head
	if _, err = commitWorkingTree(CommitRequest{Tree: tree, Amend: true}, "must refuse"); err == nil || mustGit(t, path, "rev-parse", "HEAD") != head {
		t.Fatal("published HEAD amended")
	}
}
func TestRevertSelectedPathsPreservesUnselectedIndexAndHandlesRename(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "selected.txt", "old\n")
	writeCommit(t, path, "other.txt", "old other\n")
	writeCommit(t, path, "old-name.txt", "rename\n")
	writeCommit(t, path, "deleted.txt", "restore\n")
	writeWorkingFile(t, path, "selected.txt", "new\n")
	mustGit(t, path, "add", "selected.txt")
	writeWorkingFile(t, path, "selected.txt", "newer\n")
	writeWorkingFile(t, path, "other.txt", "unrelated staged\n")
	mustGit(t, path, "add", "other.txt")
	writeWorkingFile(t, path, "new.txt", "new\n")
	writeWorkingFile(t, path, "staged-new.txt", "staged new\n")
	mustGit(t, path, "add", "staged-new.txt")
	mustGit(t, path, "mv", "old-name.txt", "new-name.txt")
	if err := os.Remove(filepath.Join(path, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	request := requestFor(t, path, "selected.txt", "new.txt", "staged-new.txt", "new-name.txt", "deleted.txt")
	if out, err := revertWorkingFiles(request); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got, err := os.ReadFile(filepath.Join(path, "selected.txt")); err != nil || string(got) != "old\n" {
		t.Fatal("selected file not restored")
	}
	for _, file := range []string{"new.txt", "staged-new.txt", "new-name.txt"} {
		if _, err := os.Lstat(filepath.Join(path, file)); !os.IsNotExist(err) {
			t.Fatal("new file not removed", file)
		}
	}
	for _, file := range []string{"old-name.txt", "deleted.txt"} {
		if _, err := os.Stat(filepath.Join(path, file)); err != nil {
			t.Fatal("original not restored", file)
		}
	}
	if got := mustGit(t, path, "show", ":other.txt"); got != "unrelated staged" {
		t.Fatal("unrelated index changed")
	}
	if got := mustGit(t, path, "status", "--porcelain"); !strings.Contains(got, "other.txt") || strings.Count(got, "\n") != 0 {
		t.Fatal(got)
	}
}
func TestRevertRejectsNewEditsAndPreservesDirectories(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "old\n")
	writeWorkingFile(t, path, "file.txt", "new\n")
	request := requestFor(t, path, "file.txt")
	writeWorkingFile(t, path, "file.txt", "newer\n")
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(path, "file.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := revertWorkingFiles(request); err == nil {
		t.Fatal("new edit discarded")
	}
	if err := os.Remove(filepath.Join(path, "file.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "file.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, path, "file.txt/child.txt", "keep\n")
	request = requestFor(t, path, "file.txt")
	if _, err := revertWorkingFiles(request); err == nil {
		t.Fatal("directory replacement accepted")
	}
	if _, err := os.Stat(filepath.Join(path, "file.txt", "child.txt")); err != nil {
		t.Fatal("unselected child lost")
	}
}
func TestOutgoingPushAndStaleHeadProtection(t *testing.T) {
	path, remote := publishedChangesRepo(t)
	writeCommit(t, path, "one.txt", "one\n")
	writeCommit(t, path, "two.txt", "two\n")
	source := readOutgoing(context.Background(), inspect(Entry{Path: path, Name: "app"}))
	if source.Error != "" || len(source.Commits) != 2 || source.Remote != "origin" || source.Destination != "refs/heads/main" {
		t.Fatalf("%+v", source)
	}
	target := PushTarget{Source: source, Remote: "origin", Destination: "main"}
	out, err := pushOutgoing(target, source.Head)
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if mustGit(t, remote, "rev-parse", "refs/heads/main") != source.Head {
		t.Fatal("remote tip mismatch")
	}
	if fresh := readOutgoing(context.Background(), source.Repository); len(fresh.Commits) != 0 {
		t.Fatalf("pushed commits remain: %+v", fresh)
	}
	writeCommit(t, path, "three.txt", "three\n")
	if _, err := pushOutgoing(target, source.Head); err == nil {
		t.Fatal("stale push accepted")
	}
}
func TestSquashContiguousCommitsPreservesTipTreeAndDescendants(t *testing.T) {
	path, _ := publishedChangesRepo(t)
	writeCommit(t, path, "one.txt", "one\n")
	writeCommit(t, path, "two.txt", "two\n")
	writeCommit(t, path, "three.txt", "three\n")
	source := readOutgoing(context.Background(), inspect(Entry{Path: path}))
	oldTree := mustGit(t, path, "rev-parse", "HEAD^{tree}")
	oldHead := source.Head
	out, err := squashOutgoing(source, source.Commits[:2], "combined one and two")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if mustGit(t, path, "rev-parse", "HEAD^{tree}") != oldTree || mustGit(t, path, "status", "--porcelain") != "" {
		t.Fatal("squash changed working contents")
	}
	if mustGit(t, path, "rev-parse", "main@{1}") != oldHead {
		t.Fatal("old tip missing from reflog")
	}
	fresh := readOutgoing(context.Background(), source.Repository)
	if len(fresh.Commits) != 2 || fresh.Commits[0].Subject != "combined one and two" || fresh.Commits[1].Subject != "change three.txt" {
		t.Fatalf("%+v", fresh.Commits)
	}
	if _, err := squashOutgoing(source, source.Commits[:2], "stale"); err == nil {
		t.Fatal("stale squash accepted")
	}
}
func TestSquashNonContiguousAndConflictLeaveOriginalHistory(t *testing.T) {
	path, _ := publishedChangesRepo(t)
	for _, file := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		writeCommit(t, path, file, file+"\n")
	}
	source := readOutgoing(context.Background(), inspect(Entry{Path: path}))
	oldTree := mustGit(t, path, "rev-parse", "HEAD^{tree}")
	out, err := squashOutgoing(source, []Commit{source.Commits[0], source.Commits[2]}, "A plus C")
	if err != nil {
		t.Fatalf("non-contiguous %s %v", out, err)
	}
	fresh := readOutgoing(context.Background(), source.Repository)
	if len(fresh.Commits) != 3 || fresh.Commits[0].Subject != "A plus C" || fresh.Commits[1].Subject != "change b.txt" || mustGit(t, path, "rev-parse", "HEAD^{tree}") != oldTree {
		t.Fatalf("%+v", fresh.Commits)
	}
	other, _ := publishedChangesRepo(t)
	writeCommit(t, other, "file.txt", "one\n")
	writeCommit(t, other, "file.txt", "two\n")
	writeCommit(t, other, "file.txt", "three\n")
	conflicting := readOutgoing(context.Background(), inspect(Entry{Path: other}))
	oldHead := conflicting.Head
	if _, err := squashOutgoing(conflicting, []Commit{conflicting.Commits[0], conflicting.Commits[2]}, "conflict"); err == nil {
		t.Fatal("expected reordering conflict")
	}
	if mustGit(t, other, "rev-parse", "HEAD") != oldHead || mustGit(t, other, "status", "--porcelain") != "" || mustGit(t, other, "show", "HEAD:file.txt") != "three" {
		t.Fatal("failed squash changed original worktree or history")
	}
}
func TestSquashHookRejectionAndDirtyWorktree(t *testing.T) {
	path, _ := publishedChangesRepo(t)
	writeCommit(t, path, "one.txt", "one\n")
	writeCommit(t, path, "two.txt", "two\n")
	source := readOutgoing(context.Background(), inspect(Entry{Path: path}))
	writeWorkingFile(t, path, "dirty.txt", "keep\n")
	if _, err := squashOutgoing(source, source.Commits, "combined"); err == nil {
		t.Fatal("dirty squash accepted")
	}
	if err := os.Remove(filepath.Join(path, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "commit-msg"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "config", "core.hooksPath", hooks)
	if _, err := squashOutgoing(source, source.Commits, "combined"); err == nil {
		t.Fatal("commit-msg rejection ignored")
	}
	if mustGit(t, path, "rev-parse", "HEAD") != source.Head || mustGit(t, path, "status", "--porcelain") != "" {
		t.Fatal("hook failure changed branch")
	}
}

func TestSquashIncludesInitialCommitWithoutCreatingAParent(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "one.txt", "one\n")
	writeCommit(t, path, "two.txt", "two\n")
	source := readOutgoing(context.Background(), inspect(Entry{Path: path}))
	tree := mustGit(t, path, "rev-parse", "HEAD^{tree}")
	if out, err := squashOutgoing(source, source.Commits, "one root commit"); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if mustGit(t, path, "rev-list", "--count", "HEAD") != "1" || mustGit(t, path, "show", "-s", "--format=%P", "HEAD") != "" || mustGit(t, path, "rev-parse", "HEAD^{tree}") != tree {
		t.Fatal("root squash ancestry or tree mismatch")
	}
}
func TestRevertInitialStagedAndUntrackedFiles(t *testing.T) {
	path := t.TempDir()
	mustGit(t, path, "init", "-b", "main")
	writeWorkingFile(t, path, "staged.txt", "staged\n")
	writeWorkingFile(t, path, "untracked.txt", "untracked\n")
	mustGit(t, path, "add", "staged.txt")
	request := requestFor(t, path, "staged.txt", "untracked.txt")
	if out, err := revertWorkingFiles(request); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if mustGit(t, path, "status", "--porcelain") != "" {
		t.Fatal("unborn revert left selected files")
	}
}
func TestAmendHookFailureKeepsMessageAndSelection(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "old\n")
	writeWorkingFile(t, path, "file.txt", "new\n")
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "config", "core.hooksPath", hooks)
	a := changesTestClient(t, path)
	a.workingAmend = true
	a.commitMessage = "retry amend"
	a.toggleWorkingFiles(path, a.workingTrees[0].Files, true)
	head := mustGit(t, path, "rev-parse", "HEAD")
	a.commitWorking("")
	if len(a.workingResults) != 1 || a.workingResults[0].Err == nil || a.commitMessage != "retry amend" || len(a.workingRequests("")) != 1 || mustGit(t, path, "rev-parse", "HEAD") != head {
		t.Fatal("failed amend lost retry state", a.output)
	}
	a.openChangesAmend(a.outgoing[0])
	a.changesDialog.Message = "message-only retry draft"
	a.applyChangesDialog()
	if !a.changesDialog.Open || a.changesDialog.Message != "message-only retry draft" || a.changesDialog.Error == "" {
		t.Fatal("message-only amend retry draft lost")
	}

}

func TestRevertRenamePreservesRecreatedIgnoredOldPath(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "old.txt", "original\n")
	mustGit(t, path, "mv", "old.txt", "new.txt")
	writeWorkingFile(t, path, ".gitignore", "old.txt\n")
	writeWorkingFile(t, path, "old.txt", "new ignored content\n")
	request := requestFor(t, path, "new.txt")
	if _, err := revertWorkingFiles(request); err == nil {
		t.Fatal("recreated ignored old path overwritten")
	}
	if data, err := os.ReadFile(filepath.Join(path, "old.txt")); err != nil || string(data) != "new ignored content\n" {
		t.Fatal("ignored file lost")
	}
}
