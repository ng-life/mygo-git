package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"strings"
)

type documentPreview struct {
	Mode, Request, Error, BeforeHTML, AfterHTML      string
	BeforeHash, AfterHash, LoadedBefore, LoadedAfter string
	Loading                                          bool
	Part                                             int
	Split                                            float32
	ShowBefore                                       bool
	Before, After                                    *mygo.WebView
}

func (a *client) refreshEditorPreview(id string) {
	tab := a.editorByID(id)
	if tab == nil || !isPreviewDocument(tab.File.Path) || tab.Loading {
		return
	}
	var f FileComparison
	if tab.Kind == "commit" {
		if len(tab.Parts) == 0 {
			return
		}
		tab.Document.Part = min(tab.Document.Part, len(tab.Parts)-1)
		if problem := tab.Parts[tab.Document.Part].Error; problem != "" {
			tab.Document.Error = problem
			tab.Document.Loading = false
			if tab.Document.Mode == "" {
				tab.Document.Mode = "preview"
			}
			return
		}
		f = tab.Parts[tab.Document.Part].Comparison
	} else {
		f = tab.Comparison
	}
	if tab.Document.Mode == "" {
		tab.Document.Mode = "preview"
	}
	if f.Notice != "" {
		tab.Document.Error = f.Notice
		tab.Document.Loading = false
		return
	}
	request := f.Version + fmt.Sprint(":", tab.Document.Part)
	if tab.Document.Request == request {
		return
	}
	tab.Document.Request = request
	tab.Document.Loading = true
	tab.Document.Error = ""
	if tab.Document.Mode == "" {
		tab.Document.Mode = "preview"
	}
	beforeSource, afterSource := f.BeforeSource, f.AfterSource
	if beforeSource.Path == "" {
		beforeSource.Path = tab.File.Path
	}
	if afterSource.Path == "" {
		afterSource.Path = tab.File.Path
	}
	a.startRead("preview:"+id, func(ctx context.Context) func() {
		before, e1 := renderDocumentPreview(ctx, beforeSource, f.Before)
		after, e2 := renderDocumentPreview(ctx, afterSource, f.After)
		beforeHash, afterHash := fmt.Sprintf("%x", sha256.Sum256([]byte(before))), fmt.Sprintf("%x", sha256.Sum256([]byte(after)))
		return func() {
			tab := a.editorByID(id)
			if tab == nil || tab.Document.Request != request {
				return
			}
			tab.Document.Loading = false
			if e1 != nil {
				tab.Document.Error = e1.Error()
			} else if e2 != nil {
				tab.Document.Error = e2.Error()
			} else {
				tab.Document.BeforeHTML = before
				tab.Document.AfterHTML = after
				tab.Document.BeforeHash, tab.Document.AfterHash = beforeHash, afterHash
				a.ensurePreviewViews(tab)
			}
		}
	})
}
func (a *client) ensurePreviewViews(tab *editorTab) {
	if a.win == nil || tab.Document.Mode == "source" || tab.Document.Loading || tab.Document.Error != "" {
		return
	}
	create := func(slot **mygo.WebView, content, hash string, loaded *string) {
		if *slot != nil && *loaded == hash {
			return
		}
		if *slot == nil {
			view, err := a.win.NewWebView(mygo.WebViewOptions{Page: mygo.PageOptions{Untrusted: true, Session: mygo.NewEphemeralSession()}, BackgroundColor: "#2b2b2b"})
			if err != nil {
				tab.Document.Error = err.Error()
				return
			}
			view.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
				if e.UserInitiated && !strings.HasPrefix(e.URL, "about:blank#") {
					e.PreventDefault()
				}
			})
			*slot = view
		}
		(*slot).Page().LoadHTML(content, "")
		*loaded = hash
	}
	create(&tab.Document.After, tab.Document.AfterHTML, tab.Document.AfterHash, &tab.Document.LoadedAfter)
	if tab.Kind != "file" {
		create(&tab.Document.Before, tab.Document.BeforeHTML, tab.Document.BeforeHash, &tab.Document.LoadedBefore)
	}
}
func (a *client) documentEditorPanel(c *ui.Context, tab *editorTab, source func()) bool {
	if !isPreviewDocument(tab.File.Path) {
		return false
	}
	ui.Row(c).Height(30).Padding(2, 6).Gap(4).Children(func() {
		for _, mode := range []struct{ ID, Label string }{{"preview", "Preview"}, {"split", "Split"}, {"source", "Source"}} {
			ui.Button(c.Key(mode.ID), mode.Label).Label("Document Mode " + mode.ID).Selected(tab.Document.Mode == mode.ID).OnClick(func() { tab.Document.Mode = mode.ID; a.ensurePreviewViews(tab) })
		}
		ui.Text(c, tab.Tree.Repository.Name+" · "+tab.File.Path).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(11).TextColor(c.Theme().TextMuted).Tooltip(tab.File.Path)
		ui.Button(c, "Refresh").Label("Refresh Document").OnClick(func() {
			tab.Document.Request = ""
			if tab.Kind == "file" || tab.Kind == "changes" {
				a.loadEditorWorkingMode(tab.ID, tab.Mode)
			} else {
				a.refreshEditorPreview(tab.ID)
			}
		})
		if len(tab.Parts) > 1 {
			ui.Spacer(c)
			ui.Button(c, "‹").Label("Previous Preview Revision").Disabled(tab.Document.Part == 0).OnClick(func() { tab.Document.Part--; a.refreshEditorPreview(tab.ID) })
			ui.Textf(c, "Revision %d / %d", tab.Document.Part+1, len(tab.Parts)).FontSize(11)
			ui.Button(c, "›").Label("Next Preview Revision").Disabled(tab.Document.Part+1 == len(tab.Parts)).OnClick(func() { tab.Document.Part++; a.refreshEditorPreview(tab.ID) })
		}
	})
	if tab.Document.Mode == "source" {
		source()
		return true
	}
	pane := func(before bool) {
		ui.Column(c).Grow(1).FillHeight().MinWidth(0).Gap(2).Children(func() {
			if tab.Kind != "file" {
				label := "After"
				if before {
					label = "Before"
				}
				ui.Text(c, label).FontSize(11).Padding(4, 8).TextColor(c.Theme().TextMuted)
			}
			if tab.Document.Loading {
				ui.Text(c, "Preparing preview…").Padding(12)
				return
			}
			if tab.Document.Error != "" {
				ui.Text(c, tab.Document.Error).Padding(12)
				return
			}
			view := tab.Document.After
			if before {
				view = tab.Document.Before
			}
			if view != nil {
				ui.WebView(c, view).Grow(1).FillWidth()
			} else {
				ui.Text(c, "Document Preview").Padding(12).TextColor(c.Theme().TextMuted)
			}
		})
	}
	if tab.Document.Mode == "split" {
		if tab.Document.Split == 0 {
			tab.Document.Split = 50
		}
		ui.SplitPercent(c.Key("document-split"), &tab.Document.Split, source, func() {
			ui.Column(c).Fill().Children(func() {
				if tab.Kind != "file" {
					ui.Row(c).Gap(4).Children(func() {
						ui.Button(c, "Before").Selected(tab.Document.ShowBefore).OnClick(func() { tab.Document.ShowBefore = true })
						ui.Button(c, "After").Selected(!tab.Document.ShowBefore).OnClick(func() { tab.Document.ShowBefore = false })
					})
				}
				pane(tab.Document.ShowBefore)
			})
		}).Grow(1).FillWidth().MinHeight(0)
	} else {
		ui.Row(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Gap(1).Children(func() {
			if tab.Kind != "file" {
				pane(true)
			}
			pane(false)
		})
	}
	return true
}
