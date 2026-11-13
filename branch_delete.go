package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type uncertainBranchDelete struct{ cause error }

func (e *uncertainBranchDelete) Error() string {
	return "删除结果待确认：无法核对远程分支，请刷新远程状态后检查：" + e.cause.Error()
}
func isUncertainBranchDelete(err error) bool {
	var target *uncertainBranchDelete
	return errors.As(err, &target)
}
func deleteBranch(path, branch string, remote bool) (string, error) {
	if !remote {
		if operationInProgress(path) {
			return "", fmt.Errorf("存在未完成的 Git 操作，请先处理后再删除分支")
		}
		// Git also protects branches checked out in linked worktrees.
		out, err := git(path, "branch", "-d", "--", branch)
		if err != nil {
			return out, fmt.Errorf("安全删除失败；当前分支、其他工作树使用的分支或未合并分支不会强制删除：%w", err)
		}
		return out, nil
	}
	remoteName, name, ok := strings.Cut(branch, "/")
	if !ok || remoteName == "" || name == "" {
		return "", fmt.Errorf("远程分支缺少远程名称：%s", branch)
	}
	remotes, err := git(path, "remote")
	if err != nil {
		return "", err
	}
	configured := false
	for _, candidate := range strings.Split(remotes, "\n") {
		if candidate == remoteName {
			configured = true
		}
	}
	if !configured {
		return "", fmt.Errorf("远程 %s 已不存在", remoteName)
	}
	// A tracking ref describes the fetch endpoint; pushurl may point to a different fork.
	endpoint, err := git(path, "remote", "get-url", "--", remoteName)
	if err != nil || endpoint == "" {
		return "", fmt.Errorf("无法读取远程 %s 的地址：%v", remoteName, err)
	}
	ref := fullBranchRef(branch, true)
	if _, err := git(path, "symbolic-ref", "--quiet", ref); err == nil {
		return "", fmt.Errorf("符号引用不能作为远程分支删除：%s", branch)
	}
	target := "refs/heads/" + name
	expected, err := git(path, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	// An explicit lease prevents deleting a branch advanced since the last fetch.
	out, pushErr := git(path, "push", "--porcelain", "--force-with-lease="+target+":"+expected, "--", endpoint, ":"+target)
	remaining, verifyErr := git(path, "ls-remote", "--refs", "--", endpoint, target)
	if verifyErr != nil {
		return out, &uncertainBranchDelete{cause: verifyErr}
	}
	exists := false
	for _, line := range strings.Split(remaining, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == target {
			exists = true
		}
	}
	if exists {
		if pushErr != nil {
			return out, fmt.Errorf("远程分支仍存在；若远程已有新提交，请 Fetch 后重新检查：%w", pushErr)
		}
		return out, fmt.Errorf("远程分支仍存在，删除未生效或分支已重新创建")
	}
	// Only remove the tracking ref if another fetch has not replaced it.
	if _, err := git(path, "update-ref", "-d", ref, expected); err != nil {
		out += "\n远程已删除，但本地跟踪引用已变化，请 Fetch 检查"
	}
	if pushErr != nil {
		out += "\n命令异常后已核对：远程分支不存在"
	}
	return out + "\n已删除并核对远程分支：" + remoteName + "/" + name, nil
}

type branchDeletion struct {
	Open, Remote bool
	Branch       string
	Targets      []Repository
}

func (a *client) requestBranchDeletion(branch string, remote bool, targets []Repository) {
	if a.busy || len(targets) == 0 {
		return
	}
	a.branchDeletion = branchDeletion{Open: true, Remote: remote, Branch: branch, Targets: append([]Repository(nil), targets...)}
}
func (a *client) branchDeletionPanel(c *ui.Context) {
	if !a.branchDeletion.Open {
		return
	}
	ui.Modal(c.Key("delete-branch-confirmation"), &a.branchDeletion.Open, func() {
		ui.Column(c).Width(620).MaxHeight(500).Gap(12).Children(func() {
			pending := a.branchDeletion
			kind := "本地"
			if pending.Remote {
				kind = "远程"
			}
			ui.Text(c, "删除"+kind+"分支").FontSize(16).Bold()
			ui.Text(c, pending.Branch).Font("monospace").Selectable()
			message := "仅安全删除已合并的本地分支；当前分支和其他工作树正在使用的分支将跳过。"
			if pending.Remote {
				message = "将删除对应远程仓库上的真实分支，不是仅移除本地跟踪引用。本地分支保留；远程已更新时会拒绝删除。"
			}
			ui.Text(c, message)
			ui.Textf(c, "影响 %d 个仓库：", len(pending.Targets))
			ui.Scroll(c).MaxHeight(200).Children(func() {
				for _, repo := range pending.Targets {
					ui.Text(c, repo.Name+"\n"+repo.Path).FontSize(12)
				}
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(func() { a.branchDeletion.Open = false })
				ui.Button(c, "删除"+kind+"分支").Label("确认删除" + kind + "分支").Disabled(a.busy).OnClick(func() {
					a.branchDeletion.Open = false
					a.showOutput = true
					a.runBranchAction("delete", pending.Branch, pending.Remote, pending.Targets)
				})
			})
		})
	})
}
