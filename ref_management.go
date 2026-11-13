package main

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

type referenceDialogState struct {
	Open                bool
	Action, Name, Error string
	Choices             []ReferenceChoice
	Repositories        []Repository
}

func (a *client) selectedReferenceRepositories() []Repository {
	seen := map[string]bool{}
	var repos []Repository
	for _, choice := range a.refSelection {
		for _, repo := range a.referenceRepositoriesFor(choice) {
			if !seen[repo.Path] {
				seen[repo.Path] = true
				repos = append(repos, repo)
			}
		}
	}
	return repos
}
func (a *client) referenceSelectionMenu(menu *ui.Menu) {
	choices := append([]ReferenceChoice(nil), a.refSelection...)
	if len(choices) == 0 {
		return
	}
	menu.Item(fmt.Sprintf("已选 %d 个引用 · %d 个仓库", len(choices), len(a.selectedReferenceRepositories()))).Disabled(true)
	if len(choices) == 1 {
		if menu.Item("与 HEAD 比较").Disabled(a.busy).Chosen() {
			a.openReferenceComparison("HEAD", false)
		}
		if menu.Item("与 Worktree 比较").Disabled(a.busy).Chosen() {
			a.openReferenceComparison("Worktree", false)
		}
		if menu.Item("与指定版本比较…").Disabled(a.busy).Chosen() {
			a.openReferenceComparison("HEAD", true)
		}
	} else if len(choices) == 2 {
		common := len(a.commonReferenceRepositories(choices))
		menu.Item(fmt.Sprintf("共同仓库：%d · 顺序 %s → %s", common, choices[0].Label(), choices[1].Label())).Disabled(true)
		if menu.Item("比较所选两个引用").Disabled(a.busy || common == 0).Chosen() {
			a.openReferenceComparison("", false)
		}
		if menu.Item("反向比较所选两个引用").Disabled(a.busy || common == 0).Chosen() {
			a.refSelection = []ReferenceChoice{choices[1], choices[0]}
			a.openReferenceComparison("", false)
		}
	}
	menu.Separator()
	if len(choices) == 1 {
		choice := choices[0]
		targets := a.referenceRepositoriesFor(choice)
		if menu.Item("从此引用新建分支…").Disabled(a.busy || len(targets) == 0).Chosen() {
			a.refDialog = referenceDialogState{Open: true, Action: "create", Choices: choices, Repositories: targets}
		}
		if choice.Kind == "local" {
			if menu.Item("重命名本地分支…").Disabled(a.busy || len(targets) == 0).Chosen() {
				a.refDialog = referenceDialogState{Open: true, Action: "rename", Name: choice.Name, Choices: choices, Repositories: targets}
			}
		}
		switch choice.Kind {
		case "local", "remote":
			a.branchMenu(menu, choice.Name, choice.Kind == "remote", targets)
		case "tags":
			a.tagMenu(menu, choice.Name, targets)
		case "head":
			if menu.Item("新建 Tag…").Disabled(a.busy).Chosen() {
				a.openTagDialog("create", "", targets)
			}
			if menu.Item("刷新状态 · 对应仓库").Disabled(a.busy).Chosen() {
				a.runReferenceBatch("refresh", choices, targets, "")
			}
			if menu.Item("Fetch · 对应仓库").Disabled(a.busy).Chosen() {
				a.runReferenceBatch("fetch", choices, targets, "")
			}
		}
		return
	}
	// Several references cannot all be checked out or rebased at once. Only
	// unambiguous batch actions survive; comparison is absent above two refs.
	repos := a.selectedReferenceRepositories()
	if menu.Item("刷新所选引用状态").Disabled(a.busy).Chosen() {
		a.runReferenceBatch("refresh", choices, repos, "")
	}
	if menu.Item("Fetch 所选引用的仓库").Disabled(a.busy).Chosen() {
		a.runReferenceBatch("fetch", choices, repos, "")
	}
	head := false
	for _, choice := range choices {
		if choice.Kind == "head" {
			head = true
		}
	}
	if head {
		menu.Item("HEAD 不能删除").Disabled(true)
	}
	if menu.Item("删除所选引用…").Disabled(a.busy || head).Chosen() {
		a.refDialog = referenceDialogState{Open: true, Action: "delete", Choices: choices, Repositories: repos}
	}
}
func (a *client) referenceManagementPanel(c *ui.Context) {
	if !a.refDialog.Open {
		return
	}
	ui.Modal(c.Key("reference-management"), &a.refDialog.Open, func() {
		ui.Column(c).Width(650).MaxHeight(600).Gap(10).Children(func() {
			action := a.refDialog.Action
			title := map[string]string{"delete": "批量删除引用", "create": "从引用新建分支", "rename": "重命名本地分支"}[action]
			ui.Text(c, title).FontSize(16).Bold()
			if action != "delete" {
				ui.TextInput(c.Key("branch-name"), &a.refDialog.Name).Label("新分支名称").Placeholder("例如 feature/SCM-N31137/review")
			}
			message := "创建本地分支，保留当前工作分支和文件内容；已存在的分支不覆盖。"
			if action == "rename" {
				message = "在对应仓库中重命名此本地分支；已存在的新名称不会覆盖。"
			}
			if action == "delete" {
				message = "本地分支仅安全删除已合并分支；远程分支会从真实远程仓库删除并核对；Tag 仅删除本地引用。当前分支、工作树使用的分支及已变化的 Tag 将跳过。"
			}
			ui.Text(c, message).FontSize(12)
			ui.Scroll(c).MaxHeight(330).Children(func() {
				for _, choice := range a.refDialog.Choices {
					ui.Text(c, choice.Label()).Bold().FontSize(12)
					for _, repo := range a.refDialog.Repositories {
						if referenceExists(repo, choice) {
							ui.Text(c, "  "+repo.Name+" · "+repo.Path).FontSize(11)
						}
					}
				}
			})
			if a.refDialog.Error != "" {
				ui.Text(c, a.refDialog.Error).FontSize(12).TextColor(statusOrange)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(func() { a.refDialog.Open = false })
				ui.PrimaryButton(c, "确认"+title).Disabled(a.busy || (action != "delete" && !referenceNameValid(a.refDialog.Name))).OnClick(func() {
					d := a.refDialog
					if d.Action != "delete" && !referenceNameValid(d.Name) {
						a.refDialog.Error = "请输入有效的新分支名称"
						return
					}
					a.refDialog.Open = false
					a.runReferenceBatch(d.Action, append([]ReferenceChoice(nil), d.Choices...), append([]Repository(nil), d.Repositories...), d.Name)
				})
			})
		})
	})
}
func referenceExists(repo Repository, choice ReferenceChoice) bool {
	if choice.Kind == "head" {
		return true
	}
	values := repo.Branches
	if choice.Kind == "remote" {
		values = repo.RemoteBranches
	} else if choice.Kind == "tags" {
		values = repo.Tags
	}
	for _, name := range values {
		if name == choice.Name {
			return true
		}
	}
	return false
}
func executeReferenceOperation(repo Repository, choice ReferenceChoice, action, name string) (string, error) {
	if action == "delete" {
		switch choice.Kind {
		case "head":
			return "", fmt.Errorf("HEAD 不能删除")
		case "tags":
			return executeTagOperation(repo, tagDialogState{Action: "delete", Name: choice.Name})
		case "local", "remote":
			return deleteBranch(repo.Path, choice.Name, choice.Kind == "remote")
		}
	}
	if action == "create" || action == "rename" {
		if !referenceNameValid(name) {
			return "", fmt.Errorf("无效分支名称")
		}
		if _, err := git(repo.Path, "check-ref-format", "refs/heads/"+name); err != nil {
			return "", fmt.Errorf("无效分支名称：%s", name)
		}
		if operationInProgress(repo.Path) {
			return "", fmt.Errorf("存在未完成的 Git 操作")
		}
		if action == "rename" {
			if choice.Kind != "local" {
				return "", fmt.Errorf("只支持重命名本地分支")
			}
			return git(repo.Path, "branch", "-m", "--", choice.Name, name)
		}
		target, err := resolveReviewRef(context.Background(), repo, choice.Ref())
		if err != nil {
			return "", err
		}
		return git(repo.Path, "branch", "--", name, target)
	}
	return "", fmt.Errorf("不支持的引用操作：%s", action)
}
func (a *client) runReferenceBatch(action string, choices []ReferenceChoice, repos []Repository, name string) {
	choices = append([]ReferenceChoice(nil), choices...)
	repos = append([]Repository(nil), repos...)
	if a.busy || len(repos) == 0 {
		return
	}
	if action == "delete" {
		for _, choice := range choices {
			if choice.Kind == "head" {
				return
			}
		}
	}
	a.showOutput = true
	a.jobProgress("引用操作 · "+action, func(report taskReporter) (func(), error) {
		var results []OperationResult
		renamedSuccess := false
		for i, repo := range repos {
			report(action+" · "+repo.Name, i, len(repos)*2, nil)
			if action == "refresh" || action == "fetch" {
				var out string
				var err error
				if action == "fetch" {
					out, err = runRepositoryOperation(repo.Path, "Fetch")
				}
				results = append(results, OperationResult{Repository: repo, Output: out, Err: err})
				report(action+" · "+repo.Name, i+1, len(repos)*2, err)
				continue
			}
			for _, choice := range choices {
				if !referenceExists(repo, choice) {
					continue
				}
				out, err := executeReferenceOperation(repo, choice, action, name)
				if action == "rename" && err == nil {
					renamedSuccess = true
				}
				label := repo
				label.Name += " · " + choice.Label()
				results = append(results, OperationResult{Repository: label, Output: out, Err: err})
				report(action+" · "+repo.Name+" · "+choice.Label(), i, len(repos)*2, err)
			}
			report(action+" · "+repo.Name, i+1, len(repos)*2, nil)
		}
		updated := inspectWithProgress(entriesFor(repos), report, len(repos), len(repos)*2)
		return func() {
			a.mergeRepositories(updated)
			a.pruneReferenceSelection()
			if action == "rename" && renamedSuccess {
				renamed := ReferenceChoice{Kind: "local", Name: name}
				if len(a.referenceRepositoriesFor(renamed)) > 0 && !a.referenceSelected(renamed) {
					a.refSelection = append(a.refSelection, renamed)
				}
				a.group = "/" + name
				a.branch = name
				a.groupRemote = false
				a.groupTags = false
			}
			a.output = formatResults(results)
		}, nil
	})
}
