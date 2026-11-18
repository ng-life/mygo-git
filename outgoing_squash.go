package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type rewriteCommit struct{ Hash, Tree, Parent, Name, Email, Date, Message string }

func readRewriteCommit(path, hash string) (rewriteCommit, error) {
	result := rewriteCommit{Hash: hash}
	raw, err := rawGit(path, "show", "-s", "--format=%T%x00%P%x00%an%x00%ae%x00%aI%x00%B", hash, "--")
	if err != nil {
		return result, err
	}
	fields := strings.SplitN(string(raw), "\x00", 6)
	if len(fields) != 6 {
		return result, fmt.Errorf("Cannot read commit metadata")
	}
	parents := strings.Fields(fields[1])
	if len(parents) > 1 {
		return result, fmt.Errorf("Squashing history containing merge commits is not supported")
	}
	result.Tree = fields[0]
	if len(parents) == 1 {
		result.Parent = parents[0]
	}
	result.Name = fields[2]
	result.Email = fields[3]
	result.Date = fields[4]
	result.Message = strings.TrimRight(fields[5], "\n")
	return result, nil
}
func squashOutgoing(source Outgoing, selected []Commit, message string) (string, error) {
	path := source.Repository.Path
	if len(selected) < 2 || strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("Select at least two unpushed commits and enter a message")
	}
	head, ref := currentPosition(path)
	if head != source.Head || ref != source.Ref || ref == "" {
		return "", fmt.Errorf("HEAD or branch changed, or HEAD is detached. Refresh to continue")
	}
	if operationInProgress(path) {
		return "", fmt.Errorf("Git operation in progress")
	}
	clean := func() error {
		status, err := git(path, "status", "--porcelain")
		if err != nil {
			return err
		}
		if status != "" {
			return fmt.Errorf("Squash requires a clean working tree. Staged and uncommitted changes are preserved")
		}
		return nil
	}
	if err := clean(); err != nil {
		return "", err
	}
	fresh := readOutgoing(context.Background(), source.Repository)
	if fresh.Error != "" {
		return "", fmt.Errorf("%s", fresh.Error)
	}
	outgoing := map[string]bool{}
	for _, c := range fresh.Commits {
		outgoing[c.Hash] = true
	}
	chosen := map[string]bool{}
	for _, c := range selected {
		if c.Repository.Path != path || !outgoing[c.Hash] {
			return "", fmt.Errorf("Only unpushed commits in the same repository can be squashed")
		}
		chosen[c.Hash] = true
	}
	if len(chosen) < 2 {
		return "", fmt.Errorf("Select at least two different commits")
	}
	chain, err := git(path, "rev-list", "--first-parent", "--reverse", head, "--")
	if err != nil {
		return "", err
	}
	hashes := strings.Fields(chain)
	first := -1
	found := 0
	for i, hash := range hashes {
		if chosen[hash] {
			if first < 0 {
				first = i
			}
			found++
		}
	}
	if found != len(chosen) {
		return "", fmt.Errorf("Selected commits are not in the current branch's linear history")
	}
	var plan []rewriteCommit
	for _, hash := range hashes[first:] {
		if !outgoing[hash] {
			return "", fmt.Errorf("Rewrite range contains published commits")
		}
		if err := requireUnpublished(path, hash); err != nil {
			return "", err
		}
		commit, err := readRewriteCommit(path, hash)
		if err != nil {
			return "", err
		}
		plan = append(plan, commit)
	}
	original, err := readRewriteCommit(path, head)
	if err != nil {
		return "", err
	}
	temp, err := os.MkdirTemp("", "mygo-squash-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	index := filepath.Join(temp, "index")
	env := map[string]string{"GIT_INDEX_FILE": index}
	base := plan[0].Parent
	baseTree := base
	if baseTree == "" {
		baseTree, err = git(path, "hash-object", "-w", "-t", "tree", "--stdin")
		if err != nil {
			return "", err
		}
	}
	run := func(args ...string) (string, error) {
		out, err := commandGitEnv(context.Background(), 90*time.Second, path, env, args...)
		return strings.TrimSpace(string(out)), err
	}
	if _, err = run("read-tree", baseTree); err != nil {
		return "", err
	}
	apply := func(commit rewriteCommit) error {
		parent := commit.Parent
		if parent == "" {
			parent = baseTree
		}
		patch, err := rawGit(path, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", parent, commit.Hash, "--")
		if err != nil {
			return err
		}
		if len(patch) == 0 {
			return nil
		}
		file := filepath.Join(temp, "change.patch")
		if err = os.WriteFile(file, patch, 0600); err != nil {
			return err
		}
		if _, err = run("apply", "--cached", "--3way", "--allow-empty", "--whitespace=nowarn", file); err != nil {
			return fmt.Errorf("Conflict while replaying commits; original branch, index, and files unchanged: %w", err)
		}
		return nil
	}
	for _, commit := range plan {
		if chosen[commit.Hash] {
			if err = apply(commit); err != nil {
				return "", err
			}
		}
	}
	msgFile := filepath.Join(temp, "message.txt")
	if err = os.WriteFile(msgFile, []byte(message), 0600); err != nil {
		return "", err
	}
	if _, err = run("hook", "run", "--ignore-missing", "commit-msg", "--", msgFile); err != nil {
		return "", fmt.Errorf("commit-msg hook rejected squash: %w", err)
	}
	checkedMessage, err := os.ReadFile(msgFile)
	if err != nil {
		return "", err
	}
	signed, _ := git(path, "config", "--bool", "commit.gpgsign")
	makeCommit := func(author rewriteCommit, parent, message string) (string, error) {
		tree, err := run("write-tree")
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(msgFile, []byte(message), 0600); err != nil {
			return "", err
		}
		args := []string{"commit-tree", tree, "-F", msgFile}
		if parent != "" {
			args = append(args, "-p", parent)
		}
		if signed == "true" {
			args = append(args, "-S")
		}
		overrides := map[string]string{"GIT_INDEX_FILE": index, "GIT_AUTHOR_NAME": author.Name, "GIT_AUTHOR_EMAIL": author.Email, "GIT_AUTHOR_DATE": author.Date}
		out, err := commandGitEnv(context.Background(), 90*time.Second, path, overrides, args...)
		if err != nil {
			return "", err
		}
		hash := strings.TrimSpace(string(out))
		actual, err := readRewriteCommit(path, hash)
		if err != nil || actual.Tree != tree || actual.Parent != parent || actual.Message != strings.TrimRight(message, "\n") {
			return "", fmt.Errorf("Cannot verify rewritten commit")
		}
		return hash, nil
	}
	next, err := makeCommit(plan[0], base, string(checkedMessage))
	if err != nil {
		return "", err
	}
	squashed := next
	for _, commit := range plan {
		if !chosen[commit.Hash] {
			if err = apply(commit); err != nil {
				return "", err
			}
			next, err = makeCommit(commit, next, commit.Message)
			if err != nil {
				return "", err
			}
		}
	}
	final, err := readRewriteCommit(path, next)
	if err != nil || final.Tree != original.Tree {
		return "", fmt.Errorf("Rewritten tree differs from the original HEAD; original branch unchanged")
	}
	if err = clean(); err != nil {
		return "", err
	}
	now, currentRef := currentPosition(path)
	if now != head || currentRef != ref {
		return "", fmt.Errorf("Branch changed during squash; the original branch was not updated")
	}
	if out, err := git(path, "update-ref", "-m", "mygo-git: squash unpublished commits", ref, next, head); err != nil {
		return out, err
	}
	return fmt.Sprintf("Squashed %d commits into %s\nOriginal HEAD: %s\nNew HEAD: %s\nFinal tree unchanged; recover the original history through the branch reflog", len(chosen), squashed, head, next), nil
}
