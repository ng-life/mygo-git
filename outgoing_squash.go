package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type rewriteCommit struct{ Hash, Tree, Parent, Name, Email, Date, Message string }

func readRewriteCommit(path, hash string) (rewriteCommit, error) {
	result := rewriteCommit{Hash: hash}
	raw, err := rawGit(path, "show", "-s", "--format=%T%x00%P%x00%an%x00%ae%x00%aI%x00%B", hash, "--")
	if err != nil {
		return result, err
	}
	fields := strings.SplitN(string(raw), "\x00", 6)
	if len(fields) != 6 {
		return result, fmt.Errorf("无法读取提交元数据")
	}
	parents := strings.Fields(fields[1])
	if len(parents) > 1 {
		return result, fmt.Errorf("暂不合并包含 merge commit 的历史")
	}
	result.Tree = fields[0]
	if len(parents) == 1 {
		result.Parent = parents[0]
	}
	result.Name = fields[2]
	result.Email = fields[3]
	result.Date = fields[4]
	result.Message = strings.TrimRight(fields[5], "\n")
	return result, nil
}
func squashOutgoing(source Outgoing, selected []Commit, message string) (string, error) {
	path := source.Repository.Path
	if len(selected) < 2 || strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("至少选择两个未 Push 提交并填写 message")
	}
	head, ref := currentPosition(path)
	if head != source.Head || ref != source.Ref || ref == "" {
		return "", fmt.Errorf("HEAD 或分支已变化，或处于 detached HEAD；请刷新")
	}
	if operationInProgress(path) {
		return "", fmt.Errorf("存在未完成的 Git 操作")
	}
	clean := func() error {
		status, err := git(path, "status", "--porcelain")
		if err != nil {
			return err
		}
		if status != "" {
			return fmt.Errorf("合并提交需要干净工作区；暂存及未提交变更不会自动丢弃")
		}
		return nil
	}
	if err := clean(); err != nil {
		return "", err
	}
	fresh := readOutgoing(context.Background(), source.Repository)
	if fresh.Error != "" {
		return "", fmt.Errorf("%s", fresh.Error)
	}
	outgoing := map[string]bool{}
	for _, c := range fresh.Commits {
		outgoing[c.Hash] = true
	}
	chosen := map[string]bool{}
	for _, c := range selected {
		if c.Repository.Path != path || !outgoing[c.Hash] {
			return "", fmt.Errorf("只能合并同一仓库的未 Push 提交")
		}
		chosen[c.Hash] = true
	}
	if len(chosen) < 2 {
		return "", fmt.Errorf("至少选择两个不同提交")
	}
	chain, err := git(path, "rev-list", "--first-parent", "--reverse", head, "--")
	if err != nil {
		return "", err
	}
	hashes := strings.Fields(chain)
	first := -1
	found := 0
	for i, hash := range hashes {
		if chosen[hash] {
			if first < 0 {
				first = i
			}
			found++
		}
	}
	if found != len(chosen) {
		return "", fmt.Errorf("所选提交不在当前分支的线性历史上")
	}
	var plan []rewriteCommit
	for _, hash := range hashes[first:] {
		if !outgoing[hash] {
			return "", fmt.Errorf("重写范围包含已 Push 提交")
		}
		if err := requireUnpublished(path, hash); err != nil {
			return "", err
		}
		commit, err := readRewriteCommit(path, hash)
		if err != nil {
			return "", err
		}
		plan = append(plan, commit)
	}
	original, err := readRewriteCommit(path, head)
	if err != nil {
		return "", err
	}
	temp, err := os.MkdirTemp("", "mygo-squash-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	index := filepath.Join(temp, "index")
	env := map[string]string{"GIT_INDEX_FILE": index}
	base := plan[0].Parent
	baseTree := base
	if baseTree == "" {
		baseTree, err = git(path, "hash-object", "-w", "-t", "tree", "--stdin")
		if err != nil {
			return "", err
		}
	}
	run := func(args ...string) (string, error) {
		out, err := commandGitEnv(context.Background(), 90*time.Second, path, env, args...)
		return strings.TrimSpace(string(out)), err
	}
	if _, err = run("read-tree", baseTree); err != nil {
		return "", err
	}
	apply := func(commit rewriteCommit) error {
		parent := commit.Parent
		if parent == "" {
			parent = baseTree
		}
		patch, err := rawGit(path, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", parent, commit.Hash, "--")
		if err != nil {
			return err
		}
		if len(patch) == 0 {
			return nil
		}
		file := filepath.Join(temp, "change.patch")
		if err = os.WriteFile(file, patch, 0600); err != nil {
			return err
		}
		if _, err = run("apply", "--cached", "--3way", "--allow-empty", "--whitespace=nowarn", file); err != nil {
			return fmt.Errorf("重排提交产生冲突；原分支、暂存区及文件未改动：%w", err)
		}
		return nil
	}
	for _, commit := range plan {
		if chosen[commit.Hash] {
			if err = apply(commit); err != nil {
				return "", err
			}
		}
	}
	msgFile := filepath.Join(temp, "message.txt")
	if err = os.WriteFile(msgFile, []byte(message), 0600); err != nil {
		return "", err
	}
	if _, err = run("hook", "run", "--ignore-missing", "commit-msg", "--", msgFile); err != nil {
		return "", fmt.Errorf("commit-msg hook 拒绝合并：%w", err)
	}
	checkedMessage, err := os.ReadFile(msgFile)
	if err != nil {
		return "", err
	}
	signed, _ := git(path, "config", "--bool", "commit.gpgsign")
	makeCommit := func(author rewriteCommit, parent, message string) (string, error) {
		tree, err := run("write-tree")
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(msgFile, []byte(message), 0600); err != nil {
			return "", err
		}
		args := []string{"commit-tree", tree, "-F", msgFile}
		if parent != "" {
			args = append(args, "-p", parent)
		}
		if signed == "true" {
			args = append(args, "-S")
		}
		overrides := map[string]string{"GIT_INDEX_FILE": index, "GIT_AUTHOR_NAME": author.Name, "GIT_AUTHOR_EMAIL": author.Email, "GIT_AUTHOR_DATE": author.Date}
		out, err := commandGitEnv(context.Background(), 90*time.Second, path, overrides, args...)
		if err != nil {
			return "", err
		}
		hash := strings.TrimSpace(string(out))
		actual, err := readRewriteCommit(path, hash)
		if err != nil || actual.Tree != tree || actual.Parent != parent || actual.Message != strings.TrimRight(message, "\n") {
			return "", fmt.Errorf("无法核对重写的提交")
		}
		return hash, nil
	}
	next, err := makeCommit(plan[0], base, string(checkedMessage))
	if err != nil {
		return "", err
	}
	squashed := next
	for _, commit := range plan {
		if !chosen[commit.Hash] {
			if err = apply(commit); err != nil {
				return "", err
			}
			next, err = makeCommit(commit, next, commit.Message)
			if err != nil {
				return "", err
			}
		}
	}
	final, err := readRewriteCommit(path, next)
	if err != nil || final.Tree != original.Tree {
		return "", fmt.Errorf("重排后的最终文件树与原 HEAD 不一致；原分支未改动")
	}
	if err = clean(); err != nil {
		return "", err
	}
	now, currentRef := currentPosition(path)
	if now != head || currentRef != ref {
		return "", fmt.Errorf("合并期间分支已变化；未更新原分支")
	}
	if out, err := git(path, "update-ref", "-m", "mygo-git: squash unpublished commits", ref, next, head); err != nil {
		return out, err
	}
	return fmt.Sprintf("已合并 %d 个提交为 %s\n原 HEAD: %s\n新 HEAD: %s\n最终文件树保持不变；可通过分支 reflog 找回原历史", len(chosen), squashed, head, next), nil
}
