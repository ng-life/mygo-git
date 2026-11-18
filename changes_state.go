package main

import (
	"context"
	"fmt"
	"strings"
)

type changesDialogState struct {
	Open                 bool
	Kind, Message, Error string
	Requests             []CommitRequest
	PushTargets          []PushTarget
	Source               Outgoing
	Commits              []Commit
}

func (a *client) updateOutgoing(updated []Outgoing) {
	previous := map[string]Outgoing{}
	for _, source := range a.outgoing {
		previous[source.Repository.Path] = source
	}
	if a.outgoingSelection == nil {
		a.outgoingSelection = map[string]bool{}
	}
	for _, source := range updated {
		if old, ok := previous[source.Repository.Path]; ok && old.Ref != source.Ref {
			for _, commit := range old.Commits {
				delete(a.outgoingSelection, commit.Key())
			}
		}
		previous[source.Repository.Path] = source
	}
	a.outgoing = nil
	valid := map[string]bool{}
	for _, repo := range a.repos {
		if source, ok := previous[repo.Path]; ok {
			a.outgoing = append(a.outgoing, source)
			for _, commit := range source.Commits {
				valid[commit.Key()] = true
			}
		}
	}
	for key := range a.outgoingSelection {
		if !valid[key] {
			delete(a.outgoingSelection, key)
		}
	}
}
func (a *client) outgoingFor(repository Repository) Outgoing {
	for _, source := range a.outgoing {
		if source.Repository.Path == repository.Path {
			return source
		}
	}
	for _, tree := range a.workingTrees {
		if tree.Repository.Path == repository.Path {
			return Outgoing{Repository: repository, Head: tree.Head, Ref: tree.Ref, Remote: "origin", Destination: tree.Ref}
		}
	}
	return Outgoing{Repository: repository, Remote: "origin", Destination: "refs/heads/" + repository.Branch}
}
func (a *client) selectedOutgoing(source Outgoing) []Commit {
	var commits []Commit
	for _, commit := range source.Commits {
		if a.outgoingSelection[commit.Key()] {
			commits = append(commits, commit)
		}
	}
	return commits
}
func (a *client) openChangesPush(sources []Outgoing) {
	dialog := changesDialogState{Open: true, Kind: "push"}
	for _, source := range sources {
		if len(source.Commits) > 0 {
			dialog.PushTargets = append(dialog.PushTargets, PushTarget{Source: source, Remote: source.Remote, Destination: strings.TrimPrefix(source.Destination, "refs/heads/")})
		}
	}
	if len(dialog.PushTargets) > 0 {
		a.changesDialog = dialog
	}
}
func (a *client) openCommitPush(onlyRepository string) {
	if a.busy || a.workingLoading {
		return
	}
	requests := a.workingRequests(onlyRepository)
	if len(requests) == 0 {
		return
	}
	dialog := changesDialogState{Open: true, Kind: "commit-push", Message: a.commitMessage, Requests: requests}
	for _, request := range requests {
		source := a.outgoingFor(request.Tree.Repository)
		dialog.PushTargets = append(dialog.PushTargets, PushTarget{Source: source, Remote: source.Remote, Destination: strings.TrimPrefix(source.Destination, "refs/heads/")})
	}
	a.changesDialog = dialog
}
func (a *client) openChangesRevert(requests []CommitRequest) {
	if !a.busy && len(requests) > 0 {
		a.changesDialog = changesDialogState{Open: true, Kind: "revert", Requests: requests}
	}
}
func (a *client) openChangesSquash(source Outgoing) {
	commits := a.selectedOutgoing(source)
	if len(commits) < 2 {
		return
	}
	var subjects []string
	for _, commit := range commits {
		subjects = append(subjects, commit.Subject)
	}
	a.changesDialog = changesDialogState{Open: true, Kind: "squash", Source: source, Commits: commits, Message: strings.Join(subjects, "\n\n")}
}
func (a *client) openChangesAmend(source Outgoing) {
	for _, tree := range a.workingTrees {
		if tree.Repository.Path == source.Repository.Path {
			a.changesDialog = changesDialogState{Open: true, Kind: "amend", Source: source, Message: source.Message, Requests: []CommitRequest{{Tree: tree, Amend: true}}}
			return
		}
	}
}
func verifiedCommitHash(output string) string {
	index := strings.LastIndex(output, " · Commit: ")
	if index < 0 {
		return ""
	}
	fields := strings.Fields(output[index+len(" · Commit: "):])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
func (a *client) beginWorkingCommit(requests []CommitRequest, message string, pushTargets []PushTarget) {
	if a.busy || len(requests) == 0 {
		return
	}
	targets := make([]Repository, 0, len(requests))
	for _, request := range requests {
		targets = append(targets, request.Tree.Repository)
	}
	retryDialog := a.changesDialog
	a.workingResults = nil
	a.showOutput = true
	a.jobProgress(fmt.Sprintf("Commit · %d repositories", len(requests)), func(report taskReporter) (func(), error) {
		results := executeWorkingCommits(requests, message, report)
		committed := map[string]bool{}
		pushes := map[string]PushTarget{}
		for _, target := range pushTargets {
			pushes[target.Source.Repository.Path] = target
		}
		for i := range results {
			report("Verify / Push · "+results[i].Repository.Name, len(requests)+i, len(requests)*3, nil)
			result := &results[i]
			if result.Err != nil {
				report("Verify / Push · "+result.Repository.Name, len(requests)+i+1, len(requests)*3, result.Err)
				continue
			}
			committed[result.Repository.Path] = true
			if target, ok := pushes[result.Repository.Path]; ok {
				hash := verifiedCommitHash(result.Output)
				if hash == "" {
					result.Err = fmt.Errorf("Commit succeeded, but the verified SHA is unavailable. Push was not started")
					report("Verify / Push · "+result.Repository.Name, len(requests)+i+1, len(requests)*3, result.Err)
					continue
				}
				out, err := pushOutgoing(target, hash)
				result.Output += "\n" + out
				if err != nil {
					result.Err = fmt.Errorf("Commit succeeded; %w", err)
				}
			}
			report("Verify / Push · "+result.Repository.Name, len(requests)+i+1, len(requests)*3, result.Err)
		}
		repos := inspectWithProgress(entriesFor(targets), report, len(requests)*2, len(requests)*3)
		return func() {
			a.mergeRepositories(repos)
			a.workingResults = results
			a.output = "Commit\n\n" + formatResults(results)
			if len(committed) > 0 {
				a.changelists.Messages = uniqueCommitMessages(append([]string{message}, a.changelists.Messages...))
			}
			allCommitted := true
			for _, request := range requests {
				if !committed[request.Tree.Repository.Path] {
					allCommitted = false
					continue
				}
				for _, file := range request.Files {
					delete(a.workingSelection, workingFileKey(request.Tree.Repository.Path, file.Path))
				}
			}
			if !allCommitted && retryDialog.Kind == "amend" && len(requests) == 1 && requests[0].Amend && len(requests[0].Files) == 0 && len(results) == 1 && !isUncertainCommit(results[0].Err) {
				retryDialog.Open = true
				retryDialog.Error = results[0].Err.Error()
				a.changesDialog = retryDialog
			}
			if allCommitted && len(a.workingRequests("")) == 0 {
				a.commitMessage = ""
				a.workingAmend = false
			}
			a.persistChangelists()
		}, nil
	})
}
func (a *client) applyChangesDialog() {
	if a.busy {
		return
	}
	dialog := a.changesDialog
	a.changesDialog.Open = false
	if dialog.Kind == "commit-push" || dialog.Kind == "amend" {
		if dialog.Kind == "commit-push" {
			a.commitMessage = dialog.Message
		}
		a.beginWorkingCommit(dialog.Requests, dialog.Message, dialog.PushTargets)
		return
	}
	a.jobProgress("Changes · "+dialog.Kind, func(report taskReporter) (func(), error) {
		var results []OperationResult
		var targets []Repository
		switch dialog.Kind {
		case "revert":
			for i, request := range dialog.Requests {
				report("Revert · "+request.Tree.Repository.Name, i, len(dialog.Requests)*2, nil)
				out, err := revertWorkingFiles(request)
				targets = append(targets, request.Tree.Repository)
				results = append(results, OperationResult{Repository: request.Tree.Repository, Output: out, Err: err})
				report("Revert · "+request.Tree.Repository.Name, i+1, len(dialog.Requests)*2, err)
			}
		case "push":
			for i, target := range dialog.PushTargets {
				report("Push · "+target.Source.Repository.Name, i, len(dialog.PushTargets)*2, nil)
				out, err := pushOutgoing(target, target.Source.Head)
				targets = append(targets, target.Source.Repository)
				results = append(results, OperationResult{Repository: target.Source.Repository, Output: out, Err: err})
				report("Push · "+target.Source.Repository.Name, i+1, len(dialog.PushTargets)*2, err)
			}
		case "squash":
			report("Squash commits · "+dialog.Source.Repository.Name, 0, 2, nil)
			out, err := squashOutgoing(dialog.Source, dialog.Commits, dialog.Message)
			targets = []Repository{dialog.Source.Repository}
			results = []OperationResult{{Repository: dialog.Source.Repository, Output: out, Err: err}}
			report("Squash commits · "+dialog.Source.Repository.Name, 1, 2, err)
		}
		repos := inspectWithProgress(entriesFor(targets), report, len(targets), len(targets)*2)
		return func() { a.mergeRepositories(repos); a.output = formatResults(results); a.showOutput = true }, nil
	})
}
func (a *client) loadAmendMessage() {
	if !a.workingAmend || a.commitMessage != "" {
		return
	}
	requests := a.workingRequests("")
	if len(requests) != 1 {
		return
	}
	tree := requests[0].Tree
	a.startRead("amend-message", func(ctx context.Context) func() {
		body, err := rawGitContext(ctx, tree.Repository.Path, "show", "-s", "--format=%B", tree.Head, "--")
		return func() {
			current := a.workingRequests("")
			matches := len(current) == 1 && current[0].Tree.Repository.Path == tree.Repository.Path && current[0].Tree.Head == tree.Head
			if err == nil && a.workingAmend && a.commitMessage == "" && matches {
				a.commitMessage = strings.TrimRight(string(body), "\n")
				a.persistChangelists()
			}
		}
	})
}
