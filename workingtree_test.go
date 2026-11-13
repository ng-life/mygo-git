package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkingFile(t *testing.T, path, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, file), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func requestFor(t *testing.T, path string, files ...string) CommitRequest {
	t.Helper()
	tree := readWorkingTree(inspect(Entry{Path: path}))
	if tree.Error != "" {
		t.Fatal(tree.Error)
	}
	request := CommitRequest{Tree: tree}
	for _, path := range files {
		found := false
		for _, file := range tree.Files {
			if file.Path == path {
				request.Files = append(request.Files, file)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("file %q not in working tree: %+v", path, tree.Files)
		}
	}
	return request
}
func TestWorkingCommitPreservesUnselectedStagedFiles(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "selected.txt", "old selected\n")
	writeCommit(t, path, "other.txt", "old other\n")
	writeWorkingFile(t, path, "selected.txt", "new selected\n")
	writeWorkingFile(t, path, "other.txt", "staged other\n")
	mustGit(t, path, "add", "other.txt")
	writeWorkingFile(t, path, "other.txt", "unstaged other\n")
	request := requestFor(t, path, "selected.txt")
	out, err := commitWorkingTree(request, "SCM-N12345 shared message\n\n# keep body\nquotes ' and $ symbols\n")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, path, "show", "HEAD:selected.txt"); got != "new selected" {
		t.Fatal(got)
	}
	if got := mustGit(t, path, "show", "HEAD:other.txt"); got != "old other" {
		t.Fatal("unselected file committed")
	}
	if got := mustGit(t, path, "show", ":other.txt"); got != "staged other" {
		t.Fatal("unselected index entry changed")
	}
	content, err := os.ReadFile(filepath.Join(path, "other.txt"))
	if err != nil || string(content) != "unstaged other\n" {
		t.Fatal("unselected working file changed")
	}
	message := mustGit(t, path, "log", "-1", "--format=%B")
	if !strings.Contains(message, "# keep body") {
		t.Fatal(message)
	}
}
func TestWorkingCommitLiteralNewRenameAndDelete(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "old name.txt", "rename\n")
	writeCommit(t, path, "deleted.txt", "delete\n")
	mustGit(t, path, "mv", "old name.txt", "renamed\nfile.txt")
	if err := os.Remove(filepath.Join(path, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, path, "literal[1]*.txt", "literal\n")
	writeWorkingFile(t, path, "literal1x.txt", "unselected\n")
	request := requestFor(t, path, "renamed\nfile.txt", "deleted.txt", "literal[1]*.txt")
	if request.Files[0].OldPath != "old name.txt" {
		t.Fatalf("rename parsing: %+v", request.Files[0])
	}
	if out, err := commitWorkingTree(request, "rename and delete"); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, path, "show", "HEAD:literal[1]*.txt"); got != "literal" {
		t.Fatal(got)
	}
	if _, err := git(path, "cat-file", "-e", "HEAD:literal1x.txt"); err == nil {
		t.Fatal("literal pathspec matched unrelated file")
	}
	if _, err := git(path, "cat-file", "-e", "HEAD:deleted.txt"); err == nil {
		t.Fatal("deletion not committed")
	}
	if got := mustGit(t, path, "show", "HEAD:renamed\nfile.txt"); got != "rename" {
		t.Fatal(got)
	}
}
func TestWorkingBatchMessageAndPartialFailure(t *testing.T) {
	first, second := testRepo(t), testRepo(t)
	writeWorkingFile(t, first, "new.txt", "first\n")
	writeWorkingFile(t, second, "new.txt", "second\n")
	requests := []CommitRequest{requestFor(t, first, "new.txt"), requestFor(t, second, "new.txt")}
	hooks := filepath.Join(first, "hooks")
	if err := os.Mkdir(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "commit-msg"), []byte("#!/bin/sh\necho 'message rejected' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, first, "config", "core.hooksPath", hooks)
	message := "SCM-N12345 shared\n\nexact body\n# same message\n"
	results := executeWorkingCommits(requests, message)
	if len(results) != 2 || results[0].Err == nil || results[1].Err != nil {
		t.Fatalf("results: %+v", results)
	}
	if got := mustGit(t, second, "log", "-1", "--format=%B"); got != strings.TrimSpace(message) {
		t.Fatalf("message: %q", got)
	}
	if got := mustGit(t, first, "show", ":new.txt"); got != "first" {
		t.Fatal("hook rejection must preserve staged file")
	}
	mustGit(t, first, "config", "core.hooksPath", filepath.Join(first, "no-hooks"))
	// Retrying uses fresh HEAD/state while preserving the exact shared message.
	if out, err := commitWorkingTree(requestFor(t, first, "new.txt"), message); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, first, "log", "-1", "--format=%B"); got != mustGit(t, second, "log", "-1", "--format=%B") {
		t.Fatal("repositories must use identical commit messages")
	}
}
func TestWorkingCommitRejectsStaleHeadAndEmptyMessage(t *testing.T) {
	path := testRepo(t)
	writeWorkingFile(t, path, "new.txt", "new\n")
	request := requestFor(t, path, "new.txt")
	if _, err := commitWorkingTree(request, " \n"); err == nil {
		t.Fatal("empty message accepted")
	}
	mustGit(t, path, "switch", "-c", "other")
	if _, err := commitWorkingTree(request, "message"); err == nil {
		t.Fatal("changed branch accepted")
	}
	if got := mustGit(t, path, "diff", "--cached", "--name-only"); got != "" {
		t.Fatal("stale request must not stage files")
	}
}
func TestInitialWorkingCommitWithUnselectedStagedFile(t *testing.T) {
	path := t.TempDir()
	mustGit(t, path, "init", "-b", "main")
	mustGit(t, path, "config", "core.hooksPath", filepath.Join(path, "no-hooks"))
	mustGit(t, path, "config", "user.name", "Test")
	mustGit(t, path, "config", "user.email", "test@example.com")
	mustGit(t, path, "config", "commit.gpgsign", "false")
	writeWorkingFile(t, path, "first.txt", "first\n")
	writeWorkingFile(t, path, "other.txt", "other\n")
	mustGit(t, path, "add", "other.txt")
	request := requestFor(t, path, "first.txt")
	if request.Tree.Head != "" {
		t.Fatal("initial HEAD must be empty")
	}
	if out, err := commitWorkingTree(request, "initial selected"); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, path, "ls-tree", "--name-only", "HEAD"); got != "first.txt" {
		t.Fatal("initial commit included unselected file: " + got)
	}
	if got := mustGit(t, path, "show", ":other.txt"); got != "other" {
		t.Fatal("unselected initial index entry was lost")
	}
}
func TestWorkingComparisonIncludesStagedAndUnstaged(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "base\n")
	writeWorkingFile(t, path, "note.txt", "staged\n")
	mustGit(t, path, "add", "note.txt")
	writeWorkingFile(t, path, "note.txt", "working\n")
	request := requestFor(t, path, "note.txt")
	if request.Files[0].Status != "MM" {
		t.Fatalf("porcelain spacing: %+v", request.Files[0])
	}
	comparison, err := readWorkingComparison(request.Tree, request.Files[0])
	if err != nil || comparison.Before != "base\n" || comparison.After != "working\n" {
		t.Fatalf("comparison: %+v %v", comparison, err)
	}
}

