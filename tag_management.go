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
	menu.Item(fmt.Sprintf("Tag %s · All %d repositories", name, len(targets))).Disabled(true)
	if menu.Item("Review… · Compare Tag").Disabled(len(targets) == 0).Chosen() {
		a.openReviewDialog(targets, "refs/tags/"+name)
	}
	if menu.Item("Log · Tag History").Chosen() {
		a.selectReference("/"+name, name, "tags")
	}
	if menu.Item("Checkout Tag (detached HEAD)").Disabled(a.busy).Chosen() {
		a.openTagDialog("checkout", name, targets)
	}
	if menu.Item("Push Tag…").Disabled(a.busy).Chosen() {
		a.openTagDialog("push", name, targets)
	}
	menu.Separator()
	if menu.Item("New Tag…").Disabled(a.busy).Chosen() {
		a.openTagDialog("create", "", targets)
		a.tagDialog.Ref = "refs/tags/" + name
	}
	if menu.Item("Delete Local Tag…").Disabled(a.busy).Chosen() {
		a.openTagDialog("delete", name, targets)
	}
}
func executeTagOperation(repo Repository, request tagDialogState) (string, error) {
	ref := "refs/tags/" + request.Name
	if _, err := git(repo.Path, "check-ref-format", ref); err != nil {
		return "", fmt.Errorf("Invalid tag name: %s", request.Name)
	}
	if request.Action == "create" {
		target, err := resolveReviewRef(context.Background(), repo, request.Ref)
		if err != nil {
			return "", err
		}
		if request.Annotated {
			if strings.TrimSpace(request.Message) == "" {
				return "", fmt.Errorf("Annotated tag message is required")
			}
			return git(repo.Path, "tag", "-a", "-m", request.Message, "--", request.Name, target)
		}
		// Empty expected value atomically requires the ref to be absent.
		return git(repo.Path, "update-ref", ref, target, "")
	}
	expected := repo.TagObjects[request.Name]
	if expected == "" {
		return "", fmt.Errorf("Tag snapshot unavailable. Refresh and retry")
	}
	current, err := git(repo.Path, "show-ref", "--verify", "--hash", ref)
	if err != nil || current != expected {
		return "", fmt.Errorf("Tag changed or no longer exists. Refresh and retry")
	}
	switch request.Action {
	case "delete":
		return git(repo.Path, "update-ref", "--no-deref", "-d", ref, expected)
	case "checkout":
		if operationInProgress(repo.Path) {
			return "", fmt.Errorf("Git operation in progress")
		}
		status, err := git(repo.Path, "status", "--porcelain")
		if err != nil {
			return "", err
		}
		if status != "" {
			return "", fmt.Errorf("Skipped: working tree is not clean")
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
			return "", fmt.Errorf("Remote does not exist: %s", request.Remote)
		}
		// Pin the source object; never overwrite a different remote tag.
		return git(repo.Path, "push", "--porcelain", request.Remote, expected+":"+ref)
	}
	return "", fmt.Errorf("Unsupported tag operation: %s", request.Action)
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
			title := map[string]string{"create": "New Tag", "delete": "Delete Local Tag", "push": "Push Tag", "checkout": "Checkout Tag"}[a.tagDialog.Action]
			ui.Text(c, title).FontSize(18).Bold()
			if a.tagDialog.Action == "create" {
				ui.TextInput(c.Key("tag-name"), &a.tagDialog.Name).Label("Tag name").Placeholder("e.g. release/v1.0.0")
				ui.TextInput(c.Key("tag-ref"), &a.tagDialog.Ref).Label("Tag target ref").Placeholder("HEAD / Branch / Commit SHA")
				ui.Checkbox(c, &a.tagDialog.Annotated, "Annotated Tag")
				if a.tagDialog.Annotated {
					ui.TextArea(c, &a.tagDialog.Message).Label("Tag message").Placeholder("Tag message").Height(70)
				}
			} else {
				ui.Text(c, a.tagDialog.Name).Font("monospace").Selectable()
			}
			if a.tagDialog.Action == "push" {
				ui.TextInput(c, &a.tagDialog.Remote).Label("Tag remote name").Placeholder("Remote name, e.g. origin")
				ui.Text(c, "Push selected local tags. Existing remote tags with different targets are not overwritten.").FontSize(12)
			}
			if a.tagDialog.Action == "delete" {
				ui.Text(c, "Delete local tags in selected repositories. Remote tags are preserved. Deletion is rejected if a ref has changed.").FontSize(12)
			}
			if a.tagDialog.Action == "checkout" {
				ui.Text(c, "Check out in detached HEAD mode. Skip repositories with working tree changes or an in-progress operation.").FontSize(12)
			}
			ui.Text(c, "Repositories (select to include):").FontSize(12)
			selected := 0
			ui.Scroll(c).MaxHeight(230).Children(func() {
				for _, repo := range a.tagDialog.Targets {
					on := a.tagDialog.Selected[repo.Path]
					if on {
						selected++
					}
					ui.Checkbox(c.Key(repo.Path), &on, repo.Name).Label("Tag repository " + repo.Name).Tooltip(repo.Path).OnClick(func() { a.tagDialog.Selected[repo.Path] = on })
				}
			})
			disabled := a.busy || selected == 0 || strings.TrimSpace(a.tagDialog.Name) == "" || (a.tagDialog.Action == "create" && (strings.TrimSpace(a.tagDialog.Ref) == "" || (a.tagDialog.Annotated && strings.TrimSpace(a.tagDialog.Message) == "")))
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { a.tagDialog.Open = false })
				ui.PrimaryButton(c, "Confirm "+title).Disabled(disabled).OnClick(a.applyTagOperation)
			})
		})
	})
}

func (a *client) fetchTags(targets []Repository) {
	targets = append([]Repository(nil), targets...)
	a.jobProgress("Fetch Tags", func(report taskReporter) (func(), error) {
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
