package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type editorCaret struct {
	Source       CodeSource
	Version      string
	Line, Column int
	PendingFocus bool
}

func (a *client) editorCaretLine(source CodeSource, version string, line int) bool {
	tab := a.activeEditor()
	return tab != nil && tab.Caret != nil && tab.Caret.Source == source && tab.Caret.Line == line && (tab.Caret.Version == "" || tab.Caret.Version == version)
}
func (a *client) editorCaretAt(source CodeSource, version string, line, column int) *editorCaret {
	if !a.editorCaretLine(source, version, line) {
		return nil
	}
	caret := a.activeEditor().Caret
	if caret.Column == column {
		return caret
	}
	return nil
}
func documentOffset(document DiffDocument, line, column int) int {
	offset := 0
	for i := 0; i < min(max(0, line-1), len(document.Lines)); i++ {
		offset += len(document.Lines[i]) + 1
	}
	if line > 0 && line <= len(document.Lines) {
		offset += min(max(0, column-1), len(document.Lines[line-1]))
	}
	return offset
}
func (a *client) setCodeCaret(tab *editorTab, target CodeTarget, document DiffDocument) {
	text := strings.Join(document.Lines, "\n")
	if tab.Code == nil || tab.CodeSource != target.Source || tab.Code.Buffer.String() != text {
		buffer := editor.NewBuffer(text)
		buffer.SetLanguage(editor.LanguageFor(target.Source.Path))
		tab.Code = &editor.State{Buffer: buffer, ReadOnly: true, Label: "Source code " + target.Source.Path}
	}
	tab.CodeSource = target.Source
	offset := documentOffset(document, target.Definition.Line, target.Definition.Column)
	tab.Code.Select(offset, offset)
	tab.Caret = &editorCaret{Source: target.Source, Line: target.Definition.Line, Column: target.Definition.Column, PendingFocus: true}
}
func (a *client) locateEditorDefinition(tab *editorTab, target CodeTarget, document DiffDocument, version string) bool {
	if tab.Kind == "file" && tab.Comparison.AfterSource == target.Source {
		a.setCodeCaret(tab, target, document)
		return true
	}
	var comparisons []FileComparison
	if tab.Kind == "changes" {
		comparisons = []FileComparison{tab.Comparison}
	} else if tab.Kind == "commit" {
		for _, part := range tab.Parts {
			comparisons = append(comparisons, part.Comparison)
		}
	}
	for _, f := range comparisons {
		if version != "" && version != f.Version {
			continue
		}
		old := f.BeforeSource == target.Source
		if !old && f.AfterSource != target.Source {
			continue
		}
		state := a.editorReviewState(f)
		// A declaration outside a changed hunk must become visible before scrolling.
		line := target.Definition.Line - 1
		for i, row := range f.ReviewRows {
			if row.Kind == 'f' && ((old && line >= row.OldLine && line < row.OldEnd) || (!old && line >= row.NewLine && line < row.NewEnd)) {
				state.Expanded[i] = true
			}
		}
		state.rebuildRows(f)
		for i, row := range state.Rows {
			if row.Kind != 'f' && row.Kind != 'h' && ((old && row.OldLine == line) || (!old && row.NewLine == line)) {
				state.List.ScrollTo(i, ui.Center)
				break
			}
		}
		for i, pair := range state.Pairs {
			row := pair.New
			if old {
				row = pair.Old
			}
			if row != nil && row.Kind != 'f' && row.Kind != 'h' && ((old && row.OldLine == line) || (!old && row.NewLine == line)) {
				state.SideLists[0].ScrollTo(i, ui.Center)
				state.SideLists[1].ScrollTo(i, ui.Center)
				break
			}
		}
		tab.Caret = &editorCaret{Source: target.Source, Version: f.Version, Line: target.Definition.Line, Column: target.Definition.Column, PendingFocus: true}
		return true
	}
	return false
}
func (a *client) openDefinitionEditor(target CodeTarget, document DiffDocument) {
	source := target.Source
	key := "source:" + source.Repository + "\x00" + source.Path + "\x00" + source.Revision
	if source.Revision == "" {
		key = "file:" + workingFileKey(source.Repository, source.Path) + "\x00file"
	}
	tab := a.openEditor(key, filepath.Base(source.Path)+" · "+sourceRevisionLabel(source.Revision), "file", "")
	repo := Repository{Path: source.Repository, Name: filepath.Base(source.Repository)}
	for _, r := range a.repos {
		if r.Path == source.Repository {
			repo = r
			break
		}
	}
	tab.Tree.Repository = repo
	tab.File = WorkingFile{Path: source.Path}
	tab.Mode = "file"
	tab.Loading = false
	tab.Error = ""
	tab.Comparison = FileComparison{Filename: source.Path, After: strings.Join(document.Lines, "\n"), AfterSource: source, AfterDocument: document}
	a.setCodeCaret(tab, target, document)
	a.restoreEditorContext()
}
func sourceRevisionLabel(revision string) string {
	if revision == "" {
		return "Worktree"
	}
	if revision == ":" {
		return "Index"
	}
	return shortHash(revision)
}
func (a *client) definitionSourcePanel(c *ui.Context, tab *editorTab) {
	document := tab.Comparison.AfterDocument
	source := tab.CodeSource
	tab.Code.OnDefinition = func(offset int) {
		line, column := 0, offset
		for line < len(document.Lines) && column > len(document.Lines[line]) {
			column -= len(document.Lines[line]) + 1
			line++
		}
		if document.Analysis != nil {
			if ref, ok := document.Analysis.reference(line, column); ok {
				a.navigateSymbol(source, document, ref)
			}
		}
	}
	ui.Text(c, fmt.Sprintf("%s · %s", sourceRevisionLabel(source.Revision), source.Path)).FontSize(11).Padding(4, 8).TextColor(c.Theme().TextMuted)
	view := editor.View(c.Key("source-editor"), tab.Code).Grow(1).FillWidth().MinHeight(0)
	if tab.Caret != nil && tab.Caret.PendingFocus {
		view.Focus()
		tab.Caret.PendingFocus = false
	}
}

func (a *client) reloadDefinitionEditor(id string) {
	tab := a.editorByID(id)
	if tab == nil {
		return
	}
	source := tab.CodeSource
	tab.Loading = true
	tab.Error = ""
	a.startRead("editor:"+id, func(ctx context.Context) func() {
		document, err := readCodeSource(ctx, source)
		return func() {
			tab := a.editorByID(id)
			if tab == nil {
				return
			}
			tab.Loading = false
			if err != nil {
				tab.Error = err.Error()
				return
			}
			tab.setComparison(FileComparison{Filename: source.Path, After: strings.Join(document.Lines, "\n"), AfterSource: source, AfterDocument: document})
		}
	})
}
