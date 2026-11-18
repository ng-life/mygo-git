package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Tabs own immutable read results. Background readers publish by ID, never by
// the currently selected tab, so selecting another file cannot replace its contents.
type editorTab struct {
	Caret                        *editorCaret
	Code                         *editor.State
	CodeSource                   CodeSource
	Document                     documentPreview
	DiffStates                   map[string]*reviewState
	FileList                     ui.ListState
	PartsScroll, ReviewScroll    ui.ScrollState
	CompareLists                 [3]ui.ListState
	Reverse, SideBySide          bool
	ID, Key, Title, Kind, Parent string
	Tree                         WorkingTree
	Sources                      []WorkingTree
	File                         WorkingFile
	Mode                         string
	Comparison                   FileComparison
	Parts                        []CommitComparison
	Loading                      bool
	Error                        string
	Review                       ReviewSession
	Compare                      CompareState
}

func (a *client) editorByID(id string) *editorTab {
	for _, tab := range a.editorTabs {
		if tab.ID == id {
			return tab
		}
	}
	return nil
}
func (a *client) activeEditor() *editorTab { return a.editorByID(a.editorActive) }
func (a *client) saveEditorContext() {
	tab := a.activeEditor()
	if tab == nil {
		return
	}
	if tab.Parent != "" {
		tab = a.editorByID(tab.Parent)
	}
	if tab != nil && tab.Kind == "compare" && a.review.Open {
		tab.Review = a.review
		tab.Compare = a.compare
		tab.Compare.selectingCommit = false
	}
}
func (a *client) restoreEditorContext() {
	current := a.activeEditor()
	a.clearWorkingComparison()
	a.clearComparison()
	if current != nil {
		if current.Kind == "file" || current.Kind == "changes" {
			a.workingFileTree = current.Tree
			a.workingFile = current.File
			a.workingFileKey = workingFileKey(current.Tree.Repository.Path, current.File.Path)
			a.workingComparison = current.Comparison
			a.workingComparisonLoading = current.Loading
			a.workingComparisonError = current.Error
		}
		if current.Kind == "commit" {
			a.selectedFile = workingFileKey(current.Tree.Repository.Path, current.File.Path)
			a.comparisons = current.Parts
		}
	}
	tab := a.activeEditor()
	if tab != nil && tab.Parent != "" {
		tab = a.editorByID(tab.Parent)
	}
	if tab != nil && tab.Kind == "compare" {
		a.review = tab.Review
		a.compare = tab.Compare
		return
	}
	a.review.Open = false
	a.compare.Active = false
}
func (a *client) openEditor(key, title, kind, parent string) *editorTab {
	a.saveEditorContext()
	for _, tab := range a.editorTabs {
		if tab.Key == key {
			a.editorActive = tab.ID
			return tab
		}
	}
	a.editorSequence++
	tab := &editorTab{ID: fmt.Sprint(a.editorSequence), Key: key, Title: title, Kind: kind, Parent: parent, SideBySide: a.diffSideBySide}
	a.editorTabs = append(a.editorTabs, tab)
	a.editorActive = tab.ID
	return tab
}
func (a *client) activateEditor(id string) {
	if id != a.editorActive && a.codeNavigation.Open {
		a.closeCodeNavigation()
	}
	if id == a.editorActive {
		if !a.review.Open {
			a.restoreEditorContext()
			a.resumeEditorReview()
		}
		return
	}
	a.saveEditorContext()
	tab := a.editorByID(id)
	if tab == nil {
		return
	}
	a.cancelRead("review")
	a.cancelRead("compare-commits")
	a.editorActive = id
	a.restoreEditorContext()
	a.resumeEditorReview()
}
func (a *client) closeEditor(id string) {
	if a.codeNavigation.Open && a.codeNavigation.OriginID == id {
		a.closeCodeNavigation()
	}
	a.saveEditorContext()
	a.cancelRead("editor:" + id)
	a.cancelRead("preview:" + id)
	if tab := a.editorByID(id); tab != nil {
		if tab.Document.Before != nil {
			tab.Document.Before.Destroy()
		}
		if tab.Document.After != nil {
			tab.Document.After.Destroy()
		}
	}
	next := a.editorTabs[:0]
	index := 0
	for i, tab := range a.editorTabs {
		if tab.ID == id {
			index = i
			continue
		}
		if tab.Parent == id {
			tab.Parent = ""
		}
		next = append(next, tab)
	}
	a.editorTabs = next
	if a.editorActive == id {
		a.editorActive = ""
		a.cancelRead("review")
		a.cancelRead("compare-commits")
		a.review.Open = false
		a.compare.Active = false
		if len(next) > 0 {
			a.editorActive = next[min(index, len(next)-1)].ID
		}
		a.restoreEditorContext()
	}
}
func (a *client) closeActiveEditor() { a.closeEditor(a.editorActive) }
func (a *client) openWorkingEditor(tree WorkingTree, file WorkingFile, fileView bool) *editorTab {
	kind, mode := "changes", "combined"
	if fileView {
		kind = "file"
		mode = "file"
	}
	key := kind + ":" + workingFileKey(tree.Repository.Path, file.Path) + "\x00" + mode
	title := filepath.Base(file.Path) + " · " + tree.Repository.Name
	if kind == "changes" {
		title += " · Changes"
	}
	tab := a.openEditor(key, title, kind, "")
	tab.Tree = tree
	tab.File = file
	tab.Mode = mode
	tab.Loading = true
	tab.Error = ""
	return tab
}
func (a *client) openCommitEditor(file MergedFile, parent string) *editorTab {
	var scope []string
	for _, part := range file.Changes {
		scope = append(scope, part.Detail.Commit.Key()+"\x00"+part.Detail.Parent+"\x00"+part.File.OldPath)
	}
	reverse := parent != "" && a.review.Request.Reverse
	tab := a.openEditor("commit:"+file.Key()+":"+strings.Join(scope, "\x00")+fmt.Sprint(":reverse:", reverse), filepath.Base(file.Path)+" · "+file.Repository.Name+" · Commit Diff", "commit", parent)
	tab.Reverse = reverse
	tab.Parent = parent
	tab.Tree.Repository = file.Repository
	tab.File = WorkingFile{Path: file.Path}
	tab.Loading = true
	return tab
}
func (a *client) openReviewEditor(request ReviewRequest) {
	if a.compare.selectingCommit {
		return
	}
	var paths []string
	for _, r := range request.Repositories {
		paths = append(paths, r.Path)
	}
	for _, c := range request.Commits {
		paths = append(paths, c.Key())
	}
	sort.Strings(paths)
	key := "compare:" + request.Kind + ":" + request.Base + ":" + request.Target + ":" + strings.Join(paths, "\x00")
	title := request.Base + " ↔ " + request.Target
	if request.Kind == "workspace" {
		title = request.Base + " ↔ Worktree"
	}
	if request.Kind == "commits" {
		title = "Selected Commits"
	}
	previous := a.editorActive
	tab := a.openEditor(key, title, "compare", "")
	if tab.ID != previous {
		if tab.Review.Open {
			a.review = tab.Review
			a.compare = tab.Compare
		} else {
			a.compare = CompareState{}
		}
	}
}
func (a *client) editorPanel(c *ui.Context) {
	ui.Column(c).Fill().Gap(0).Background(ideaEditor).Children(func() {
		if len(a.editorTabs) == 0 {
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				ui.Text(c, "MyGo Git").FontSize(22).Bold().TextColor(c.Theme().TextMuted)
				ui.Text(c, "Open a file from Repositories or Changes").FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Text(c, "Compare branches and revisions from Logs").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			return
		}
		ui.ScrollHorizontal(c.Key("editor-tab-strip")).Height(32).Shrink(0).FillWidth().Background(ideaPanel).Children(func() {
			ui.Row(c).Gap(0).Children(func() {
				for _, tab := range a.editorTabs {
					active := tab.ID == a.editorActive
					ui.Row(c.Key(tab.ID)).Height(31).Gap(4).Padding(0, 8).DrawOver(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y + 6, W: 1, H: r.H - 12}, ui.Hex("#454749"), 0)
						if active {
							p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 2, W: r.W, H: 2}, ideaLink, 0)
						}
					}).Background(func() ui.Color {
						if tab.ID == a.editorActive {
							return ui.Hex("#4b4d50")
						}
						return ideaPanel
					}()).Children(func() {
						ui.ButtonBase(c.Key("title")).Height(30).MaxWidth(250).Label("Editor Tab " + tab.ID).Tooltip(tab.Title + "\n" + tab.Tree.Repository.Path + "\n" + tab.File.Path).OnClick(func() { a.activateEditor(tab.ID) }).ContextMenu(func(menu *ui.Menu) {
							if menu.Item("Close").Chosen() {
								a.closeEditor(tab.ID)
							}
							if menu.Item("Close Other Tabs").Chosen() {
								a.closeOtherEditors(tab.ID)
							}
							if menu.Item("Close All Tabs").Chosen() {
								a.closeOtherEditors("")
							}
						}).Children(func() {
							ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
								if tab.File.Path != "" {
									fileTypeIcon(c, tab.File.Path)
								}
								ui.Text(c, tab.Title).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
							})
						})
						ui.ButtonBase(c.Key("close")).Height(20).Width(16).Children(func() { ui.Text(c, "×").FontSize(12).TextColor(c.Theme().TextMuted) }).Label("Close Editor Tab " + tab.ID).OnClick(func() { a.closeEditor(tab.ID) })
					})
				}
			})
		})
		tab := a.activeEditor()
		if tab == nil {
			return
		}
		ui.Column(c.Key("editor-content:" + tab.ID)).Grow(1).FillWidth().MinHeight(0).Gap(4).Children(func() {
			if tab.Kind == "compare" {
				ui.Column(c).Fill().Center().Gap(8).Children(func() {
					ui.Text(c, tab.Title).FontSize(15).TextColor(c.Theme().TextMuted)
					ui.Text(c, "Select a changed file or compare commits below").FontSize(12).TextColor(c.Theme().TextMuted)
				})
				return
			}
			if tab.Loading {
				ui.Text(c, "Loading file…").Padding(12)
				return
			}
			if tab.Error != "" {
				ui.Text(c, tab.Error).Padding(12).Selectable()
				return
			}
			if tab.Caret != nil {
				ui.Textf(c, "Ln %d, Col %d", tab.Caret.Line, tab.Caret.Column).FontSize(11).Padding(2, 8).TextColor(c.Theme().TextMuted)
			}
			a.reviewFileNavigation(c)
			a.editorScopeTabs(c, tab)
			if a.documentEditorPanel(c, tab, func() { a.editorSourcePanel(c, tab) }) {
				return
			}
			a.editorSourcePanel(c, tab)
		})
	})
}
func (a *client) editorSourcePanel(c *ui.Context, tab *editorTab) {
	if tab.Kind == "commit" {
		a.editorCommitPanel(c, tab)
		return
	}
	ui.Row(c).Padding(0, 6).Children(func() {
		ui.Spacer(c)
		ui.Button(c, "Refresh File").FontSize(11).OnClick(func() { a.loadEditorWorkingMode(tab.ID, tab.Mode) })
	})
	ui.Text(c, tab.Tree.Repository.Name+" · "+tab.File.Path).FontSize(12).SingleLine().Ellipsis("…").Tooltip(tab.Tree.Repository.Path+"/"+tab.File.Path).Padding(5, 8)

	if tab.Comparison.Notice != "" {
		ui.Text(c, tab.Comparison.Notice).Padding(8)
		return
	}
	if tab.Kind == "file" && tab.Code != nil {
		a.definitionSourcePanel(c, tab)
		return
	}
	if tab.Kind == "file" {
		comparisonPane(c, "Current Worktree · "+tab.Tree.Repository.Branch, tab.Comparison.AfterDocument, nil, false, CodeNavigation{Client: a, Source: tab.Comparison.AfterSource})
		return
	}
	before, after := "HEAD", "Worktree"
	if tab.Mode == "worktree" {
		before = "Index"
	}
	if tab.Mode == "index" {
		after = "Index"
	}
	reviewComparison(c, tab.Comparison, before, after, a)
}
func (a *client) editorCommitPanel(c *ui.Context, tab *editorTab) {
	render := func(part CommitComparison) {
		ui.Text(c, part.Change.Detail.Commit.Repository.Name+" · "+part.Change.File.Label()).FontSize(12).Padding(5, 8)
		if part.Error != "" {
			ui.Text(c, part.Error)
			return
		}
		if part.Comparison.Notice != "" {
			ui.Text(c, part.Comparison.Notice)
			return
		}
		before, after := shortHash(part.Change.Detail.Parent), reviewAfterLabel(part.Change.Detail.Commit.Hash)
		if before == "" {
			before = "Empty Tree"
		}
		if tab.Reverse {
			before = "Worktree"
			after = shortHash(part.Comparison.AfterSource.Revision)
		}
		reviewComparison(c, part.Comparison, "Before · "+before, "After · "+after, a)
	}
	if len(tab.Parts) == 1 {
		render(tab.Parts[0])
		return
	}
	ui.Scroll(c).TrackScroll(&tab.PartsScroll).Fill().Gap(6).Children(func() {
		for i, part := range tab.Parts {
			ui.Column(c.Key(i)).Height(320).Children(func() { render(part) })
		}
	})
}