func TestRecreatedStagedDeletionIsOneSelectableFile(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "same.txt", "old\n")
	mustGit(t, path, "rm", "same.txt")
	writeWorkingFile(t, path, "same.txt", "replacement\n")
	request := requestFor(t, path, "same.txt")
	if len(request.Tree.Files) != 1 || request.Files[0].Status != "D?" {
		t.Fatalf("recreated deletion must have one row: %+v", request.Tree.Files)
	}
	if out, err := commitWorkingTree(request, "replace"); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, path, "show", "HEAD:same.txt"); got != "replacement" {
		t.Fatal(got)
	}
}
func TestRenameCannotCommitUnselectedRecreatedSource(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "old.txt", "old\n")
	mustGit(t, path, "mv", "old.txt", "new.txt")
	writeWorkingFile(t, path, "old.txt", "unselected replacement\n")
	request := requestFor(t, path, "new.txt")
	before := mustGit(t, path, "rev-parse", "HEAD")
	if _, err := commitWorkingTree(request, "rename only"); err == nil {
		t.Fatal("recreated source must not be committed implicitly")
	}
	if got := mustGit(t, path, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD changed despite ambiguous selection")
	}
	request = requestFor(t, path, "new.txt", "old.txt")
	if out, err := commitWorkingTree(request, "both explicitly selected"); err != nil {
		t.Fatalf("%s %v", out, err)
	}
}
