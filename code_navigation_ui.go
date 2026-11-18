package main

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

type CodeNavigation struct {
	Client  *client
	Source  CodeSource
	Version string
}
type navigationState struct {
	Open, Loading           bool
	Message                 string
	Targets                 []CodeTarget
	Target                  CodeTarget
	Document                DiffDocument
	OriginID, OriginVersion string
	OriginSource            CodeSource
	OriginDocument          DiffDocument
	List                    ui.ListState
	Choice                  int
	FocusChoice             bool
	Generation              uint64
}

func (a *client) navigateSymbol(source CodeSource, document DiffDocument, ref CodeReference) {
	a.navigateSymbolAt(source, document, ref, "")
}
func (a *client) navigateSymbolAt(source CodeSource, document DiffDocument, ref CodeReference, version string) {
	a.cancelRead("symbol")
	a.codeNavigation.Generation++
	gen := a.codeNavigation.Generation
	a.codeNavigation.Open = true
	a.codeNavigation.Loading = true
	a.codeNavigation.OriginID = a.editorActive
	a.codeNavigation.OriginSource = source
	a.codeNavigation.OriginDocument = document
	a.codeNavigation.OriginVersion = version
	a.codeNavigation.Message = "Searching for " + ref.Name + " definitions…"
	a.codeNavigation.Targets = nil
	a.startRead("symbol", func(ctx context.Context) func() {
		resolution, err := resolveCode(ctx, source, document, ref)
		return func() {
			if gen != a.codeNavigation.Generation || !a.codeNavigation.Open {
				return
			}
			if a.editorActive != a.codeNavigation.OriginID {
				a.closeCodeNavigation()
				return
			}
			a.codeNavigation.Loading = false
			a.codeNavigation.Targets = resolution.Targets
			a.codeNavigation.Message = resolution.Notice
			a.codeNavigation.Choice = 0
			a.codeNavigation.List = ui.ListState{Selected: &a.codeNavigation.Choice}
			a.codeNavigation.FocusChoice = true
			if err != nil {
				a.codeNavigation.Message = err.Error()
				return
			}
			if len(resolution.Targets) == 0 {
				a.codeNavigation.Message = "No matching definition found"
				if resolution.Notice != "" {
					a.codeNavigation.Message += "; " + resolution.Notice
				}
				return
			}
			if len(resolution.Targets) == 1 {
				target := resolution.Targets[0]
				if target.Source == source {
					a.showCodeTarget(target, document)
				} else {
					a.openCodeTarget(target)
				}
			}
		}
	})
}
func (a *client) showCodeTarget(target CodeTarget, document DiffDocument) {
	a.codeNavigation.Target = target
	a.codeNavigation.Document = document
	origin := a.editorByID(a.codeNavigation.OriginID)
	same := target.Source == a.codeNavigation.OriginSource && origin != nil && a.editorActive == origin.ID
	a.codeNavigation.Open = false
	a.codeNavigation.Loading = false
	if same && a.locateEditorDefinition(origin, target, document, a.codeNavigation.OriginVersion) {
		return
	}
	a.openDefinitionEditor(target, document)
}
func (a *client) openCodeTarget(target CodeTarget) {
	if target.Source == a.codeNavigation.OriginSource {
		a.showCodeTarget(target, a.codeNavigation.OriginDocument)
		return
	}
	a.codeNavigation.Generation++
	gen := a.codeNavigation.Generation
	a.codeNavigation.Loading = true
	a.startRead("symbol", func(ctx context.Context) func() {
		document, err := readCodeSource(ctx, target.Source)
		return func() {
			if gen != a.codeNavigation.Generation || !a.codeNavigation.Open {
				return
			}
			if a.editorActive != a.codeNavigation.OriginID {
				a.closeCodeNavigation()
				return
			}
			a.codeNavigation.Loading = false
			if err != nil {
				a.codeNavigation.Message = err.Error()
				return
			}
			if document.Analysis == nil {
				a.codeNavigation.Message = "Cannot analyze syntax in the target file"
				return
			}
			var matches []CodeDefinition
			for _, def := range document.Analysis.Definitions {
				if def.Name == target.Definition.Name && def.Kind == target.Definition.Kind && def.Container == target.Definition.Container && def.Arity == target.Definition.Arity {
					matches = append(matches, def)
				}
			}
			found := false
			for _, def := range matches {
				if def.Line == target.Definition.Line {
					target.Definition = def
					found = true
					break
				}
			}
			if !found {
				if len(matches) != 1 {
					a.codeNavigation.Message = "Target definition changed. Retry navigation"
					return
				}
				target.Definition = matches[0]
			}
			a.showCodeTarget(target, document)
		}
	})
}
func (a *client) closeCodeNavigation() {
	a.codeNavigation.Open = false
	a.codeNavigation.Generation++
	a.cancelRead("symbol")
}
func (a *client) codeNavigationPanel(c *ui.Context) {
	if !a.codeNavigation.Open {
		return
	}
	if a.codeNavigation.Loading {
		c.OnWindowShortcut(0, ui.KeyEscape, a.closeCodeNavigation)
		return
	}
	ui.Modal(c.Key("code-navigation"), &a.codeNavigation.Open, func() {
		ui.Column(c).Width(680).Height(360).Padding(12).Gap(8).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "Go to Definition").FontSize(15).Bold()
				ui.Spacer(c)
				ui.Button(c, "Close").OnClick(a.closeCodeNavigation)
			})
			if a.codeNavigation.Message != "" {
				ui.Text(c, a.codeNavigation.Message).FontSize(12)
			}
			ui.Textf(c, "%d targets found. Select a target", len(a.codeNavigation.Targets)).FontSize(12)
			list := ui.List(c.Key("targets"), &a.codeNavigation.List, len(a.codeNavigation.Targets)).Label("Definition Targets").Grow(1).ItemKey(func(i int) any {
				t := a.codeNavigation.Targets[i]
				return fmt.Sprintf("%v:%d:%d", t.Source, t.Definition.Line, t.Definition.Column)
			})

			list.Rows(func(row ui.ListRow) {
				target := a.codeNavigation.Targets[row.Index]
				label := fmt.Sprintf("%s · %s · %s:%d:%d", target.Definition.Name, target.Definition.Container, target.Source.Path, target.Definition.Line, target.Definition.Column)
				ui.Button(row.Context, label).Label(label).Tooltip(target.Source.Repository + "\n" + target.Source.Revision).OnClick(func() { a.openCodeTarget(target) })
			}).OnSubmit(func() {
				index := a.codeNavigation.Choice
				if index >= 0 && index < len(a.codeNavigation.Targets) {
					a.openCodeTarget(a.codeNavigation.Targets[index])
				}
			})

			if a.codeNavigation.FocusChoice {
				a.codeNavigation.FocusChoice = !a.codeNavigation.List.Focus(c)
			}
		})
	})
}