func (a *client) closeReviewEditors() {
	id := a.editorActive
	if tab := a.activeEditor(); tab != nil && tab.Parent != "" {
		id = tab.Parent
	}
	var ids []string
	for _, tab := range a.editorTabs {
		if tab.ID == id || tab.Parent == id {
			ids = append(ids, tab.ID)
		}
	}
	for _, id := range ids {
		a.closeEditor(id)
	}
	if len(ids) == 0 {
		a.closeReview()
	}
}

func (a *client) closeOtherEditors(keep string) {
	var ids []string
	for _, tab := range a.editorTabs {
		if tab.ID != keep {
			ids = append(ids, tab.ID)
		}
	}
	for _, id := range ids {
		a.closeEditor(id)
	}
	if keep != "" {
		a.activateEditor(keep)
	}
}
func (a *client) cycleEditor(direction int) {
	if len(a.editorTabs) < 2 {
		return
	}
	for i, tab := range a.editorTabs {
		if tab.ID == a.editorActive {
			index := (i + direction + len(a.editorTabs)) % len(a.editorTabs)
			a.activateEditor(a.editorTabs[index].ID)
			return
		}
	}
}

func (a *client) editorReviewState(f FileComparison) *reviewState {
	tab := a.activeEditor()
	if tab == nil {
		return nil
	}
	if tab.DiffStates == nil {
		tab.DiffStates = map[string]*reviewState{}
	}
	state := tab.DiffStates[f.Version]
	if state == nil {
		value := newReviewState(f)
		value.SideBySide = tab.SideBySide
		state = &value
		tab.DiffStates[f.Version] = state
	}
	return state
}
func (a *client) editorContext() *editorTab {
	tab := a.activeEditor()
	if tab != nil && tab.Parent != "" {
		return a.editorByID(tab.Parent)
	}
	return tab
}
func (a *client) compareListState(index int) *ui.ListState {
	tab := a.editorContext()
	if tab == nil {
		return nil
	}
	return &tab.CompareLists[index]
}

