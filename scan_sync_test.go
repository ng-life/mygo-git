package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestScanReconcileScopeDepthMetadataAndIncompleteScan(t *testing.T) {
	root := scanCanonicalPath(t.TempDir())
	outside := scanCanonicalPath(t.TempDir())
	one := filepath.Join(root, "one")
	three := filepath.Join(root, "a", "b", "three")
	four := filepath.Join(root, "deep", "a", "b", "four")
	for _, path := range []string{one, three, four, filepath.Join(one, "nested")} {
		markScanRepository(t, path)
	}
	existing := []Entry{{Path: one, Name: "custom/name"}, {Path: four}, {Path: outside}, {Path: filepath.Join(one, "nested")}}
	found, err := discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	entries, added, removed := reconcileScanEntries(existing, found, []string{root}, false)
	if len(entries) != 2 || added != 1 || removed != 3 {
		t.Fatalf("%+v added=%d removed=%d", entries, added, removed)
	}
	for _, entry := range entries {
		if entry.Path == one && entry.Name != "custom/name" {
			t.Fatal("repository name was lost")
		}
	}
	// An unavailable directory must not make an in-scope repository disappear.
	missing := filepath.Join(root, "offline", "repo")
	existing = append(existing, Entry{Path: missing})
	entries, _, removed = reconcileScanEntries(existing, nil, []string{root}, true)
	if len(entries) != 2 || removed != 3 {
		t.Fatalf("incomplete scan: %+v removed=%d", entries, removed)
	}
	for _, entry := range entries {
		if entry.Path != one && entry.Path != missing {
			t.Fatal("retained out-of-scope repository", entry)
		}
	}
	entries, _, removed = reconcileScanEntries(existing, nil, nil, true)
	if len(entries) != 0 || removed != len(existing) {
		t.Fatal("empty roots must remove all entries")
	}
	if _, err := os.Stat(filepath.Join(one, ".git")); err != nil {
		t.Fatal("repository on disk was touched", err)
	}
}

func TestSettingsApplyPrunesRemovedRepositoryState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	first, second := scanCanonicalPath(testRepo(t)), scanCanonicalPath(testRepo(t))
	writeCommit(t, first, "one.txt", "one\n")
	writeCommit(t, second, "two.txt", "two\n")
	a := testClient(inspectAll([]Entry{{Path: first}, {Path: second}}))
	a.entries = []Entry{{Path: first}, {Path: second}}
	a.scanFolders = []string{first, second}
	a.scanSettingsPath = filepath.Join(home, "settings.json")
	a.readRunner = func(work func() func()) { work()() }
	a.writeRunner = func(work func() func()) { work()() }
	a.repositoryPath = second
	a.selected[second] = true
	a.repositoryFiles = map[string]repositoryFileState{second: {Loaded: true}}
	a.workingFileTree = WorkingTree{Repository: a.repos[1]}
	a.workingFileKey = workingFileKey(second, "two.txt")
	a.openScanSettings()
	tester := ui.NewTester(a.view, 1560, 940)
	if err := tester.Click("Remove scan root " + second); err != nil {
		t.Fatal(err)
	}
	if len(a.entries) != 2 {
		t.Fatal("draft removal changed repositories before Apply")
	}
	if err := tester.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	if !a.settings.Open || len(a.entries) != 1 || a.entries[0].Path != first || a.repositoryPath != "" || a.selected[second] || a.workingFileKey != "" {
		t.Fatalf("stale removed repository state: %s", a.output)
	}
	if _, ok := a.repositoryFiles[second]; ok {
		t.Fatal("removed file cache survived")
	}
	entries, err := loadEntries()
	if err != nil || len(entries) != 1 || entries[0].Path != first {
		t.Fatal("repository removal not persisted", entries, err)
	}
	if _, err := os.Stat(filepath.Join(second, ".git")); err != nil {
		t.Fatal("removed repository files were deleted", err)
	}
	if err := tester.Click("OK"); err != nil {
		t.Fatal(err)
	}
	if a.settings.Open {
		t.Fatal("OK did not close settings")
	}
}
