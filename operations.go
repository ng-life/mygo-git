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
		return "", fmt.Errorf("无效分支名：%s", branch)
	}
	if _, err := git(path, "show-ref", "--verify", "--quiet", ref); err != nil {
		return "", fmt.Errorf("分支已不存在：%s", ref)
	}
	switch action {
	case "delete":
		return deleteBranch(path, branch, remote)
	case "diff":
		out, err := git(path, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "HEAD", ref, "--")
		if err == nil && out == "" {
			out = "当前 HEAD 与目标分支无差异"
		}
		return out, err
	case "log":
		return git(path, "log", "-30", "--oneline", "--decorate", ref, "--")
	case "checkout", "merge", "rebase":
		if operationInProgress(path) {
			return "", fmt.Errorf("存在未完成的合并、变基或拣选操作，请先处理")
		}
		status, err := git(path, "status", "--porcelain")
		if err != nil {
			return status, err
		}
		if status != "" {
			return "", fmt.Errorf("工作区不干净，已跳过")
		}
	default:
		return "", fmt.Errorf("不支持的操作：%s", action)
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
		return "", fmt.Errorf("远程分支缺少远程名称：%s", branch)
	}
	if _, err := git(path, "show-ref", "--verify", "--quiet", "refs/heads/"+local); err == nil {
		upstream, err := git(path, "for-each-ref", "--format=%(upstream)", "refs/heads/"+local)
		if err != nil || upstream != ref {
			return "", fmt.Errorf("本地分支 %s 已存在，但未跟踪 %s，已跳过", local, branch)
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
			out += "\n若发生冲突，操作状态保留；请解决后继续，或在仓库执行 git " + action + " --abort。"
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
		mark := "成功"
		out := r.Output
		if r.Err != nil {
			mark = "失败 / 跳过"
			if isUncertainCommit(r.Err) || isUncertainBranchDelete(r.Err) {
				mark = "结果待确认"
			}
			out += "\n" + r.Err.Error()
		}
		lines = append(lines, fmt.Sprintf("[%s] %s\n%s\n%s", mark, r.Repository.Name, r.Repository.Path, out))
	}
	return strings.Join(lines, "\n\n")
}

func runRepositoryOperation(path, action string) (string, error) {
	switch action {
	case "刷新状态":
		return git(path, "status", "--short", "--branch")
	case "Fetch":
		return git(path, "fetch", "--all", "--prune")
	case "Pull":
		if operationInProgress(path) {
			return "", fmt.Errorf("存在未完成的 Git 操作，已跳过")
		}
		status, err := git(path, "status", "--porcelain")
		if err != nil {
			return status, err
		}
		if status != "" {
			return "", fmt.Errorf("工作区不干净，已跳过")
		}
		return git(path, "pull", "--ff-only")
	case "diff · 工作区修改":
		return git(path, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "HEAD", "--")
	case "log · 当前分支提交":
		return git(path, "log", "-30", "--oneline", "--decorate", "HEAD", "--")
	default:
		return "", fmt.Errorf("不支持的仓库操作：%s", action)
	}
}
