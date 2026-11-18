package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type uncertainBranchDelete struct{ cause error }

func (e *uncertainBranchDelete) Error() string {
	return "Deletion unverified: cannot verify the remote branch. Fetch and check: " + e.cause.Error()
}
func isUncertainBranchDelete(err error) bool {
	var target *uncertainBranchDelete
	return errors.As(err, &target)
}

// A grouped branch is protected if any associated repository has it checked out.
func currentLocalBranch(branch string, targets []Repository) bool {
	for _, repo := range targets {
		if branch != "" && repo.Branch == branch {
			return true
		}
	}
	return false
}

func deleteBranch(path, branch string, remote bool) (string, error) {
	if !remote {
		current, err := git(path, "symbolic-ref", "--quiet", "HEAD")
		if err == nil && current == fullBranchRef(branch, false) {
			return "", fmt.Errorf("Cannot delete the current branch: %s", branch)
		}
		if operationInProgress(path) {
			return "", fmt.Errorf("Finish the in-progress Git operation before deleting branches")
		}
		// Git also protects branches checked out in linked worktrees.
		out, err := git(path, "branch", "-d", "--", branch)
		if err != nil {
			return out, fmt.Errorf("Safe deletion failed. Current, checked-out, or unmerged branches are not force-deleted: %w", err)
		}
		return out, nil
	}
	remoteName, name, ok := strings.Cut(branch, "/")
	if !ok || remoteName == "" || name == "" {
		return "", fmt.Errorf("Remote branch is missing a remote name: %s", branch)
	}
	remotes, err := git(path, "remote")
	if err != nil {
		return "", err
	}
	configured := false
	for _, candidate := range strings.Split(remotes, "\n") {
		if candidate == remoteName {
			configured = true
		}
	}
	if !configured {
		return "", fmt.Errorf("Remote %s no longer exists", remoteName)
	}
	// A tracking ref describes the fetch endpoint; pushurl may point to a different fork.
	endpoint, err := git(path, "remote", "get-url", "--", remoteName)
	if err != nil || endpoint == "" {
		return "", fmt.Errorf("Cannot read URL for remote %s: %v", remoteName, err)
	}
	ref := fullBranchRef(branch, true)
	if _, err := git(path, "symbolic-ref", "--quiet", ref); err == nil {
		return "", fmt.Errorf("Cannot delete symbolic ref as a remote branch: %s", branch)
	}
	target := "refs/heads/" + name
	expected, err := git(path, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	// An explicit lease prevents deleting a branch advanced since the last fetch.
	out, pushErr := git(path, "push", "--porcelain", "--force-with-lease="+target+":"+expected, "--", endpoint, ":"+target)
	remaining, verifyErr := git(path, "ls-remote", "--refs", "--", endpoint, target)
	if verifyErr != nil {
		return out, &uncertainBranchDelete{cause: verifyErr}
	}
	exists := false
	for _, line := range strings.Split(remaining, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == target {
			exists = true
		}
	}
	if exists {
		if pushErr != nil {
			return out, fmt.Errorf("Remote branch still exists. Fetch and check for new remote commits: %w", pushErr)
		}
		return out, fmt.Errorf("Remote branch still exists; deletion failed or the branch was recreated")
	}
	// Only remove the tracking ref if another fetch has not replaced it.
	if _, err := git(path, "update-ref", "-d", ref, expected); err != nil {
		out += "\nRemote branch deleted, but the local tracking ref changed. Fetch to verify"
	}
	if pushErr != nil {
		out += "\nVerified after command failure: remote branch does not exist"
	}
	return out + "\nRemote branch deletion verified: " + remoteName + "/" + name, nil
}

type branchDeletion struct {
	Open, Remote bool
	Branch       string
	Targets      []Repository
}

func (a *client) requestBranchDeletion(branch string, remote bool, targets []Repository) {
	if a.busy || len(targets) == 0 || (!remote && currentLocalBranch(branch, targets)) {
		return
	}
	a.branchDeletion = branchDeletion{Open: true, Remote: remote, Branch: branch, Targets: append([]Repository(nil), targets...)}
}
func (a *client) branchDeletionPanel(c *ui.Context) {
	if !a.branchDeletion.Open {
		return
	}
	ui.Modal(c.Key("delete-branch-confirmation"), &a.branchDeletion.Open, func() {
		ui.Column(c).Width(620).MaxHeight(500).Gap(12).Children(func() {
			pending := a.branchDeletion
			kind := "Local"
			if pending.Remote {
				kind = "Remote"
			}
			ui.Text(c, "Delete "+kind+" Branch").FontSize(16).Bold()
			ui.Text(c, pending.Branch).Font("monospace").Selectable()
			message := "Safely delete merged local branches. Skip the current branch and branches used by other worktrees."
			if pending.Remote {
				message = "Delete the branch on the remote, preserving local branches. Deletion is rejected if the remote branch has changed."
			}
			ui.Text(c, message)
			ui.Textf(c, "Affects %d repositories:", len(pending.Targets))
			ui.Scroll(c).MaxHeight(200).Children(func() {
				for _, repo := range pending.Targets {
					ui.Text(c, repo.Name+"\n"+repo.Path).FontSize(12)
				}
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { a.branchDeletion.Open = false })
				ui.Button(c, "Delete "+kind+" Branch").Label("Confirm Delete " + kind + " Branch").Disabled(a.busy).OnClick(func() {
					a.branchDeletion.Open = false
					a.showOutput = true
					a.runBranchAction("delete", pending.Branch, pending.Remote, pending.Targets)
				})
			})
		})
	})
}
