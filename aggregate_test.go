package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSelectedCommitFileUnionExcludesUnselectedChanges(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "shared.txt", "v1\n")
	writeCommit(t, path, "shared.txt", "v2\n")
	first := headCommit(t, path)
	writeCommit(t, path, "unselected.txt", "skip\n")
	gap := headCommit(t, path)
	writeCommit(t, path, "shared.txt", "v3\n")
	last := headCommit(t, path)
	details, files, err := readSelectedCommits([]Commit{last, first})
	if err != "" || len(details) != 2 || len(files) != 1 || files[0].Path != "shared.txt" || len(files[0].Changes) != 2 {
		t.Fatalf("union: %+v %s", files, err)
	}
	comparisons := readMergedComparison(files[0])
	if len(comparisons) != 2 || comparisons[0].Error != "" || comparisons[0].Comparison.Before != "v2\n" || comparisons[0].Comparison.After != "v3\n" || comparisons[1].Comparison.Before != "v1\n" {
		t.Fatalf("comparison union: %+v", comparisons)
	}
	a := testClient([]Repository{last.Repository})
	a.readRunner = func(work func() func()) { work()() }
	a.commits = []Commit{last, gap, first}
	a.clickCommit(0, 0)
	a.clickCommit(2, ui.Ctrl)
	if len(a.activeCommits) != 2 || len(a.files) != 1 {
		t.Fatal("Control must select non-adjacent commits")
	}
	a.clickCommit(2, ui.Ctrl)
	if len(a.activeCommits) != 1 {
		t.Fatal("Control must toggle selected commit off")
	}
	a.clickCommit(2, ui.Ctrl)
	a.clickCommit(0, ui.Shift)
	if len(a.activeCommits) != 3 || len(a.files) != 2 {
		t.Fatal("Shift must select continuous range")
	}
	a.clickCommit(0, 0)
	a.clickCommit(0, ui.Ctrl)
	if len(a.activeCommits) != 0 || len(a.files) != 0 || a.selectedFile != "" {
		t.Fatal("toggling the final commit must clear dependent views")
	}
}
func TestSamePathAcrossRepositoriesRemainsSeparate(t *testing.T) {
	first, second := testRepo(t), testRepo(t)
	writeCommit(t, first, "same.txt", "first\n")
	writeCommit(t, second, "same.txt", "second\n")
	_, files, err := readSelectedCommits([]Commit{headCommit(t, first), headCommit(t, second)})
	if err != "" || len(files) != 2 || files[0].Key() == files[1].Key() {
		t.Fatal("same path in different repositories must remain distinct")
	}
	for _, file := range files {
		result := readMergedComparison(file)
		if len(result) != 1 || result[0].Change.Detail.Commit.Repository.Path != file.Repository.Path {
			t.Fatal("comparison repository attribution lost")
		}
	}
}