func (tab *editorTab) setComparison(comparison FileComparison) {
	if tab.Comparison.Version != comparison.Version || tab.Comparison.Notice != comparison.Notice {
		tab.DiffStates = nil
		tab.Document.Request = ""
	}
	if tab.Code != nil && tab.Kind == "file" {
		offset := tab.Code.Cursor()
		text := strings.Join(comparison.AfterDocument.Lines, "\n")
		if tab.Code.Buffer.String() != text {
			buffer := editor.NewBuffer(text)
			buffer.SetLanguage(editor.LanguageFor(tab.File.Path))
			tab.Code.Buffer = buffer
			tab.Code.Select(offset, offset)
			tab.Caret = nil
		}
		tab.CodeSource = comparison.AfterSource
	}
	tab.Comparison = comparison
}
func (tab *editorTab) setParts(parts []CommitComparison) {
	changed := len(tab.Parts) != len(parts)
	if !changed {
		for i, part := range parts {
			if part.Comparison.Version != tab.Parts[i].Comparison.Version || part.Error != tab.Parts[i].Error {
				changed = true
				break
			}
		}
	}
	if changed {
		tab.DiffStates = nil
		tab.Document.Request = ""
	}
	tab.Parts = parts
}

func (a *client) resumeEditorReview() {
	tab := a.editorContext()
	if tab == nil || tab.Kind != "compare" || !a.review.Loading {
		return
	}
	active, mode := a.editorActive, a.compare.Mode
	a.compare.selectingCommit = a.compare.Active && a.review.Request.Kind == "commits"
	a.beginReview(a.review.Request)
	a.compare.selectingCommit = false
	a.compare.Mode = mode
	if a.editorByID(active) != nil {
		a.editorActive = active
	}
	a.saveEditorContext()
}

func (a *client) editorScopeTabs(c *ui.Context, tab *editorTab) {
	if tab.Kind == "changes" {
		ui.Row(c).Gap(4).Padding(0, 6).Children(func() {
			for _, mode := range []struct{ ID, Label string }{{"combined", "HEAD → Worktree"}, {"worktree", "Index → Worktree"}, {"index", "HEAD → Index"}} {
				ui.Button(c.Key(mode.ID), mode.Label).Selected(tab.Mode == mode.ID).Label("Diff Scope " + mode.ID).OnClick(func() { a.openEditorWorkingMode(tab.ID, mode.ID) })
			}
		})
	}
}
