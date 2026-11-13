package main

import (
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func interactiveClient(t *testing.T) *client {
	t.Helper()
	repo := testRepo(t)
	writeCommit(t, repo, "main.go", "package main\nfunc target() {}\nfunc use() {}\n")
	writeWorkingFile(t, repo, "main.go", "package main\nfunc target() {}\nfunc use() {\n target()\n _ = target\n}\n")
	a := testClient([]Repository{inspect(Entry{Path: repo, Name: "interactive"})})
	a.readRunner = func(work func() func()) { work()() }
	a.switchTab(1)
	a.selectWorkingFile(a.workingTrees[0], a.workingTrees[0].Files[0])
	return a
}
func dragText(t *testing.T, tester *ui.Tester, first, last string) {
	t.Helper()
	start, ok := tester.Find(first)
	if !ok {
		t.Fatal("missing selection start", first)
	}
	end, ok := tester.Find(last)
	if !ok {
		t.Fatal("missing selection end", last)
	}
	tester.Press(start.X+0.5, start.Y+start.H/2)
	tester.Move(end.X+end.W-0.5, end.Y+end.H/2)
	tester.Release(end.X+end.W-0.5, end.Y+end.H/2)
	tester.Key(ui.Cmd, ui.KeyC)
}
func TestDiffSelectionCopyAndCtrlClickInRealWorkspace(t *testing.T) {
	for _, mode := range []string{"Inline", "Side-by-side"} {
		t.Run(mode, func(t *testing.T) {
			a := interactiveClient(t)
			tester := ui.NewTester(a.view, 1560, 940)
			if mode == "Side-by-side" {
				if err := tester.Click(mode); err != nil {
					t.Fatal(err)
				}
			}
			dragText(t, tester, " target()", " _ = target")
			if got := tester.Clipboard(); got != " target()\n _ = target" {
				t.Fatalf("%s copy=%q", mode, got)
			}
			word, ok := tester.Find("跳转 main.go:4:2 target")
			if !ok {
				t.Fatal("missing symbol")
			}
			tester.Press(word.X+0.2, word.Y+word.H/2)
			tester.Move(word.X+word.W-0.2, word.Y+word.H/2)
			tester.Release(word.X+word.W-0.2, word.Y+word.H/2)
			tester.Key(ui.Cmd, ui.KeyC)
			if tester.Clipboard() != "target" || a.codeNavigation.Open {
				t.Fatal("identifier cannot be selected normally", tester.Clipboard())
			}

			if mode == "Side-by-side" {
				if output := os.Getenv("MYGO_SIDE_DIFF_SCREENSHOT"); output != "" {
					file, err := os.Create(output)
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(file, tester.Image()); err != nil {
						t.Fatal(err)
					}
					file.Close()
				}
			}
			if err := tester.ClickWith(ui.Ctrl, "跳转 main.go:4:2 target"); err != nil {
				t.Fatal(err)
			}
			if !a.codeNavigation.Preview || a.codeNavigation.Target.Definition.Line != 2 {
				t.Fatal("Ctrl+click in workspace failed", a.codeNavigation)
			}
			if err := tester.Click("关闭"); err != nil {
				t.Fatal(err)
			}
			// An ordinary secondary click must retain the text Copy context menu.
			if err := tester.RightClick("跳转 main.go:4:2 target"); err != nil {
				t.Fatal(err)
			}
			if a.codeNavigation.Open || len(tester.Menu()) == 0 {
				t.Fatal("ordinary right-click did not open the copy menu")
			}
			tester.CloseMenu()
			if !isDefinitionPress(ui.InputEvent{Kind: ui.InputPointerDown, Button: 1, Mods: ui.Ctrl}) {
				t.Fatal("Control-secondary-click must be a definition gesture")
			}

		})
	}
}
func TestResizableSidebarAndSingleLineRepositoryName(t *testing.T) {
	a := interactiveClient(t)
	name := strings.Repeat("very-long-project-name-", 6)
	a.repos[0].Name = name
	a.workingTrees[0].Repository.Name = name
	tester := ui.NewTester(a.view, 1560, 940)
	label := "仓库名 " + name
	narrow, ok := tester.Find(label)
	if !ok {
		t.Fatal("missing repository name")
	}
	if narrow.H > 28 {
		t.Fatalf("project name wrapped: %+v", narrow)
	}
	old := a.sidebarWidths[1]
	tester.Press(old+1, 250)
	tester.Move(old+180, 250)
	tester.Release(old+180, 250)
	if a.sidebarWidths[1] < old+100 {
		t.Fatalf("sidebar did not resize: %f", a.sidebarWidths[1])
	}
	wide, _ := tester.Find(label)
	if wide.W <= narrow.W || wide.H != narrow.H {
		t.Fatalf("name did not grow on one line: %+v -> %+v", narrow, wide)
	}
	if output := os.Getenv("MYGO_RESIZABLE_SCREENSHOT"); output != "" {
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSideBySidePairsAndScrollStayAligned(t *testing.T) {
	f := reviewFixture(t)
	state := newReviewState(f)
	state.SideBySide = true
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { reviewSideBySide(c, f, &state, "before", "after", CodeNavigation{}, CodeNavigation{}) })
	}, 1000, 300)
	old, ok := tester.Find("// unchanged line 10")
	if !ok {
		t.Fatal("old change not rendered")
	}
	new, ok := tester.Find("func first() string { return \"one\" }")
	if !ok || old.Y != new.Y {
		t.Fatalf("changes not aligned: %+v %+v", old, new)
	}
	tester.Scroll(250, 200, 0, 180)
	tester.Frame()
	if state.SideScroll[0].Y != state.SideScroll[1].Y {
		t.Fatalf("vertical scroll diverged: %+v", state.SideScroll)
	}
}
