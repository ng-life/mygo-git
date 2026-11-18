package main

import (
	"fmt"
	"strings"
)

func verifyAmendedCommit(path, before, ref, expectedTree, message, output string, commandErr error, paths []string) (string, error) {
	fail := func(reason string) (string, error) { return output, &uncertainCommit{cause: reason} }
	head, currentRef := currentPosition(path)
	if currentRef != ref || head == "" {
		return fail("Cannot verify HEAD or the current branch after amend")
	}
	if head == before && commandErr != nil {
		return output, fmt.Errorf("Amend did not create a new commit: %w", commandErr)
	}
	oldParents, err := git(path, "show", "-s", "--format=%P", before, "--")
	if err != nil {
		return fail("Cannot read original commit parents")
	}
	newParents, err := git(path, "show", "-s", "--format=%P", head, "--")
	if err != nil || oldParents != newParents {
		return fail("Amend changed the parent commit")
	}
	body, err := rawGit(path, "show", "-s", "--format=%B", head, "--")
	if err != nil || strings.TrimRight(string(body), "\n") != strings.TrimRight(message, "\n") {
		return fail("Amended message does not match the request")
	}
	changed, err := rawGit(path, "diff", "--name-only", "-z", before, head, "--")
	if err != nil {
		return fail("Cannot verify amended file scope")
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[strings.TrimPrefix(p, ":(literal)")] = true
	}
	for _, p := range strings.Split(string(changed), "\x00") {
		if p != "" && !allowed[p] {
			return fail("Amend includes unselected files")
		}
	}
	if len(paths) > 0 {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--quiet", expectedTree, head, "--"}
		args = append(args, paths...)
		if _, err := git(path, args...); err != nil {
			return fail("Amended content does not match the staged snapshot")
		}
	}
	if commandErr != nil {
		output += "\nGit command failed: " + commandErr.Error()
	}
	return output + "\nAmend verified · Commit: " + head, nil
}
