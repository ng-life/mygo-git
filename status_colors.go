package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

var (
	statusBlue   = ui.RGB(104, 151, 187)
	statusOrange = ui.RGB(232, 164, 108)
	statusGreen  = ui.RGB(106, 168, 79)
)

// Remote tracking refs are local snapshots. Inspect never fetches or prompts.
func readPushStatus(repo *Repository) { readPushStatusContext(context.Background(), repo) }
func readPushStatusContext(ctx context.Context, repo *Repository) {
	repo.Ahead = 0
	repo.PushBase = ""
	repo.PushError = ""
	if _, err := gitContext(ctx, repo.Path, "rev-parse", "--verify", "HEAD"); err != nil {
		return
	}
	for _, ref := range []string{"@{push}", "@{upstream}"} {
		base, err := gitContext(ctx, repo.Path, "rev-parse", "--symbolic-full-name", "--verify", ref)
		if err == nil && base != "" {
			repo.PushBase = base
			break
		}
	}
	args := []string{"rev-list", "--count", "HEAD", "--not"}
	if repo.PushBase != "" {
		args = append(args, repo.PushBase)
	} else {
		args = append(args, "--remotes")
	}
	count, err := gitContext(ctx, repo.Path, args...)
	if err != nil {
		repo.PushError = "Cannot read unpushed status: " + err.Error()
		return
	}
	repo.Ahead, err = strconv.Atoi(count)
	if err != nil {
		repo.PushError = "Cannot parse unpushed commit count"
	}
}
func repositoryStateColor(repo Repository, normal ui.Color) ui.Color {
	if repo.Status != "" {
		return statusOrange
	}
	if repo.Ahead > 0 {
		return statusBlue
	}
	return normal
}
func repositoryStateTooltip(repo Repository) string {
	text := repo.Path + "\nCurrent branch: " + repo.Branch
	if repo.Status != "" {
		text += "\nUncommitted changes"
	} else {
		text += "\nWorking tree clean"
	}
	if repo.PushError != "" {
		return text + "\n" + repo.PushError
	}
	if repo.PushBase != "" {
		text += "\nCompared with " + repo.PushBase
	} else {
		text += "\nNo push or upstream ref configured; comparing against all remote-tracking refs"
	}
	return text + "\nUnpushed: " + strconv.Itoa(repo.Ahead) + " commits (based on local refs; Fetch to update)"
}
func workingChangeKind(file WorkingFile) string {
	if file.Change != "" && !file.Conflicted() {
		file.Status = file.Change
	}
	if file.Conflicted() {
		return "conflicted"
	}
	if strings.Contains(file.Status, "D") && !strings.Contains(file.Status, "?") {
		return "deleted"
	}
	if strings.Contains(file.Status, "?") && !strings.Contains(file.Status, "D") || strings.ContainsAny(file.Status, "AC") {
		return "added"
	}
	return "modified"
}
func workingStatusColor(file WorkingFile, muted ui.Color) ui.Color {
	switch workingChangeKind(file) {
	case "conflicted":
		return statusOrange
	case "deleted":
		return muted
	case "added":
		return statusGreen
	default:
		return statusBlue
	}
}
