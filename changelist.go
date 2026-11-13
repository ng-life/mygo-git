package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Changelists show the final HEAD-to-worktree difference. Keep porcelain
// metadata intact for commit/revert, without modifying the user's index.
func changelistFiles(ctx context.Context, path, head string, files []WorkingFile) ([]WorkingFile, error) {
	if head == "" {
		result := make([]WorkingFile, 0, len(files))
		for _, file := range files {
			_, err := os.Lstat(filepath.Join(path, filepath.FromSlash(file.Path)))
			if os.IsNotExist(err) && !file.Conflicted() {
				continue
			}
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			file.Change = "A"
			result = append(result, file)
		}
		return result, nil
	}
	data, err := rawGitContext(ctx, path, "diff", "--no-ext-diff", "--no-textconv", "--name-status", "--no-renames", "-z", head, "--")
	if err != nil {
		return nil, err
	}
	tokens := bytes.Split(data, []byte{0})
	changed := map[string]string{}
	for i := 0; i+1 < len(tokens); i += 2 {
		changed[string(tokens[i+1])] = string(tokens[i])
	}
	result := make([]WorkingFile, 0, len(files))
	for _, file := range files {
		kind := changed[file.Path]
		if kind == "" && file.OldPath != "" {
			kind = changed[file.OldPath]
		}
		if strings.Contains(file.Status, "?") {
			// An index-deleted file recreated locally is untracked from Git's
			// perspective. Compare its filtered blob against HEAD, not the index.
			before, exists := gitContext(ctx, path, "rev-parse", "--verify", head+":"+file.Path)
			if exists == nil {
				after, hashErr := gitContext(ctx, path, "hash-object", "--path="+file.Path, "--", file.Path)
				if hashErr != nil {
					return nil, fmt.Errorf("读取文件 %s：%w", file.Path, hashErr)
				}
				if before == after {
					continue
				}
				kind = "M"
			} else {
				kind = "A"
			}
		}
		if file.Conflicted() {
			kind = "U"
		}
		if kind == "" {
			continue
		}
		file.Change = kind
		result = append(result, file)
	}
	return result, nil
}
