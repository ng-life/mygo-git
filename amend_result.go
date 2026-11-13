package main

import (
	"fmt"
	"strings"
)

func verifyAmendedCommit(path, before, ref, expectedTree, message, output string, commandErr error, paths []string) (string, error) {
	fail := func(reason string) (string, error) { return output, &uncertainCommit{cause: reason} }
	head, currentRef := currentPosition(path)
	if currentRef != ref || head == "" {
		return fail("Amend 后无法核对 HEAD 或当前分支")
	}
	if head == before && commandErr != nil {
		return output, fmt.Errorf("Amend 未产生新提交：%w", commandErr)
	}
	oldParents, err := git(path, "show", "-s", "--format=%P", before, "--")
	if err != nil {
		return fail("无法读取原提交父节点")
	}
	newParents, err := git(path, "show", "-s", "--format=%P", head, "--")
	if err != nil || oldParents != newParents {
		return fail("Amend 改变了父提交")
	}
	body, err := rawGit(path, "show", "-s", "--format=%B", head, "--")
	if err != nil || strings.TrimRight(string(body), "\n") != strings.TrimRight(message, "\n") {
		return fail("Amend message 与请求不一致")
	}
	changed, err := rawGit(path, "diff", "--name-only", "-z", before, head, "--")
	if err != nil {
		return fail("无法核对 Amend 文件范围")
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[strings.TrimPrefix(p, ":(literal)")] = true
	}
	for _, p := range strings.Split(string(changed), "\x00") {
		if p != "" && !allowed[p] {
			return fail("Amend 包含未选择文件")
		}
	}
	if len(paths) > 0 {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--quiet", expectedTree, head, "--"}
		args = append(args, paths...)
		if _, err := git(path, args...); err != nil {
			return fail("Amend 内容与暂存快照不一致")
		}
	}
	if commandErr != nil {
		output += "\nGit 命令异常：" + commandErr.Error()
	}
	return output + "\n已核对 Amend 成功 · Commit: " + head, nil
}
