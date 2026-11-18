package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Outgoing struct {
	Repository                                           Repository
	Head, Ref, Remote, Destination, Base, Message, Error string
	Commits                                              []Commit // oldest first; complete range, independent of the Log limit
}
type PushTarget struct {
	Source              Outgoing
	Remote, Destination string
}

func requireUnpublished(path, hash string) error {
	refs, err := git(path, "for-each-ref", "--contains", hash, "--format=%(refname)", "refs/remotes/")
	if err != nil {
		return err
	}
	if refs != "" {
		return fmt.Errorf("Commit is reachable from a remote-tracking ref. Fetch to verify; published commits are not rewritten")
	}
	return nil
}
func readOutgoing(ctx context.Context, repo Repository) Outgoing {
	result := Outgoing{Repository: repo}
	result.Head, result.Ref = currentPositionContext(ctx, repo.Path)
	if result.Head == "" {
		return result
	}
	copy := repo
	readPushStatusContext(ctx, &copy)
	result.Base = copy.PushBase
	if copy.PushError != "" {
		result.Error = copy.PushError
		return result
	}
	args := []string{"log", "--reverse", "--topo-order", "--format=%H%x00%ct%x00%an%x00%s%x00", "HEAD", "--not"}
	if result.Base != "" {
		args = append(args, result.Base)
	} else {
		args = append(args, "--remotes")
	}
	args = append(args, "--")
	data, err := gitContext(ctx, repo.Path, args...)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	fields := strings.Split(data, "\x00")
	for i := 0; i+3 < len(fields); i += 4 {
		stamp, err := strconv.ParseInt(fields[i+1], 10, 64)
		if err != nil {
			result.Error = "Cannot parse unpushed commits"
			return result
		}
		result.Commits = append(result.Commits, Commit{Repository: repo, Hash: strings.TrimSpace(fields[i]), Timestamp: stamp, Author: fields[i+2], Subject: fields[i+3]})
	}
	if result.Ref != "" {
		config, err := gitContext(ctx, repo.Path, "for-each-ref", "--format=%(push:remotename)%00%(push:remoteref)%00%(upstream:remotename)%00%(upstream:remoteref)", result.Ref)
		if err == nil {
			parts := strings.Split(config, "\x00")
			if len(parts) == 4 {
				result.Remote = parts[0]
				result.Destination = parts[1]
				if result.Remote == "" || result.Destination == "" {
					result.Remote = parts[2]
					result.Destination = parts[3]
				}
			}
		}
		if result.Remote == "" {
			result.Remote = "origin"
		}
		if result.Destination == "" {
			result.Destination = result.Ref
		}
	}
	body, err := rawGitContext(ctx, repo.Path, "show", "-s", "--format=%B", result.Head, "--")
	if err == nil {
		result.Message = strings.TrimRight(string(body), "\n")
	}
	return result
}
func readOutgoingRepositories(ctx context.Context, repos []Repository) []Outgoing {
	var out []Outgoing
	for _, repo := range repos {
		if ctx.Err() != nil {
			break
		}
		out = append(out, readOutgoing(ctx, repo))
	}
	return out
}
func pushOutgoing(target PushTarget, head string) (string, error) {
	source := target.Source
	currentHead, currentRef := currentPosition(source.Repository.Path)
	if currentHead != head || currentRef != source.Ref || currentRef == "" {
		return "", fmt.Errorf("HEAD or branch changed, or HEAD is detached. Refresh and retry")
	}
	remote := strings.TrimSpace(target.Remote)
	dest := strings.TrimSpace(target.Destination)
	if strings.HasPrefix(dest, "refs/") && !strings.HasPrefix(dest, "refs/heads/") {
		return "", fmt.Errorf("Target must be a branch name or refs/heads/ ref")
	}
	if !strings.HasPrefix(dest, "refs/heads/") {
		dest = "refs/heads/" + dest
	}
	if _, err := git(source.Repository.Path, "check-ref-format", dest); err != nil {
		return "", fmt.Errorf("Invalid target branch")
	}
	remotes, err := git(source.Repository.Path, "remote")
	if err != nil {
		return "", err
	}
	found := false
	for _, name := range strings.Fields(remotes) {
		if name == remote {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("Remote %s is not configured. Configure it and retry", remote)
	}
	if operationInProgress(source.Repository.Path) {
		return "", fmt.Errorf("Git operation in progress")
	}
	out, err := git(source.Repository.Path, "push", "--porcelain", "--", remote, head+":"+dest)
	if err != nil {
		return out, fmt.Errorf("Push failed; local commits are preserved: %w", err)
	}
	// Confirm against the actual push endpoint, which may differ from fetch URL.
	endpoint, err := git(source.Repository.Path, "remote", "get-url", "--push", remote)
	if err != nil {
		return out, fmt.Errorf("Unverified: cannot read push URL")
	}
	refs, err := git(source.Repository.Path, "ls-remote", "--refs", "--", endpoint, dest)
	if err != nil || len(strings.Fields(refs)) < 2 || strings.Fields(refs)[0] != head {
		return out, fmt.Errorf("Unverified: cannot confirm the remote branch points to this commit. Verify and refresh")
	}
	// Configure an upstream only for an exact conventional branch-to-branch push.
	if source.Base == "" && dest == source.Ref {
		branch := strings.TrimPrefix(source.Ref, "refs/heads/")
		configuredRemote, _ := git(source.Repository.Path, "config", "--get", "branch."+branch+".remote")
		configuredMerge, _ := git(source.Repository.Path, "config", "--get", "branch."+branch+".merge")
		if configuredRemote != "" || configuredMerge != "" {
			return out + "\nPush verified: " + head, nil
		}
		if _, err := git(source.Repository.Path, "config", "branch."+branch+".remote", remote); err != nil {
			return out, fmt.Errorf("Push succeeded, but saving upstream failed: %w", err)
		}
		if _, err := git(source.Repository.Path, "config", "branch."+branch+".merge", dest); err != nil {
			return out, fmt.Errorf("Push succeeded, but saving upstream failed: %w", err)
		}
	}
	return out + "\nPush verified: " + head + " → " + remote + "/" + strings.TrimPrefix(dest, "refs/heads/"), nil
}
