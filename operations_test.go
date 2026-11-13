package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRepo(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	mustGit(t, path, "init", "-b", "main")
	mustGit(t, path, "config", "core.hooksPath", filepath.Join(path, "no-hooks"))
	mustGit(t, path, "config", "user.name", "Test")
	mustGit(t, path, "config", "user.email", "test@example.com")
	mustGit(t, path, "config", "commit.gpgsign", "false")
	mustGit(t, path, "commit", "--allow-empty", "-m", "initial")
	return path
}
func mustGit(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := git(path, args...)
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return out
}
func writeCommit(t *testing.T, path, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, file), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "add", "--", file)
	mustGit(t, path, "commit", "-m", "change "+file)
}
func featureRepo(t *testing.T) Repository {
	t.Helper()
	path := testRepo(t)
	mustGit(t, path, "switch", "-c", "feature/order")
	writeCommit(t, path, "feature.txt", "feature\n")
	mustGit(t, path, "switch", "main")
	mustGit(t, path, "remote", "add", "origin", filepath.Join(t.TempDir(), "remote.git"))
	mustGit(t, path, "update-ref", "refs/remotes/origin/feature/order", "refs/heads/feature/order")
	mustGit(t, path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/feature/order")
	return inspect(Entry{Path: path})
}
func TestRemoteBranchesAndTree(t *testing.T) {
	r := featureRepo(t)
	if len(r.RemoteBranches) != 1 || r.RemoteBranches[0] != "origin/feature/order" {
		t.Fatalf("remote HEAD must be excluded: %v", r.RemoteBranches)
	}
	tree := branchTree([]Repository{r, r}, true)
	leaf := tree.Children[0].Children[0].Children[0]
	if tree.Children[0].Name != "origin" || leaf.Branch != "origin/feature/order" || len(leaf.Children) != 0 {
		t.Fatalf("remote hierarchy: %+v", leaf)
	}
	a := &client{repos: []Repository{r}, group: "/origin/feature", groupRemote: true}
	if len(a.visible()) != 1 {
		t.Fatal("remote prefix filter failed")
	}
	a.groupRemote = false
	if len(a.visible()) != 0 {
		t.Fatal("local and remote groups must be separate")
	}
}
func TestBranchBatchOperations(t *testing.T) {
	for _, action := range []string{"checkout", "merge", "rebase", "diff", "log"} {
		t.Run(action, func(t *testing.T) {
			repos := []Repository{featureRepo(t), featureRepo(t)}
			if action == "rebase" {
				for _, r := range repos {
					writeCommit(t, r.Path, "main.txt", "main\n")
				}
			}
			targets := branchTargets(repos, "feature/order", false)
			results := executeBranchBatch(targets, action, "feature/order", false)
			if len(results) != 2 {
				t.Fatal("must execute on every repository")
			}
			for _, r := range results {
				if r.Err != nil {
					t.Fatalf("%s: %s %v", action, r.Output, r.Err)
				}
				switch action {
				case "checkout":
					if got := mustGit(t, r.Repository.Path, "branch", "--show-current"); got != "feature/order" {
						t.Fatal(got)
					}
				case "merge", "rebase":
					mustGit(t, r.Repository.Path, "merge-base", "--is-ancestor", "feature/order", "HEAD")
				case "diff":
					if !strings.Contains(r.Output, "feature.txt") {
						t.Fatal(r.Output)
					}
				case "log":
					if !strings.Contains(r.Output, "change feature.txt") {
						t.Fatal(r.Output)
					}
				}
			}
		})
	}
}
func TestRemoteCheckoutTracksWithoutOverwritingLocal(t *testing.T) {
	r := featureRepo(t)
	mustGit(t, r.Path, "branch", "-D", "feature/order")
	if out, err := runBranchOperation(r.Path, "checkout", "origin/feature/order", true); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if got := mustGit(t, r.Path, "rev-parse", "--symbolic-full-name", "@{upstream}"); got != "refs/remotes/origin/feature/order" {
		t.Fatal(got)
	}
	mustGit(t, r.Path, "switch", "main")
	if out, err := runBranchOperation(r.Path, "checkout", "origin/feature/order", true); err != nil {
		t.Fatalf("existing tracked branch: %s %v", out, err)
	}
	mustGit(t, r.Path, "switch", "main")
	mustGit(t, r.Path, "branch", "--unset-upstream", "feature/order")
	if _, err := runBranchOperation(r.Path, "checkout", "origin/feature/order", true); err == nil {
		t.Fatal("must not reuse unrelated local branch")
	}
}
func TestBatchSkipsDirtyAndContinues(t *testing.T) {
	repos := []Repository{featureRepo(t), featureRepo(t)}
	if err := os.WriteFile(filepath.Join(repos[0].Path, "dirty.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	results := executeBranchBatch(repos, "checkout", "feature/order", false)
	if results[0].Err == nil || results[1].Err != nil {
		t.Fatalf("results: %+v", results)
	}
	if got := mustGit(t, repos[0].Path, "branch", "--show-current"); got != "main" {
		t.Fatal("dirty repository changed")
	}
	if _, err := runBranchOperation(repos[0].Path, "diff", "feature/order", false); err != nil {
		t.Fatal("read only diff should work with dirty worktree")
	}
}
func TestConflictPreservesStateAndBlocksNextWrite(t *testing.T) {
	for _, action := range []string{"merge", "rebase"} {
		t.Run(action, func(t *testing.T) {
			path := testRepo(t)
			writeCommit(t, path, "shared.txt", "base\n")
			mustGit(t, path, "switch", "-c", "feature/conflict")
			writeCommit(t, path, "shared.txt", "feature\n")
			mustGit(t, path, "switch", "main")
			writeCommit(t, path, "shared.txt", "main\n")
			_, err := runBranchOperation(path, action, "feature/conflict", false)
			if err == nil || !operationInProgress(path) {
				t.Fatal("expected preserved conflict state")
			}
			if _, err := runBranchOperation(path, "checkout", "feature/conflict", false); err == nil {
				t.Fatal("must block writes during unresolved operation")
			}
			mustGit(t, path, action, "--abort")
			if operationInProgress(path) {
				t.Fatal("abort should clear operation state")
			}
		})
	}
}

func TestSingleRepositoryOperationsStayScoped(t *testing.T) {
	first, second := featureRepo(t), featureRepo(t)
	results := executeBranchBatch([]Repository{first}, "checkout", "feature/order", false)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("single repository result: %+v", results)
	}
	if got := mustGit(t, second.Path, "branch", "--show-current"); got != "main" {
		t.Fatal("unselected repository must be unchanged")
	}
	writeCommit(t, first.Path, "tracked.txt", "before\n")
	if err := os.WriteFile(filepath.Join(first.Path, "tracked.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runRepositoryOperation(first.Path, "diff · 工作区修改")
	if err != nil || !strings.Contains(out, "+after") {
		t.Fatalf("working tree diff: %s %v", out, err)
	}
	out, err = runRepositoryOperation(first.Path, "刷新状态")
	if err != nil || !strings.Contains(out, "tracked.txt") {
		t.Fatalf("status: %s %v", out, err)
	}
	out, err = runRepositoryOperation(first.Path, "log · 当前分支提交")
	if err != nil || !strings.Contains(out, "change tracked.txt") {
		t.Fatalf("log: %s %v", out, err)
	}
}
