package main

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

type CodeNavigation struct {
	Client *client
	Source CodeSource
}
type navigationState struct {
	Open, Loading bool
	Message       string
	Targets       []CodeTarget
	Target        CodeTarget
	Document      DiffDocument
	List          ui.ListState
	Preview       bool
	Generation    uint64
}

func (a *client) navigateSymbol(source CodeSource, document DiffDocument, ref CodeReference) {
	a.cancelRead("symbol")
	a.codeNavigation.Generation++
	gen := a.codeNavigation.Generation
	a.codeNavigation.Open = true
	a.codeNavigation.Loading = true
	a.codeNavigation.Preview = false
	a.codeNavigation.Message = "正在查找 " + ref.Name + " 的定义…"
	a.codeNavigation.Targets = nil
	a.startRead("symbol", func(ctx context.Context) func() {
		resolution, err := resolveCode(ctx, source, document, ref)
		return func() {
			if gen != a.codeNavigation.Generation || !a.codeNavigation.Open {
				return
			}
			a.codeNavigation.Loading = false
			a.codeNavigation.Targets = resolution.Targets
			a.codeNavigation.Message = resolution.Notice
			if err != nil {
				a.codeNavigation.Message = err.Error()
				return
			}
			if len(resolution.Targets) == 0 {
				a.codeNavigation.Message = "未找到符合语义的定义"
				if resolution.Notice != "" {
					a.codeNavigation.Message += "；" + resolution.Notice
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
	a.codeNavigation.Loading = false
	a.codeNavigation.Preview = true
	a.codeNavigation.List = ui.ListState{}
	a.codeNavigation.List.ScrollTo(max(0, target.Definition.Line-1), ui.Center)
}
func (a *client) openCodeTarget(target CodeTarget) {
	a.codeNavigation.Generation++
	gen := a.codeNavigation.Generation
	a.codeNavigation.Loading = true
	a.startRead("symbol", func(ctx context.Context) func() {
		document, err := readCodeSource(ctx, target.Source)
		return func() {
			if gen != a.codeNavigation.Generation || !a.codeNavigation.Open {
				return
			}
			a.codeNavigation.Loading = false
			if err != nil {
				a.codeNavigation.Message = err.Error()
				return
			}
			if document.Analysis == nil {
				a.codeNavigation.Message = "目标文件无法进行语法分析"
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
					a.codeNavigation.Message = "目标定义已变化，请重新发起跳转"
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
		a.cancelRead("symbol")
		return
	}
	ui.Modal(c.Key("code-navigation"), &a.codeNavigation.Open, func() {
		ui.Column(c).Width(1000).Height(600).Padding(12).Gap(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "语法跳转").FontSize(15).Bold()
				ui.Spacer(c)
				ui.Button(c, "关闭").OnClick(a.closeCodeNavigation)
			})
			if a.codeNavigation.Message != "" {
				ui.Text(c, a.codeNavigation.Message).FontSize(12).SingleLine()
			}
			if a.codeNavigation.Loading {
				ui.Text(c, "正在解析定义…")
				return
			}
			if !a.codeNavigation.Preview {
				if len(a.codeNavigation.Targets) > 1 {
					ui.Textf(c, "找到 %d 个目标，请选择", len(a.codeNavigation.Targets))
				}
				ui.List(c, nil, len(a.codeNavigation.Targets)).Grow(1).Rows(func(row ui.ListRow) {
					target := a.codeNavigation.Targets[row.Index]
					label := fmt.Sprintf("%s · %s · %s:%d", target.Definition.Name, target.Definition.Container, target.Source.Path, target.Definition.Line)
					ui.Button(row.Context, label).Label(label).OnClick(func() { a.openCodeTarget(target) })
				})
				return
			}
			target := a.codeNavigation.Target
			revision := target.Source.Revision
			if revision == "" {
				revision = "工作区"
			} else {
				revision = shortHash(revision)
			}
			ui.Textf(c, "%s:%d · %s · %s", target.Source.Path, target.Definition.Line, target.Definition.Name, revision).FontSize(12)
			if len(a.codeNavigation.Targets) > 1 {
				ui.Button(c, "返回目标列表").OnClick(func() { a.codeNavigation.Preview = false })
			}
			document := a.codeNavigation.Document
			ui.List(c.Key(target.Source.Repository+"\x00"+target.Source.Path+"\x00"+revision), &a.codeNavigation.List, len(document.Lines)).Grow(1).Selectable().RowWidth(document.Width).Rows(func(row ui.ListRow) {
				line := ui.Row(row.Context).Height(20).Gap(10).Padding(1, 6)
				if row.Index+1 == target.Definition.Line {
					line.Background(c.Theme().SurfaceHover)
				}
				line.Children(func() {
					ui.Textf(c, "%4d", row.Index+1).Unselectable().Width(42).Font("monospace").FontSize(11)
					codeLineWithNavigation(c, document, row.Index, c.Theme().Text, &CodeNavigation{Client: a, Source: target.Source})
				})
			})
		})
	})
}
