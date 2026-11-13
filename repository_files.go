package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

type repositoryFileState struct {
	Tree            WorkingTree
	Root            *workingNode
	Loading, Loaded bool
	Error           string
}

func readRepositoryFiles(ctx context.Context, repo Repository) repositoryFileState {
	state := repositoryFileState{Loaded: true, Tree: readWorkingTreeContext(ctx, repo)}
	if state.Tree.Error != "" {
		state.Error = state.Tree.Error
		return state
	}
	data, err := rawGitContext(ctx, repo.Path, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		state.Error = err.Error()
		return state
	}
	changes := map[string]WorkingFile{}
	for _, file := range state.Tree.Files {
		changes[file.Path] = file
	}
	var files []WorkingFile
	seen := map[string]bool{}
	for _, file := range strings.Split(string(data), "\x00") {
		if ctx.Err() != nil {
			state.Error = ctx.Err().Error()
			return state
		}
		if file == "" || seen[file] || !validWorkingPath(file) {
			continue
		}
		seen[file] = true
		if _, err := os.Lstat(filepath.Join(repo.Path, filepath.FromSlash(file))); err != nil {
			if !os.IsNotExist(err) {
				state.Error = "部分文件无法读取：" + err.Error()
			}
			continue
		}
		value, ok := changes[file]
		if !ok {
			value = WorkingFile{Path: file}
		}
		files = append(files, value)
	}
	state.Tree.Files = files
	state.Root = workingHierarchy(files)
	return state
}
func (a *client) browserOpen(key string) *bool {
	if a.expanded == nil {
		a.expanded = map[string]*bool{}
	}
	if a.expanded[key] == nil {
		value := false
		a.expanded[key] = &value
	}
	return a.expanded[key]
}
func (a *client) loadRepositoryFiles(repo Repository, force bool) {
	if a.repositoryFiles == nil {
		a.repositoryFiles = map[string]repositoryFileState{}
	}
	old := a.repositoryFiles[repo.Path]
	if !force && (old.Loaded || old.Loading) {
		return
	}
	old.Loading = true
	old.Error = ""
	a.repositoryFiles[repo.Path] = old
	a.startRead("repository-files:"+repo.Path, func(ctx context.Context) func() {
		state := readRepositoryFiles(ctx, repo)
		return func() {
			a.repositoryFiles[repo.Path] = state
			if a.tab == 0 && a.workingFileKey != "" && a.workingFileTree.Repository.Path == repo.Path {
				for _, file := range state.Tree.Files {
					if file.Path == a.workingFile.Path {
						a.selectWorkingFile(state.Tree, file)
						return
					}
				}
				a.clearWorkingComparison()
			}
		}
	})
}
func (a *client) refreshRepositoryFiles(repos []Repository) {
	for _, repo := range repos {
		if state, ok := a.repositoryFiles[repo.Path]; ok && (state.Loaded || state.Loading) {
			a.cancelRead("repository-files:" + repo.Path)
			if *a.browserOpen("repository:" + repo.Path) {
				a.loadRepositoryFiles(repo, true)
			} else {
				delete(a.repositoryFiles, repo.Path)
			}
		}
	}
}
func (a *client) repositoryFileNodes(c *ui.Context, repo Repository, state repositoryFileState, nodes []*workingNode) {
	for _, node := range nodes {
		if node.File == nil {
			open := a.browserOpen("repository-folder:" + repo.Path + "\x00" + node.Path)
			treeContentAction(ui.TreeItem(c.Key(node.Path), node.Name, open, func() { a.repositoryFileNodes(c, repo, state, node.Children) }).Label(node.Name).Tooltip(node.Path), nil, func() { *open = !*open })
		} else {
			file := *node.File
			ui.TreeItem(c.Key(node.Path), node.Name, nil, nil).Label("工作区文件 " + repo.Name + "/" + file.Path).Tooltip(file.Path).Selected(a.workingFileKey == workingFileKey(repo.Path, file.Path)).OnClick(func() {
				if a.repositoryPath != repo.Path {
					a.selectRepository(repo)
				}
				a.selectWorkingFile(state.Tree, file)
			})
		}
	}
}
func (a *client) repositoryFilePanel(c *ui.Context) {
	ui.Column(c.Key("repository-file:" + a.workingFileKey)).Fill().Padding(10).Gap(8).Children(func() {
		ui.Text(c, a.workingFileTree.Repository.Name+" · "+a.workingFile.Path).Bold().SingleLine().Tooltip(a.workingFile.Path)
		a.reviewFileNavigation(c)
		if a.workingComparisonLoading {
			ui.Text(c, "正在读取工作区文件…")
			return
		}
		if a.workingComparisonError != "" {
			ui.Text(c, a.workingComparisonError)
			return
		}
		if a.workingComparison.Notice != "" {
			ui.Text(c, a.workingComparison.Notice)
			return
		}
		comparisonPane(c, "当前工作区 · "+a.workingFileTree.Repository.Branch, a.workingComparison.AfterDocument, nil, false, CodeNavigation{Client: a, Source: a.workingComparison.AfterSource})
	})
}
func (a *client) workingReviewTrees() []WorkingTree {
	if a.tab == 0 {
		if state, ok := a.repositoryFiles[a.workingFileTree.Repository.Path]; ok && state.Loaded {
			return []WorkingTree{state.Tree}
		}
		return nil
	}
	return a.workingTrees
}
func repositoryMatches(repo Repository, query string) bool {
	return strings.Contains(strings.ToLower(repo.Name+" "+repo.Path), strings.ToLower(strings.TrimSpace(query)))
}
func filteredBranchRepositories(repos []Repository, query string, remote bool) []Repository {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return repos
	}
	result := make([]Repository, 0, len(repos))
	for _, repo := range repos {
		var branches []string
		for _, branch := range repositoryBranches(repo, remote) {
			if strings.Contains(strings.ToLower(branch), query) {
				branches = append(branches, branch)
			}
		}
		if remote {
			repo.RemoteBranches = branches
		} else {
			repo.Branches = branches
		}
		result = append(result, repo)
	}
	return result
}

// Keep messages separate from the tree so unloaded and empty repositories are distinguishable.
func repositoryFileMessage(state repositoryFileState) string {
	if state.Loading {
		return "正在读取文件…"
	}
	if state.Error != "" {
		return state.Error
	}
	if state.Loaded && (state.Root == nil || len(state.Root.Children) == 0) {
		return "暂无工作区文件"
	}
	return ""
}
