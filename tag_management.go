package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type tagDialogState struct {
	Open                               bool
	Action, Name, Ref, Message, Remote string
	Annotated                          bool
	Targets                            []Repository
	Selected                           map[string]bool
}

func (a *client) openTagDialog(action, name string, targets []Repository) {
	selected := map[string]bool{}
	for _, repo := range targets {
		selected[repo.Path] = true
	}
	a.tagDialog = tagDialogState{Open: true, Action: action, Name: name, Ref: "HEAD", Remote: "origin", Targets: append([]Repository(nil), targets...), Selected: selected}
}
func (a *client) tagMenu(menu *ui.Menu, name string, targets []Repository) {
	menu.Item(fmt.Sprintf("Tag %s · 全部 %d 个仓库", name, len(targets))).Disabled(true)
	if menu.Item("Review… · 比较 Tag").Disabled(len(targets) == 0).Chosen() {
		a.openReviewDialog(targets, "refs/tags/"+name)
	}
	if menu.Item("Log · 查看 Tag 历史").Chosen() {
		a.selectReference("/"+name, name, "tags")
	}
	if menu.Item("checkout · 切换到 Tag（detached HEAD）").Disabled(a.busy).Chosen() {
		a.openTagDialog("checkout", name, targets)
	}
	if menu.Item("推送 Tag…").Disabled(a.busy).Chosen() {
		a.openTagDialog("push", name, targets)
	}
	menu.Separator()
	if menu.Item("新建 Tag…").Disabled(a.busy).Chosen() {
		a.openTagDialog("create", "", targets)
		a.tagDialog.Ref = "refs/tags/" + name
	}
	if menu.Item("删除本地 Tag…").Disabled(a.busy).Chosen() {
		a.openTagDialog("delete", name, targets)
	}
}
func executeTagOperation(repo Repository, request tagDialogState) (string, error) {
	ref := "refs/tags/" + request.Name
	if _, err := git(repo.Path, "check-ref-format", ref); err != nil {
		return "", fmt.Errorf("无效 Tag 名称：%s", request.Name)
	}
	if request.Action == "create" {
		target, err := resolveReviewRef(context.Background(), repo, request.Ref)
		if err != nil {
			return "", err
		}
		if request.Annotated {
			if strings.TrimSpace(request.Message) == "" {
				return "", fmt.Errorf("附注 Tag 的 message 不能为空")
			}
			return git(repo.Path, "tag", "-a", "-m", request.Message, "--", request.Name, target)
		}
		// Empty expected value atomically requires the ref to be absent.
		return git(repo.Path, "update-ref", ref, target, "")
	}
	expected := repo.TagObjects[request.Name]
	if expected == "" {
		return "", fmt.Errorf("缺少 Tag 快照，请刷新后重试")
	}
	current, err := git(repo.Path, "show-ref", "--verify", "--hash", ref)
	if err != nil || current != expected {
		return "", fmt.Errorf("Tag 已变化或不存在，请刷新后重试")
	}
	switch request.Action {
	case "delete":
		return git(repo.Path, "update-ref", "--no-deref", "-d", ref, expected)
	case "checkout":
		if operationInProgress(repo.Path) {
			return "", fmt.Errorf("存在未完成的 Git 操作")
		}
		status, err := git(repo.Path, "status", "--porcelain")
		if err != nil {
			return "", err
		}
		if status != "" {
			return "", fmt.Errorf("工作区不干净，已跳过")
		}
		target, err := resolveReviewRef(context.Background(), repo, expected)
		if err != nil {
			return "", err
		}
		return git(repo.Path, "switch", "--detach", target)
	case "push":
		remotes, err := git(repo.Path, "remote")
		if err != nil {
			return "", err
		}
		found := false
		for _, name := range strings.Fields(remotes) {
			if name == request.Remote {
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("远程名称不存在：%s", request.Remote)
		}
		// Pin the source object; never overwrite a different remote tag.
		return git(repo.Path, "push", "--porcelain", request.Remote, expected+":"+ref)
	}
	return "", fmt.Errorf("不支持的 Tag 操作：%s", request.Action)
}
func (a *client) applyTagOperation() {
	request := a.tagDialog
	request.Targets = nil
	request.Selected = nil
	var targets []Repository
	for _, repo := range a.tagDialog.Targets {
		if a.tagDialog.Selected[repo.Path] {
			targets = append(targets, repo)
		}
	}
	a.tagDialog.Open = false
	a.jobProgress("Tag · "+request.Action+" · "+request.Name, func(report taskReporter) (func(), error) {
		var results []OperationResult
		for i, repo := range targets {
			report("Tag · "+repo.Name, i, len(targets)*2, nil)
			out, err := executeTagOperation(repo, request)
			results = append(results, OperationResult{Repository: repo, Output: out, Err: err})
			report("Tag · "+repo.Name, i+1, len(targets)*2, err)
		}
		repos := inspectWithProgress(entriesFor(targets), report, len(targets), len(targets)*2)
		return func() {
			a.mergeRepositories(repos)
			a.output = formatResults(results)
			a.showOutput = true
			if a.groupTags && a.group == "/"+request.Name && len(referenceTargets(a.repos, request.Name, "tags")) == 0 {
				a.groupTags = false
				a.group = ""
				a.branch = ""
			}
		}, nil
	})
}
func (a *client) tagManagementPanel(c *ui.Context) {
	if !a.tagDialog.Open {
		return
	}
	ui.Modal(c.Key("tag-management"), &a.tagDialog.Open, func() {
		ui.Column(c).Width(660).MaxHeight(620).Gap(10).Children(func() {
			title := map[string]string{"create": "新建 Tag", "delete": "删除本地 Tag", "push": "推送 Tag", "checkout": "切换到 Tag"}[a.tagDialog.Action]
			ui.Text(c, title).FontSize(18).Bold()
			if a.tagDialog.Action == "create" {
				ui.TextInput(c.Key("tag-name"), &a.tagDialog.Name).Label("Tag 名称").Placeholder("例如 release/v1.0.0")
				ui.TextInput(c.Key("tag-ref"), &a.tagDialog.Ref).Label("Tag 目标引用").Placeholder("HEAD / 分支 / Commit SHA")
				ui.Checkbox(c, &a.tagDialog.Annotated, "附注 Tag")
				if a.tagDialog.Annotated {
					ui.TextArea(c, &a.tagDialog.Message).Label("Tag message").Placeholder("Tag message").Height(70)
				}
			} else {
				ui.Text(c, a.tagDialog.Name).Font("monospace").Selectable()
			}
			if a.tagDialog.Action == "push" {
				ui.TextInput(c, &a.tagDialog.Remote).Label("Tag 远程名称").Placeholder("远程名称，例如 origin")
				ui.Text(c, "推送所选本地 Tag，不覆盖已有的不同远程 Tag。").FontSize(12)
			}
			if a.tagDialog.Action == "delete" {
				ui.Text(c, "仅删除所选仓库中的本地 Tag，保留远程 Tag；引用已变化时拒绝删除。").FontSize(12)
			}
			if a.tagDialog.Action == "checkout" {
				ui.Text(c, "将进入 detached HEAD；工作区有变更或存在未完成操作时跳过。").FontSize(12)
			}
			ui.Text(c, "影响仓库（可勾选）：").FontSize(12)
			selected := 0
			ui.Scroll(c).MaxHeight(230).Children(func() {
				for _, repo := range a.tagDialog.Targets {
					on := a.tagDialog.Selected[repo.Path]
					if on {
						selected++
					}
					ui.Checkbox(c.Key(repo.Path), &on, repo.Name).Label("Tag 仓库 " + repo.Name).Tooltip(repo.Path).OnClick(func() { a.tagDialog.Selected[repo.Path] = on })
				}
			})
			disabled := a.busy || selected == 0 || strings.TrimSpace(a.tagDialog.Name) == "" || (a.tagDialog.Action == "create" && (strings.TrimSpace(a.tagDialog.Ref) == "" || (a.tagDialog.Annotated && strings.TrimSpace(a.tagDialog.Message) == "")))
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(func() { a.tagDialog.Open = false })
				ui.PrimaryButton(c, "确认"+title).Disabled(disabled).OnClick(a.applyTagOperation)
			})
		})
	})
}

func (a *client) fetchTags(targets []Repository) {
	targets = append([]Repository(nil), targets...)
	a.jobProgress("获取 Tags", func(report taskReporter) (func(), error) {
		var results []OperationResult
		for i, repo := range targets {
			report("Tag · "+repo.Name, i, len(targets)*2, nil)
			out, err := git(repo.Path, "fetch", "--all", "--tags")
			results = append(results, OperationResult{Repository: repo, Output: out, Err: err})
			report("Tag · "+repo.Name, i+1, len(targets)*2, err)
		}
		repos := inspectWithProgress(entriesFor(targets), report, len(targets), len(targets)*2)
		return func() { a.mergeRepositories(repos); a.output = formatResults(results); a.showOutput = true }, nil
	})
}
