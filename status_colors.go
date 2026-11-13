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
		repo.PushError = "无法读取未 Push 状态：" + err.Error()
		return
	}
	repo.Ahead, err = strconv.Atoi(count)
	if err != nil {
		repo.PushError = "无法解析未 Push 数量"
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
	text := repo.Path + "\n当前分支：" + repo.Branch
	if repo.Status != "" {
		text += "\n有未提交变更"
	} else {
		text += "\n工作区干净"
	}
	if repo.PushError != "" {
		return text + "\n" + repo.PushError
	}
	if repo.PushBase != "" {
		text += "\n相对 " + repo.PushBase
	} else {
		text += "\n未配置可用推送/上游引用，与全部远程跟踪引用比较"
	}
	return text + "\n未 Push：" + strconv.Itoa(repo.Ahead) + " 个提交（基于本地引用，Fetch 后更新）"
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
