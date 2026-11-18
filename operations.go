package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A remote branch name includes its remote, e.g. origin/feature/order.
func fullBranchRef(branch string, remote bool) string {
	if remote {
		return "refs/remotes/" + branch
	}
	return "refs/heads/" + branch
}
func operationInProgress(path string) bool {
	for _, name := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD"} {
		p, err := git(path, "rev-parse", "--git-path", name)
		if err != nil {
			return true
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
func runBranchOperation(path, action, branch string, remote bool) (string, error) {
	ref := fullBranchRef(branch, remote)
	if _, err := git(path, "check-ref-format", ref); err != nil {
		return "", fmt.Errorf("Invalid branch name: %s", branch)
	}
	if _, err := git(path, "show-ref", "--verify", "--quiet", ref); err != nil {
		return "", fmt.Errorf("Branch no longer exists: %s", ref)
	}
	switch action {
	case "delete":
		return deleteBranch(path, branch, remote)
	case "diff":
		out, err := git(path, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "HEAD", ref, "--")
		if err == nil && out == "" {
			out = "No differences between HEAD and the target branch"
		}
		return out, err
	case "log":
		return git(path, "log", "-30", "--oneline", "--decorate", ref, "--")
	case "checkout", "merge", "rebase":
		if operationInProgress(path) {
			return "", fmt.Errorf("Finish the in-progress merge, rebase, or cherry-pick first")
		}
		status, err := git(path, "status", "--porcelain")
		if err != nil {
			return status, err
		}
		if status != "" {
			return "", fmt.Errorf("Skipped: working tree is not clean")
		}
	default:
		return "", fmt.Errorf("Unsupported operation: %s", action)
	}
	if action == "merge" {
		return git(path, "merge", "--no-edit", ref)
	}
	if action == "rebase" {
		return git(path, "rebase", ref)
	}
	if !remote {
		return git(path, "switch", "--no-guess", "--", branch)
	}
	_, local, ok := strings.Cut(branch, "/")
	if !ok || local == "" {
		return "", fmt.Errorf("Remote branch is missing a remote name: %s", branch)
	}
	if _, err := git(path, "show-ref", "--verify", "--quiet", "refs/heads/"+local); err == nil {
		upstream, err := git(path, "for-each-ref", "--format=%(upstream)", "refs/heads/"+local)
		if err != nil || upstream != ref {
			return "", fmt.Errorf("Skipped: local branch %s already exists but does not track %s", local, branch)
		}
		return git(path, "switch", "--no-guess", "--", local)
	}
	return git(path, "switch", "--track", "-c", local, ref)
}

type OperationResult struct {
	Repository Repository
	Output     string
	Err        error
}

func executeBranchBatch(targets []Repository, action, branch string, remote bool, reporters ...taskReporter) []OperationResult {
	results := make([]OperationResult, 0, len(targets))
	for i, r := range targets {
		if len(reporters) > 0 {
			reporters[0](action+" · "+r.Name, i, len(targets)*2, nil)
		}
		out, err := runBranchOperation(r.Path, action, branch, remote)
		if err != nil && (action == "merge" || action == "rebase") {
			out += "\nOn conflict, the operation remains in progress. Resolve and continue, or run git " + action + " --abort。"
		}
		results = append(results, OperationResult{Repository: r, Output: out, Err: err})
		if len(reporters) > 0 {
			reporters[0](action+" · "+r.Name, i+1, len(targets)*2, err)
		}
	}
	return results
}
func formatResults(results []OperationResult) string {
	var lines []string
	for _, r := range results {
		mark := "Succeeded"
		out := r.Output
		if r.Err != nil {
			mark = "Failed / Skipped"
			if isUncertainCommit(r.Err) || isUncertainBranchDelete(r.Err) {
				mark = "Unverified"
			}
			out += "\n" + r.Err.Error()
		}
		lines = append(lines, fmt.Sprintf("[%s] %s\n%s\n%s", mark, r.Repository.Name, r.Repository.Path, out))
	}
	return strings.Join(lines, "\n\n")
}

func runRepositoryOperation(path, action string) (string, error) {
	switch action {
	case "Refresh Status":
		return git(path, "status", "--short", "--branch")
	case "Fetch":
		return git(path, "fetch", "--all", "--prune")
	case "Pull":
		if operationInProgress(path) {
			return "", fmt.Errorf("Skipped: Git operation in progress")
		}
		status, err := git(path, "status", "--porcelain")
		if err != nil {
			return status, err
		}
		if status != "" {
			return "", fmt.Errorf("Skipped: working tree is not clean")
		}
		return git(path, "pull", "--ff-only")
	case "Diff · Working Tree Changes":
		return git(path, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "HEAD", "--")
	case "Log · Current Branch":
		return git(path, "log", "-30", "--oneline", "--decorate", "HEAD", "--")
	default:
		return "", fmt.Errorf("Unsupported repository operation: %s", action)
	}
}
