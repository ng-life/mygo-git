package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type uncertainCommit struct{ cause string }

func (e *uncertainCommit) Error() string {
	return "结果待确认：" + e.cause + "；请核对仓库历史后再提交"
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
				return output, fmt.Errorf("已核对首次提交未创建，Commit 失败：%w", commandErr)
			}
		}
		return uncertain("无法读取提交后的 HEAD")
	}
	currentRef, refErr := git(path, "symbolic-ref", "--quiet", "HEAD")
	if ref == "" {
		if refErr == nil {
			return uncertain("当前分支已变化")
		}
	} else if refErr != nil || currentRef != ref {
		return uncertain("当前分支已变化")
	}
	if head == before {
		if commandErr != nil {
			return output, fmt.Errorf("已核对 HEAD 未变化，Commit 失败；暂存状态已保留：%w", commandErr)
		}
		return uncertain("Git 返回成功，但 HEAD 未变化")
	}
	output += "\n候选 Commit: " + head
	parents, err := git(path, "rev-list", "--parents", "-n", "1", head, "--")
	fields := strings.Fields(parents)
	if err != nil || len(fields) != 1+boolInt(before != "") || (before != "" && fields[1] != before) {
		return output, &uncertainCommit{cause: "新提交的父提交与原 HEAD 不一致"}
	}
	actual, err := rawGit(path, "show", "-s", "--format=%B", head, "--")
	if err != nil || strings.TrimRight(string(actual), "\n") != strings.TrimRight(message, "\n") {
		return output, &uncertainCommit{cause: "新提交的信息与本次提交不一致"}
	}
	detail, err := readCommit(Commit{Repository: Repository{Path: path}, Hash: head})
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[strings.TrimPrefix(p, ":(literal)")] = true
	}
	if err != nil || len(detail.Files) == 0 {
		return output, &uncertainCommit{cause: "无法核对新提交的变更文件"}
	}
	for _, file := range detail.Files {
		if !allowed[file.Path] || (file.OldPath != "" && !allowed[file.OldPath]) {
			return output, &uncertainCommit{cause: "新提交包含本次选择以外的文件"}
		}
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--quiet", expectedTree, head, "--"}
	args = append(args, paths...)
	if expectedTree == "" {
		return output, &uncertainCommit{cause: "未能记录提交前的文件快照"}
	}
	if _, err := git(path, args...); err != nil {
		return output, &uncertainCommit{cause: "新提交的文件内容与本次暂存快照不一致"}
	}
	if commandErr != nil {
		output += "\nGit 命令异常：" + commandErr.Error()
	}
	return output + "\n已核对成功 · Commit: " + head, nil
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
