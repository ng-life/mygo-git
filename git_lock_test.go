package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCanceledCommitReleasesIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hook fixture")
	}
	repo := testRepo(t)
	writeCommit(t, repo, "file.txt", "before\n")
	writeWorkingFile(t, repo, "file.txt", "after\n")
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch .git/hook-started\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "config", "core.hooksPath", filepath.Dir(hook))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := commandGit(ctx, 10*time.Second, repo, "commit", "-am", "cancel fixture")
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(repo, ".git", "hook-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("hook never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); err != nil {
		cancel()
		t.Fatal("fixture did not acquire index.lock", err)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("canceled commit succeeded")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatal("canceled Git command left index.lock", err)
	}
	if out, err := git(repo, "add", "file.txt"); err != nil {
		t.Fatal("subsequent Git command blocked", out, err)
	}
	if got := mustGit(t, repo, "log", "-1", "--format=%s"); strings.Contains(got, "cancel fixture") {
		t.Fatal("canceled commit changed HEAD")
	}
}

func TestGitDoesNotRemoveExistingIndexLock(t *testing.T) {
	repo := testRepo(t)
	lock := filepath.Join(repo, ".git", "index.lock")
	content := []byte("owned by another Git process\n")
	if err := os.WriteFile(lock, content, 0600); err != nil {
		t.Fatal(err)
	}
	writeWorkingFile(t, repo, "file.txt", "content\n")
	if _, err := git(repo, "add", "file.txt"); err == nil {
		t.Fatal("Git ignored an existing lock")
	}
	got, err := os.ReadFile(lock)
	if err != nil || string(got) != string(content) {
		t.Fatal("existing lock was removed or changed", err)
	}
}
func TestReadOnlyStatusDoesNotRefreshIndex(t *testing.T) {
	repo := testRepo(t)
	writeCommit(t, repo, "file.txt", "same\n")
	index := filepath.Join(repo, ".git", "index")
	before, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(repo, "file.txt"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "status", "--porcelain=v1"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("read-only status rewrote the index")
	}
}
func TestQuitDefersUntilWriteJobFinishes(t *testing.T) {
	a := testClient(nil)
	a.busy = true
	if a.canQuitSafely() || !a.quitPending {
		t.Fatal("busy job allowed process shutdown")
	}
	a.busy = false
	if !a.canQuitSafely() {
		t.Fatal("completed job still prevents shutdown")
	}
}

func TestCanceledAddReleasesIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX clean filter fixture")
	}
	repo := testRepo(t)
	writeCommit(t, repo, "file.txt", "before\n")
	filter := filepath.Join(repo, "slow-clean.sh")
	if err := os.WriteFile(filter, []byte("#!/bin/sh\ntouch .git/filter-started\nsleep 30\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "config", "filter.slow.clean", filter)
	writeWorkingFile(t, repo, ".gitattributes", "file.txt filter=slow\n")
	writeWorkingFile(t, repo, "file.txt", "after\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := commandGit(ctx, 10*time.Second, repo, "add", "file.txt"); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(repo, ".git", "filter-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("filter never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); err != nil {
		cancel()
		<-done
		t.Fatal("fixture has no index lock", err)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("canceled add succeeded")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatal("canceled add left index.lock", err)
	}
	if got := mustGit(t, repo, "show", ":file.txt"); got != "before" {
		t.Fatal("canceled add changed staged content", got)
	}
}
