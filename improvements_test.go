package main

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestCommitReconcilesErrorAfterSuccessfulRefUpdate(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "before\n")
	writeWorkingFile(t, path, "file.txt", "after\n")
	request := requestFor(t, path, "file.txt")
	out, err := commitWorkingTreeWithCommand(request, "verified commit\n\nbody", func(path string, args ...string) (string, error) {
		out, err := git(path, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out, context.DeadlineExceeded
	})
	if err != nil || !strings.Contains(out, "已核对成功") || !strings.Contains(out, "deadline exceeded") {
		t.Fatalf("%s: %v", out, err)
	}
	if readWorkingTree(request.Tree.Repository).Head == request.Tree.Head {
		t.Fatal("commit missing")
	}
}
func TestCommitUnexpectedMessageIsUncertain(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "before\n")
	writeWorkingFile(t, path, "file.txt", "after\n")
	request := requestFor(t, path, "file.txt")
	out, err := commitWorkingTreeWithCommand(request, "intended", func(path string, args ...string) (string, error) {
		out, e := git(path, "commit", "-m", "unexpected")
		if e != nil {
			t.Fatal(e)
		}
		return out, errors.New("connection lost")
	})
	if !isUncertainCommit(err) || !strings.Contains(formatResults([]OperationResult{{Output: out, Err: err}}), "[结果待确认]") {
		t.Fatalf("%s: %v", out, err)
	}
}
func TestCommitRejectedHasVerifiedUnchangedHead(t *testing.T) {
	path := testRepo(t)
	writeCommit(t, path, "file.txt", "before\n")
	writeWorkingFile(t, path, "file.txt", "after\n")
	_, err := commitWorkingTreeWithCommand(requestFor(t, path, "file.txt"), "rejected", func(string, ...string) (string, error) { return "", errors.New("rejected") })
	if err == nil || isUncertainCommit(err) || !strings.Contains(err.Error(), "HEAD 未变化") {
		t.Fatal(err)
	}
}
func TestHistoryCacheUsesImmutableTipsAndInvalidatesChangedRepo(t *testing.T) {
	a, b := testRepo(t), testRepo(t)
	writeCommit(t, a, "a", "first")
	writeCommit(t, b, "b", "second")
	sources := historySources([]Repository{inspect(Entry{Path: a}), inspect(Entry{Path: b})}, "", false)
	rows, err, cache := cachedHistory(context.Background(), sources, nil)
	if err != "" || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	for key, entry := range cache {
		entry.Commits = append([]Commit(nil), entry.Commits...)
		for i := range entry.Commits {
			entry.Commits[i].Subject = "cached sentinel"
		}
		cache[key] = entry
	}
	writeCommit(t, a, "a", "new")
	rows, err, _ = cachedHistory(context.Background(), sources, cache)
	if err != "" {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Repository.Path == b && row.Subject != "cached sentinel" {
			t.Fatal("unchanged repository was reloaded")
		}
		if row.Repository.Path == a && row.Subject == "cached sentinel" {
			t.Fatal("changed repository reused old history")
		}
	}
}
func TestIncrementalWorkingRefreshPreservesOtherRepoAndSelection(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	first, second := a.workingTrees[0], a.workingTrees[1]
	a.selectWorkingFile(second, second.Files[0])
	key := a.workingFileKey
	// A new file in the other repository must wait for its own refresh.
	writeWorkingFile(t, second.Repository.Path, "external.txt", "external")
	a.reloadWorkingTreesFor([]Repository{first.Repository})
	if a.workingFileKey != key {
		t.Fatal("unrelated comparison cleared")
	}
	for _, tree := range a.workingTrees {
		if tree.Repository.Path == second.Repository.Path && len(tree.Files) != len(second.Files) {
			t.Fatal("unrelated repository rescanned")
		}
	}
	a.selectCommit(a.commits[0])
	selected := a.selectedCommit
	a.reloadHistory()
	if a.selectedCommit != selected || !a.commitSelection.Has(selected) {
		t.Fatal("refresh lost commit selection")
	}
}
func TestSupersededReadContextIsCanceled(t *testing.T) {
	a := testClient(nil)
	var queue []func() func()
	a.readRunner = func(fn func() func()) { queue = append(queue, fn) }
	var first context.Context
	a.startRead("test", func(ctx context.Context) func() { first = ctx; return func() { t.Fatal("canceled result applied") } })
	apply := queue[0]()
	a.startRead("test", func(context.Context) func() { return func() {} })
	if first.Err() != context.Canceled {
		t.Fatal("old read not canceled")
	}
	apply()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rawGitContext(ctx, t.TempDir(), "status"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestDiffVirtualizationRendersVisibleRowsAndReachesLastLine(t *testing.T) {
	var text strings.Builder
	for i := 1; i <= 12000; i++ {
		fmt.Fprintf(&text, "line-%05d %s\n", i, strings.Repeat("x", 160))
	}
	document := prepareDiff(text.String())
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() { comparisonPane(c, "large diff", document, map[int]bool{12000: true}, true) })
	}, 700, 400)
	if !tester.HasText(document.Lines[0]) || len(tester.Texts()) > 150 {
		t.Fatalf("not virtualized: %d texts", len(tester.Texts()))
	}
	tester.Scroll(300, 200, 0, 1000000)
	tester.Frame()
	if !tester.HasText(document.Lines[11999]) {
		t.Fatalf("last line unreachable: %v", tester.Texts())
	}
	if path := os.Getenv("MYGO_VIRTUAL_DIFF_SCREENSHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGitReadCancellationStopsRunningProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := rawGitContext(ctx, testRepo(t), "-c", "alias.wait=!sleep 10", "wait")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation took %s: %v", time.Since(start), err)
	}
}
func TestCommitSuccessfulExitWithoutNewHeadIsUncertain(t *testing.T) {
	path := testRepo(t)
	writeWorkingFile(t, path, "new.txt", "new")
	_, err := commitWorkingTreeWithCommand(requestFor(t, path, "new.txt"), "missing commit", func(string, ...string) (string, error) { return "", nil })
	if !isUncertainCommit(err) {
		t.Fatal(err)
	}
}
func TestCommitMatchingMessageButWrongContentIsUncertain(t *testing.T) {
	path := testRepo(t)
	writeWorkingFile(t, path, "new.txt", "intended")
	_, err := commitWorkingTreeWithCommand(requestFor(t, path, "new.txt"), "same message", func(path string, args ...string) (string, error) {
		writeWorkingFile(t, path, "new.txt", "different")
		return git(path, args...)
	})
	if !isUncertainCommit(err) || !strings.Contains(err.Error(), "文件内容") {
		t.Fatal(err)
	}
}
