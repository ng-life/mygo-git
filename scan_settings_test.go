package main

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func markScanRepository(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoverDepthThreeAndPrunesRepositories(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	one := filepath.Join(root, "one")
	three := filepath.Join(root, "group", "team", "three")
	four := filepath.Join(root, "deep", "a", "b", "four")
	for _, path := range []string{one, three, four, filepath.Join(one, "nested")} {
		markScanRepository(t, path)
	}
	entries, err := discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	if len(paths) != 2 || !slices.Contains(paths, one) || !slices.Contains(paths, three) {
		t.Fatalf("unexpected depth results: %v", paths)
	}
	// An explicitly selected deep project is a new depth-zero root.
	entries, err = discover([]string{root, four, one})
	if err != nil || len(entries) != 3 {
		t.Fatalf("direct root / dedup: %v %v", entries, err)
	}
	entries, err = discover([]string{one})
	if err != nil || len(entries) != 1 || entries[0].Path != one {
		t.Fatalf("repository pruning: %v %v", entries, err)
	}
}
func TestDiscoverWorktreeMarkerAndExplicitSymlinkRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktree")
	if err := os.MkdirAll(worktree, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /example/worktree"), 0600); err != nil {
		t.Fatal(err)
	}
	markScanRepository(t, filepath.Join(worktree, "nested"))
	linkedRepo := filepath.Join(t.TempDir(), "project")
	markScanRepository(t, linkedRepo)
	linkedRepo, err = filepath.EvalSymlinks(linkedRepo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedRepo, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	entries, err := discover([]string{root})
	if err != nil || len(entries) != 1 || entries[0].Path != worktree {
		t.Fatalf("worktree/prune: %v %v", entries, err)
	}
	entries, err = discover([]string{filepath.Join(root, "linked"), linkedRepo})
	if err != nil || len(entries) != 1 || entries[0].Path != linkedRepo {
		t.Fatalf("explicit symlink: %v %v", entries, err)
	}
}
func TestScanFolderSettingsPersistAndCancel(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	roots := []string{filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "parent")}
	if err := saveScanFolders(file, roots); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadScanFolders(file)
	if err != nil || !slices.Equal(loaded, roots) {
		t.Fatalf("%v %v", loaded, err)
	}
	a := testClient(nil)
	a.scanFolders = loaded
	a.openScanSettings()
	a.appendScanFolders([]string{roots[0], filepath.Join(roots[1], "..", filepath.Base(roots[1])), "/new/projects"})
	if len(a.settings.Folders) != 3 || len(a.scanFolders) != 2 {
		t.Fatal("draft changes leaked or duplicate paths")
	}
	a.settings.Open = false
	a.openScanSettings()
	if !slices.Equal(a.settings.Folders, roots) {
		t.Fatal("cancel did not discard draft")
	}
	if err = saveScanFolders(file, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadScanFolders(file)
	if err != nil || len(loaded) != 0 {
		t.Fatalf("clear settings: %v %v", loaded, err)
	}
}
func TestScanSettingsUIReplacesToolbarScanning(t *testing.T) {
	a := testClient(nil)
	a.scanFolders = []string{"/Users/example/Projects", "/Users/example/my-project"}
	tester := ui.NewTester(a.view, 1560, 940)
	if tester.HasText("Scan and Add") || tester.HasText("Choose Directory") {
		t.Fatal("scanning remains in toolbar")
	}
	chooseApplicationAction(t, a, "settings")
	tester.Frame()
	if !tester.HasText("Version Control  ›  Git") {
		t.Fatal(tester.Texts())
	}
	if err := tester.Click("Scan root path"); err != nil {
		t.Fatal(err)
	}
	tester.Type("/Users/example/more")
	if err := tester.Click("Add Directory"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Folders) != 3 || len(a.scanFolders) != 2 {
		t.Fatal("settings draft was not isolated")
	}
	if dest := os.Getenv("MYGO_SETTINGS_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tester.Click("Remove scan root /Users/example/more"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Folders) != 2 {
		t.Fatal("remove failed")
	}
	if err := tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.settings.Open || len(a.scanFolders) != 2 {
		t.Fatal("cancel changed persisted roots")
	}
}

func TestSaveAndScanFoldersSynchronizesRepositories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	rel, err := filepath.Rel(home, configFile())
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("configuration not isolated: %s", configFile())
	}
	first, second := testRepo(t), testRepo(t)
	a := testClient(nil)
	a.scanSettingsPath = filepath.Join(home, "settings.json")
	a.readRunner = func(work func() func()) { work()() }
	a.writeRunner = func(work func() func()) { work()() }
	a.openScanSettings()
	a.scanFoldersJob([]string{first, second}, true)
	if a.busy || a.settings.Open || len(a.entries) != 2 || len(a.repos) != 2 || len(a.scanFolders) != 2 {
		t.Fatalf("save and scan: %s", a.output)
	}
	roots, err := loadScanFolders(a.scanConfigPath())
	if err != nil || len(roots) != 2 {
		t.Fatalf("roots: %v %v", roots, err)
	}
	entries, err := loadEntries()
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries: %v %v", entries, err)
	}
	a.scanFoldersJob(nil, true)
	if len(a.scanFolders) != 0 || len(a.entries) != 0 || len(a.repos) != 0 {
		t.Fatal("removing all roots must clear registered repositories")
	}
}
