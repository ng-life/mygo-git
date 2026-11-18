package main

import (
	"context"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCommitMessageHistoryAndAmendButtonsUI(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	message := "SCM-N12345 最近提交\n\n完整正文第二段"
	path := a.repos[0].Path
	mustGit(t, path, "commit", "--allow-empty", "-m", message)
	tester := ui.NewTester(a.view, 1560, 940)
	tab, _ := tester.Find("Repositories")
	if tab.W > 24 {
		t.Fatalf("tab strip too wide: %+v", tab)
	}
	if tester.HasText("Commit Selected Repositories") || tester.HasText("Shared across repositories") {
		t.Fatal("verbose commit labels remain")
	}
	if err := tester.Click("Commit Message History"); err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, text := range a.messageHistory.Messages {
		if text == message {
			index = i
		}
	}
	if index < 0 {
		t.Fatalf("recent message missing: %+v", a.messageHistory)
	}
	if err := tester.Click("Search Commit Messages"); err != nil {
		t.Fatal(err)
	}
	tester.Type("SCM-N12345")
	if err := tester.Click("Use Commit Message " + strconv.Itoa(index)); err != nil {
		t.Fatal(err)
	}
	if a.commitMessage != message || a.messageHistory.Open {
		t.Fatal("full message not applied")
	}
	tester.Key(ui.Ctrl, ui.KeyM)
	if !a.messageHistory.Open {
		t.Fatal("history shortcut failed")
	}
	if err := tester.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Amend"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Amend Commit") || !tester.HasText("Amend Commit and Push…") {
		t.Fatal("Amend buttons not updated", tester.Texts())
	}
	if dest := os.Getenv("MYGO_COMMIT_CONTROLS_SCREENSHOT"); dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			t.Fatal(err)
		}
		fresh := ui.NewTester(a.view, 1560, 940)
		if err = png.Encode(f, fresh.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := tester.Click("Amend"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("Amend Commit and Push…") || !tester.HasText("Commit and Push…") {
		t.Fatal("normal labels not restored")
	}
	if a.commitMessage != message {
		t.Fatal("toggle changed draft")
	}
}
func TestCommitMessageHistoryPersistenceAndDeduplication(t *testing.T) {
	a := workingClient(t)
	a.switchTab(1)
	a.ensureChangelists()
	a.changelists.Messages = []string{"older"}
	a.toggleWorkingFiles(a.workingTrees[0].Repository.Path, a.workingTrees[0].Files, true)
	a.commitMessage = "new message\n\nbody"
	a.commitWorking(a.repos[0].Path)
	if len(a.changelists.Messages) != 2 || a.changelists.Messages[0] != "new message\n\nbody" {
		t.Fatal(a.changelists.Messages)
	}
	path := t.TempDir() + "/changelists.json"
	if err := saveChangelists(path, a.changelists); err != nil {
		t.Fatal(err)
	}
	state, err := loadChangelists(path)
	if err != nil || len(state.Messages) != 2 || state.Messages[0] != a.changelists.Messages[0] {
		t.Fatal(state, err)
	}
	messages, problem := recentCommitMessages(context.Background(), a.repos)
	if problem != "" || len(messages) == 0 || !strings.Contains(strings.Join(messages, "\n"), "new message\n\nbody") {
		t.Fatal(messages, problem)
	}
	if got := uniqueCommitMessages([]string{"same\n", "same", "", "  ", "other"}); len(got) != 2 {
		t.Fatal(got)
	}
}
