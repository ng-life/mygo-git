package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWindowShortcutsTabsFocusAndModalGuard(t *testing.T) {
	a := workingClient(t)
	tester := ui.NewTester(a.view, 1560, 940)
	mod := primaryShortcutModifier()
	for _, index := range []int{2, 0, 1} {
		tester.Key(mod, ui.Key(int(ui.Key0)+index))
		if a.tab != index {
			t.Fatalf("tab shortcut %d -> %d", index, a.tab)
		}
	}
	tester.Key(mod, ui.KeyK)
	tester.Type("keyboard message")
	if a.commitMessage != "keyboard message" {
		t.Fatal("commit focus shortcut failed", a.commitMessage)
	}
	if err := tester.Click("New Changelist"); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, a.repos[0].Path, "rev-parse", "HEAD")
	tester.Key(mod, ui.Key2)
	tester.Key(mod, ui.KeyEnter)
	if a.tab != 1 || !a.changelistDialog.Open || mustGit(t, a.repos[0].Path, "rev-parse", "HEAD") != before {
		t.Fatal("window shortcuts escaped modal")
	}
	if err := tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tester.Key(mod, ui.Key2)
	tester.Key(mod, ui.KeyF)
	tester.Type("feature")
	if a.logsQuery != "feature" {
		t.Fatal("branch search not focused", a.logsQuery)
	}
	tester.Key(mod, ui.Key0)
	tester.Key(mod, ui.KeyF)
	tester.Type("supplychain")
	if a.repositoryQuery != "supplychain" {
		t.Fatal("repository search not focused", a.repositoryQuery)
	}
	tester.Key(mod, ui.Key1)
	a.toggleWorkingFiles(a.workingTrees[0].Repository.Path, a.workingTrees[0].Files, true)
	tester.Frame()
	tester.Key(mod, ui.KeyEnter)
	if mustGit(t, a.repos[0].Path, "rev-parse", "HEAD") == before {
		t.Fatal("commit shortcut did not commit selection")
	}
	chooseApplicationAction(t, a, "shortcuts")
	tester.Frame()
	if !tester.HasText("Keyboard Shortcuts") || !a.shortcutHelpOpen {
		t.Fatal("help missing")
	}
}
func TestBackgroundProgressAndFailureUI(t *testing.T) {
	a := testClient(nil)
	var pending func() func()
	a.writeRunner = func(job func() func()) { pending = job }
	a.jobProgress("Pull · 2 repositories", func(report taskReporter) (func(), error) {
		report("Pull · app-one", 0, 4, nil)
		report("Pull · app-one", 1, 4, nil)
		report("Pull · app-two", 2, 4, fmt.Errorf("app-two 拉取failed"))
		return func() {}, nil
	})
	tester := ui.NewTester(a.view, 1560, 940)
	if len(a.tasks.snapshot()) != 1 || a.tasks.snapshot()[0].Finished {
		t.Fatal("task not started")
	}
	apply := pending()
	tester.Frame()
	task := a.tasks.snapshot()[0]
	if task.Done != 2 || task.Total != 4 || taskProgress(task) != .5 || task.Finished {
		t.Fatalf("%+v", task)
	}
	if !tester.HasText("2 / 4 steps") {
		t.Fatal("status progress missing", tester.Texts())
	}
	if err := tester.Click("Background Tasks"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Pull · app-two: app-two 拉取failed") {
		t.Fatal("task details missing")
	}
	if dest := os.Getenv("MYGO_TASKS_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		fresh := ui.NewTester(a.view, 1560, 940)
		if err = png.Encode(f, fresh.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := tester.Click("Close"); err != nil {
		t.Fatal(err)
	}
	apply()
	tester.Frame()
	task = a.tasks.snapshot()[0]
	if !task.Finished || task.Error == "" || !tester.HasText("Completed with Errors") {
		t.Fatal("partial failure incorrectly marked successful", task)
	}
}
func TestReadTaskCancellationAndConcurrentProgress(t *testing.T) {
	a := testClient(nil)
	var jobs []func() func()
	a.readRunner = func(job func() func()) { jobs = append(jobs, job) }
	a.startRead("history", func(context.Context) func() { return func() { t.Fatal("obsolete read applied") } })
	old := a.tasks.snapshot()[0].ID
	applied := false
	a.startRead("history", func(context.Context) func() { return func() { applied = true } })
	jobs[0]()()
	for _, task := range a.tasks.snapshot() {
		if task.ID == old && !task.Cancelled {
			t.Fatal("old task not cancelled")
		}
	}
	if a.tasks.snapshot()[0].Finished {
		t.Fatal("old completion finished new task")
	}
	jobs[1]()()
	if !applied || !a.tasks.snapshot()[0].Finished {
		t.Fatal("current read not completed")
	}
	var store backgroundTasks
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		id := store.begin(fmt.Sprintf("task-%d", i), true)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				store.report(id, "repo", j, 100, nil)
				_ = store.snapshot()
			}
			store.finish(id, nil)
		}()
	}
	wg.Wait()
	for _, task := range store.snapshot() {
		if !task.Finished || task.Done != 100 {
			t.Fatal(task)
		}
	}
	running := store.begin("still running", false)
	for i := 0; i < 25; i++ {
		id := store.begin("completed", false)
		store.finish(id, nil)
	}
	completed, found := 0, false
	for _, task := range store.snapshot() {
		if task.Finished {
			completed++
		}
		if task.ID == running && !task.Finished {
			found = true
		}
	}
	if completed != 20 || !found {
		t.Fatal("history bound evicted running work or retained too many completed tasks")
	}

}
func TestMultiRepositoryPullReportsCompletionAndFailure(t *testing.T) {
	a := workingClient(t)
	// The first repository succeeds through a local bare remote; the second has
	// no upstream. A failed repository must not hide progress or stop inspection.
	path := a.repos[0].Path
	mustGit(t, path, "add", ".")
	mustGit(t, path, "commit", "-m", "clean fixture")
	remote := t.TempDir() + "/remote.git"
	mustGit(t, path, "init", "--bare", remote)
	mustGit(t, path, "remote", "add", "origin", remote)
	mustGit(t, path, "push", "-u", "origin", "main")
	second := a.repos[1].Path
	mustGit(t, second, "add", ".")
	mustGit(t, second, "commit", "-m", "clean second fixture")
	tester := ui.NewTester(a.view, 1560, 940)
	tester.Key(primaryShortcutModifier(), ui.KeyT)
	var task backgroundTask
	for _, entry := range a.tasks.snapshot() {
		if entry.Write {
			task = entry
			break
		}
	}
	if !task.Finished || task.Done != 4 || task.Total != 4 || task.Error == "" {
		t.Fatalf("%+v", task)
	}
}
