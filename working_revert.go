package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cheap filesystem snapshot prevents discarding edits made after a refresh.
func workingFileStamp(repository string, file WorkingFile) string {
	var result []string
	for _, path := range []string{file.Path, file.OldPath} {
		if path == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(repository, filepath.FromSlash(path)))
		if err != nil {
			result = append(result, path+":"+err.Error())
			continue
		}
		result = append(result, fmt.Sprintf("%s:%d:%d:%d", path, info.Mode(), info.Size(), info.ModTime().UnixNano()))
	}
	return strings.Join(result, "\x00")
}
func revertWorkingFiles(request CommitRequest) (string, error) {
	tree := request.Tree
	path := tree.Repository.Path
	if operationInProgress(path) {
		return "", fmt.Errorf("存在未完成的 Git 操作，请先处理")
	}
	current := readWorkingTree(tree.Repository)
	if current.Error != "" {
		return "", fmt.Errorf("%s", current.Error)
	}
	if current.Head != tree.Head || current.Ref != tree.Ref {
		return "", fmt.Errorf("HEAD 或分支已变化，请刷新")
	}
	indexed := map[string]WorkingFile{}
	for _, file := range current.Files {
		indexed[file.Path] = file
	}
	selected := map[string]bool{}
	for _, file := range request.Files {
		selected[file.Path] = true
	}
	var restore, remove []string
	seen := map[string]bool{}
	for _, file := range request.Files {
		now, exists := indexed[file.Path]
		if !exists || now.Status != file.Status || now.OldPath != file.OldPath || file.Stamp != "" && file.Stamp != now.Stamp {
			return "", fmt.Errorf("文件状态或内容已变化，请刷新：%s", file.Path)
		}
		if file.Conflicted() {
			return "", fmt.Errorf("不自动 Revert 冲突文件：%s", file.Path)
		}
		if file.OldPath != "" && !selected[file.OldPath] {
			if strings.Contains(file.Status, "R") {
				if _, err := os.Lstat(filepath.Join(path, filepath.FromSlash(file.OldPath))); err == nil {
					return "", fmt.Errorf("重命名旧路径已重新创建，请先处理：%s", file.OldPath)
				}
			}
			if _, exists := indexed[file.OldPath]; exists {
				return "", fmt.Errorf("重命名旧路径另有变更，请一并选择或先处理：%s", file.OldPath)
			}
		}
		for _, p := range []string{file.Path, file.OldPath} {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			if !validWorkingPath(p) {
				return "", fmt.Errorf("无效文件路径")
			}
			info, err := os.Lstat(filepath.Join(path, filepath.FromSlash(p)))
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
			if err == nil && info.IsDir() {
				return "", fmt.Errorf("不自动删除目录或子模块：%s", p)
			}
			existsInHead := ""
			if tree.Head != "" {
				var err error
				existsInHead, err = git(path, "ls-tree", "-z", tree.Head, "--", ":(literal)"+p)
				if err != nil {
					return "", err
				}
			}
			if existsInHead != "" {
				restore = append(restore, ":(literal)"+p)
			} else {
				remove = append(remove, p)
			}
		}
	}
	if len(restore) > 0 {
		args := []string{"restore", "--source=" + tree.Head, "--staged", "--worktree", "--"}
		args = append(args, restore...)
		if out, err := git(path, args...); err != nil {
			return out, fmt.Errorf("恢复已提交文件失败，部分文件可能已恢复，请刷新：%w", err)
		}
	}
	for _, p := range remove {
		if out, err := git(path, "rm", "--cached", "-f", "--ignore-unmatch", "--", ":(literal)"+p); err != nil {
			return out, err
		}
		if err := os.Remove(filepath.Join(path, filepath.FromSlash(p))); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("删除未提交文件失败：%s: %w", p, err)
		}
	}
	return fmt.Sprintf("已 Revert %d 个文件，恢复 HEAD 版本并清除所选暂存/未跟踪变更", len(request.Files)), nil
}
