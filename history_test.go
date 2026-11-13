package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func headCommit(t *testing.T, path string) Commit {
	t.Helper()
	return Commit{Repository: inspect(Entry{Path: path}), Hash: mustGit(t, path, "rev-parse", "HEAD")}
}
func TestCombinedHistoryKeepsRepositoryAndSelectedRef(t *testing.T) {
	first, second := featureRepo(t), featureRepo(t)
	sources := historySources([]Repository{first, second}, "/feature/order", false)
	commits, err := readHistory(sources)
	if err != "" || len(commits) != 4 {
		t.Fatalf("history: %d %s", len(commits), err)
	}
	paths := map[string]bool{}
	keys := map[string]bool{}
	for i, c := range commits {
		paths[c.Repository.Path] = true
		if keys[c.Key()] {
			t.Fatal("duplicate identity")
		}
		keys[c.Key()] = true
		if i > 0 && commits[i-1].Timestamp < c.Timestamp {
			t.Fatal("not sorted")
		}
	}
	if len(paths) != 2 {
		t.Fatal("repository attribution lost")
	}
	local, _ := readHistory(historySources([]Repository{first}, "", false))
	if len(local) != 1 {
		t.Fatal("current branch history must not include unselected feature")
	}
	remote, _ := readHistory(historySources([]Repository{first}, "/origin/feature/order", true))
	if len(remote) != 2 {
		t.Fatal("remote reference history missing")
	}
}
func TestCommitFileComparison(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file with spaces.txt", "before\n\n")
	writeCommit(t, path, "file with spaces.txt", "after\n\n")
	d, err := readCommit(headCommit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.Files[0].Status != "M" {
		t.Fatalf("files: %+v", d.Files)
	}
	comparison, err := readComparison(d, d.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Before != "before\n\n" || comparison.After != "after\n\n" || !comparison.OldLines[1] || !comparison.NewLines[1] {
		t.Fatalf("comparison: %+v", comparison)
	}
	mustGit(t, path, "mv", "file with spaces.txt", "renamed\nfile.txt")
	mustGit(t, path, "commit", "-m", "rename")
	d, err = readCommit(headCommit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || !strings.HasPrefix(d.Files[0].Status, "R") || d.Files[0].OldPath != "file with spaces.txt" || d.Files[0].Path != "renamed\nfile.txt" {
		t.Fatalf("rename: %+v", d.Files)
	}
	comparison, err = readComparison(d, d.Files[0])
	if err != nil || comparison.Before != comparison.After {
		t.Fatalf("rename comparison: %+v %v", comparison, err)
	}
	mustGit(t, path, "rm", "renamed\nfile.txt")
	mustGit(t, path, "commit", "-m", "delete")
	d, err = readCommit(headCommit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	comparison, err = readComparison(d, d.Files[0])
	if err != nil || comparison.Before != "after\n\n" || comparison.After != "" {
		t.Fatalf("delete: %+v %v", comparison, err)
	}
}
func TestRootBinaryAndLargeFileComparison(t *testing.T) {
	path := testRepo(t)
	mustGit(t, path, "checkout", "--orphan", "root-history")
	writeCommit(t, path, "new.txt", "initial\n")
	d, err := readCommit(headCommit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if d.Parent != "" || len(d.Files) != 1 {
		t.Fatalf("root: %+v", d)
	}
	comparison, err := readComparison(d, d.Files[0])
	if err != nil || comparison.Before != "" || comparison.After != "initial\n" || !comparison.NewLines[1] {
		t.Fatalf("root comparison: %+v %v", comparison, err)
	}
	writeCommit(t, path, "binary.dat", "\x00binary")
	d, err = readCommit(headCommit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	comparison, err = readComparison(d, d.Files[0])
	if err != nil || comparison.Notice == "" {
		t.Fatal("binary must show notice")
	}
	if err := os.WriteFile(filepath.Join(path, "large.txt"), []byte(strings.Repeat("x", blobLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "add", "large.txt")
	mustGit(t, path, "commit", "-m", "large")
	d, err = readCommit(headCommit(t, path))
	if err != nil {
		t.Fatal(err)
	}
	comparison, err = readComparison(d, d.Files[0])
	if err != nil || !strings.Contains(comparison.Notice, "超过") {
		t.Fatal("large file must show limit")
	}
}
func TestStaleHistoryAndDetailResponsesAreIgnored(t *testing.T) {
	first, second := featureRepo(t), featureRepo(t)
	a := &client{repos: []Repository{first, second}, selected: map[string]bool{}, expanded: map[string]*bool{}}
	// Keep this test focused on history/detail response ordering.
	a.repositoryFiles = map[string]repositoryFileState{first.Path: {Loaded: true}, second.Path: {Loaded: true}}
	var queued []func() func()
	a.readRunner = func(work func() func()) { queued = append(queued, work) }
	a.selectRepository(first)
	old := queued[0]()
	a.selectRepository(second)
	queued[1]()()
	old()
	if len(a.commits) != 1 || a.commits[0].Repository.Path != second.Path {
		t.Fatal("stale history replaced latest selection")
	}
	a.selectCommit(a.commits[0])
	oldDetail := queued[2]()
	a.selectRepository(first)
	oldDetail()
	if a.selectedCommit != "" || len(a.files) != 0 {
		t.Fatal("stale detail must be ignored")
	}
}

func TestStaleFileResponseIsIgnoredAfterChangingCommit(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "note.txt", "before\n")
	writeCommit(t, path, "note.txt", "after\n")
	commit := headCommit(t, path)
	detail, err := readCommit(commit)
	if err != nil {
		t.Fatal(err)
	}
	a := testClient([]Repository{commit.Repository})
	a.details = []CommitDetail{detail}
	_, a.files, _ = readSelectedCommits([]Commit{commit})
	a.selectedCommit = commit.Key()
	var queued []func() func()
	a.readRunner = func(work func() func()) { queued = append(queued, work) }
	a.selectFile(a.files[0])
	apply := queued[0]()
	a.selectCommit(commit)
	apply()
	if a.selectedFile != "" || len(a.comparisons) != 0 {
		t.Fatal("stale file content replaced new commit selection")
	}
}
