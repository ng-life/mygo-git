package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type uncertainCommit struct{ cause string }

func (e *uncertainCommit) Error() string {
	return "Unverified: " + e.cause + "; verify repository history before committing again"
}
func isUncertainCommit(err error) bool { var target *uncertainCommit; return errors.As(err, &target) }

// Reconcile independently after git exits: an error may follow a successful ref update.
func verifyCommit(path, before, ref, expectedTree, message, output string, commandErr error, paths []string) (string, error) {
	uncertain := func(reason string) (string, error) { return output, &uncertainCommit{cause: reason} }
	head, err := git(path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		// An unborn branch legitimately has no HEAD object after a rejected first commit.
		currentRef, refErr := git(path, "symbolic-ref", "--quiet", "HEAD")
		if before == "" && commandErr != nil && refErr == nil && currentRef == ref {
			_, missingErr := git(path, "show-ref", "--verify", "--quiet", ref)
			var exitErr *exec.ExitError
			if errors.As(missingErr, &exitErr) && exitErr.ExitCode() == 1 {
				return output, fmt.Errorf("Verified no initial commit was created: commit failed: %w", commandErr)
			}
		}
		return uncertain("Cannot read HEAD after commit")
	}
	currentRef, refErr := git(path, "symbolic-ref", "--quiet", "HEAD")
	if ref == "" {
		if refErr == nil {
			return uncertain("Current branch changed")
		}
	} else if refErr != nil || currentRef != ref {
		return uncertain("Current branch changed")
	}
	if head == before {
		if commandErr != nil {
			return output, fmt.Errorf("Verified unchanged HEAD: commit failed; staged state preserved: %w", commandErr)
		}
		return uncertain("Git reported success, but HEAD did not change")
	}
	output += "\nCandidate commit: " + head
	parents, err := git(path, "rev-list", "--parents", "-n", "1", head, "--")
	fields := strings.Fields(parents)
	if err != nil || len(fields) != 1+boolInt(before != "") || (before != "" && fields[1] != before) {
		return output, &uncertainCommit{cause: "New commit parent does not match the original HEAD"}
	}
	actual, err := rawGit(path, "show", "-s", "--format=%B", head, "--")
	if err != nil || strings.TrimRight(string(actual), "\n") != strings.TrimRight(message, "\n") {
		return output, &uncertainCommit{cause: "New commit metadata does not match the request"}
	}
	detail, err := readCommit(Commit{Repository: Repository{Path: path}, Hash: head})
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[strings.TrimPrefix(p, ":(literal)")] = true
	}
	if err != nil || len(detail.Files) == 0 {
		return output, &uncertainCommit{cause: "Cannot verify changed files in the new commit"}
	}
	for _, file := range detail.Files {
		if !allowed[file.Path] || (file.OldPath != "" && !allowed[file.OldPath]) {
			return output, &uncertainCommit{cause: "New commit contains unselected files"}
		}
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--quiet", expectedTree, head, "--"}
	args = append(args, paths...)
	if expectedTree == "" {
		return output, &uncertainCommit{cause: "Failed to capture file snapshot before commit"}
	}
	if _, err := git(path, args...); err != nil {
		return output, &uncertainCommit{cause: "New commit content does not match the staged snapshot"}
	}
	if commandErr != nil {
		output += "\nGit command failed: " + commandErr.Error()
	}
	return output + "\nCommit verified · Commit: " + head, nil
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
