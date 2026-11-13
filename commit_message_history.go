package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

type commitMessageHistoryState struct {
	Open, Loading bool
	Generation    uint64
	Query, Error  string
	Messages      []string
}

func commitButtonLabels(amend bool) (string, string) {
	if amend {
		return "Amend Commit", "Amend Commit and Push…"
	}
	return "Commit", "Commit and Push…"
}
func uniqueCommitMessages(messages []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, message := range messages {
		message = strings.TrimRight(message, "\r\n")
		if strings.TrimSpace(message) == "" || seen[message] {
			continue
		}
		seen[message] = true
		result = append(result, message)
		if len(result) == 50 {
			break
		}
	}
	return result
}
func recentCommitMessages(ctx context.Context, repos []Repository) ([]string, string) {
	type entry struct {
		Message string
		Time    int64
	}
	var entries []entry
	var problems []string
	for _, repo := range repos {
		if ctx.Err() != nil {
			return nil, ctx.Err().Error()
		}
		if _, err := gitContext(ctx, repo.Path, "rev-parse", "--verify", "HEAD"); err != nil {
			continue
		}
		data, err := rawGitContext(ctx, repo.Path, "log", "-20", "--format=%ct%x00%B%x00", "HEAD", "--")
		if err != nil {
			problems = append(problems, repo.Name+": "+err.Error())
			continue
		}
		fields := strings.Split(string(data), "\x00")
		for i := 0; i+1 < len(fields); i += 2 {
			timestamp, err := strconv.ParseInt(strings.TrimSpace(fields[i]), 10, 64)
			if err != nil {
				continue
			}
			entries = append(entries, entry{fields[i+1], timestamp})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time > entries[j].Time })
	var messages []string
	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}
	return uniqueCommitMessages(messages), strings.Join(problems, "\n")
}
func (a *client) openCommitMessageHistory() {
	if a.busy {
		return
	}
	a.ensureChangelists()
	a.messageHistory.Generation++
	generation := a.messageHistory.Generation
	a.messageHistory.Open = true
	a.messageHistory.Loading = true
	a.messageHistory.Query = ""
	a.messageHistory.Error = ""
	saved := append([]string(nil), a.changelists.Messages...)
	a.messageHistory.Messages = uniqueCommitMessages(saved)
	var repos []Repository
	for _, request := range a.workingRequests("") {
		repos = append(repos, request.Tree.Repository)
	}
	if len(repos) == 0 {
		repos = append(repos, a.repos...)
	}
	a.startRead("commit-message-history", func(ctx context.Context) func() {
		messages, problem := recentCommitMessages(ctx, repos)
		return func() {
			if !a.messageHistory.Open || generation != a.messageHistory.Generation {
				return
			}
			a.messageHistory.Loading = false
			a.messageHistory.Error = problem
			a.messageHistory.Messages = uniqueCommitMessages(append(saved, messages...))
		}
	})
}
func (a *client) useCommitMessage(message string) {
	if a.busy {
		return
	}
	a.commitMessage = message
	a.messageHistory.Open = false
	a.cancelRead("commit-message-history")
	a.persistChangelists()
}
func (a *client) commitMessageHistoryPanel(c *ui.Context) {
	if !a.messageHistory.Open {
		return
	}
	ui.Modal(c.Key("commit-message-history"), &a.messageHistory.Open, func() {
		ui.Column(c).Width(660).MaxHeight(540).Gap(8).Children(func() {
			ui.Text(c, "Commit Message History").FontSize(16).Bold()
			ui.TextInput(c.Key("message-search"), &a.messageHistory.Query).Label("搜索 Commit Message").Placeholder("搜索提交说明")
			if a.messageHistory.Loading {
				ui.Text(c, "正在读取最近提交…").FontSize(11)
			}
			if a.messageHistory.Error != "" {
				ui.Text(c, a.messageHistory.Error).FontSize(11).TextColor(statusOrange)
			}
			ui.Scroll(c).MaxHeight(390).Children(func() {
				found := 0
				for i, message := range a.messageHistory.Messages {
					if !strings.Contains(strings.ToLower(message), strings.ToLower(strings.TrimSpace(a.messageHistory.Query))) {
						continue
					}
					found++
					title, _, _ := strings.Cut(message, "\n")
					ui.ButtonBase(c.Key(i)).FillWidth().MinHeight(30).Padding(5, 8).Justify(ui.Start).Label(fmt.Sprintf("引用 Commit Message %d", i)).Tooltip(message).OnClick(func() { a.useCommitMessage(message) }).Children(func() { ui.Text(c, title).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0) })
				}
				if found == 0 && !a.messageHistory.Loading {
					ui.Text(c, "暂无匹配的 Commit Message").FontSize(12).TextColor(c.Theme().TextMuted)
				}
			})
			ui.Button(c, "关闭").OnClick(func() { a.messageHistory.Open = false; a.cancelRead("commit-message-history") })
		})
	})
}
