package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBranchGrouping(t *testing.T) {
	repos := []Repository{{Name: "a", Branches: []string{"main", "feature/order/export", "feature/order/import"}}, {Name: "b", Branches: []string{"main", "feature/order/export"}}}
	tree := branchTree(repos, false)
	if len(tree.Children) != 2 || tree.Children[0].Name != "feature" || tree.Children[0].Count != 2 {
		t.Fatalf("incorrect tree: %+v", tree.Children)
	}
	order := tree.Children[0].Children[0]
	if order.Name != "order" || order.Count != 2 || order.Children[0].Name != "export" || order.Children[0].Count != 2 {
		t.Fatalf("incorrect nested grouping: %+v", order)
	}
	a := &client{repos: repos, group: "/feature/order/export"}
	if len(a.visible()) != 2 {
		t.Fatal("same branch should match both repositories")
	}
	a.group = "/feature/order"
	if len(a.visible()) != 2 {
		t.Fatal("prefix must match nested branches")
	}
}
func TestDiscoverAndInspect(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "repo")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := git(path, args...); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
	run("init", "-b", "main")
	run("config", "core.hooksPath", filepath.Join(root, "no-hooks"))
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	run("branch", "feature/order/export")
	run("remote", "add", "origin", "git@example.com:team/repo.git")
	entries, err := discover([]string{root, path})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dedup failed: %v", entries)
	}
	repo := inspect(entries[0])
	if repo.Error != "" || repo.Branch != "main" || len(repo.Branches) != 2 {
		t.Fatalf("inspect: %+v", repo)
	}
	run("switch", "--", "feature/order/export")
	if inspect(entries[0]).Branch != "feature/order/export" {
		t.Fatal("switch failed")
	}
}
