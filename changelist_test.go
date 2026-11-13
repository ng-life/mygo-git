package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChangelistHidesIndexOnlyChanges(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "base\n")
	writeWorkingFile(t, path, "note.txt", "staged\n")
	writeWorkingFile(t, path, "ghost.txt", "staged addition\n")
	mustGit(t, path, "add", ".")
	writeWorkingFile(t, path, "note.txt", "base\n")
	if err := os.Remove(filepath.Join(path, "ghost.txt")); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, path, "write-tree")
	tree := readWorkingTree(Repository{Path: path})
	if tree.Error != "" || len(tree.Files) != 0 {
		t.Fatalf("%+v", tree)
	}
	if repo := inspect(Entry{Path: path}); repo.Status != "" || repo.Error != "" {
		t.Fatalf("%+v", repo)
	}
	if got := mustGit(t, path, "write-tree"); got != before {
		t.Fatal("index modified")
	}
	writeWorkingFile(t, path, "new.txt", "new\n")
	request := requestFor(t, path, "new.txt")
	if _, err := commitWorkingTree(request, "selected changelist"); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, path, "show", ":ghost.txt"); got != "staged addition" {
		t.Fatal("hidden staged addition lost")
	}
}

func TestChangelistRecreatedDeletion(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "base\n")
	mustGit(t, path, "rm", "note.txt")
	writeWorkingFile(t, path, "note.txt", "base\n")
	if tree := readWorkingTree(Repository{Path: path}); tree.Error != "" || len(tree.Files) != 0 {
		t.Fatalf("%+v", tree)
	}
	writeWorkingFile(t, path, "note.txt", "changed\n")
	tree := readWorkingTree(Repository{Path: path})
	if tree.Error != "" || len(tree.Files) != 1 || workingChangeKind(tree.Files[0]) != "modified" {
		t.Fatalf("%+v", tree)
	}
	if _, err := revertWorkingFiles(CommitRequest{Tree: tree, Files: tree.Files}); err != nil {
		t.Fatal(err)
	}
}

func TestChangelistBeforeInitialCommit(t *testing.T) {
	path := testRepo(t)
	writeWorkingFile(t, path, "ghost.txt", "staged\n")
	mustGit(t, path, "add", "ghost.txt")
	if err := os.Remove(filepath.Join(path, "ghost.txt")); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, path, "new.txt", "new\n")
	tree := readWorkingTree(Repository{Path: path})
	if tree.Error != "" || len(tree.Files) != 1 || tree.Files[0].Path != "new.txt" {
		t.Fatalf("%+v", tree)
	}
}
