package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sort"
)

func highlightDocument(document *DiffDocument, filename string) {
	defer func() { document.Analysis = analyzeDocument(*document, filename) }()
	language := editor.LanguageFor(filename)
	if language == nil || language.Tokenizer == nil {
		return
	}
	document.Language = language.Name
	document.Tokens = make([][]editor.Token, len(document.Lines))
	var state uint64
	for i, line := range document.Lines {
		document.Tokens[i], state = language.Tokenizer.Tokenize(line, state, nil)
	}
}
func tokenColor(kind editor.TokenKind, base ui.Color) ui.Color {
	if int(base.R)+int(base.G)+int(base.B) > 500 {
		switch kind {
		case editor.TokenKeyword, editor.TokenTag, editor.TokenMeta:
			return ui.RGB(204, 120, 50)
		case editor.TokenType:
			return ui.RGB(169, 183, 198)
		case editor.TokenFunction:
			return ui.RGB(255, 198, 109)
		case editor.TokenString, editor.TokenEscape, editor.TokenRegexp:
			return ui.RGB(144, 179, 106)
		case editor.TokenNumber, editor.TokenConstant:
			return ui.RGB(104, 151, 187)
		case editor.TokenComment:
			return ui.RGB(128, 128, 128)
		default:
			return base
		}
	}
	switch kind {
	case editor.TokenKeyword, editor.TokenTag, editor.TokenMeta:
		return ui.RGB(143, 55, 164)
	case editor.TokenType, editor.TokenFunction:
		return ui.RGB(31, 91, 170)
	case editor.TokenString, editor.TokenEscape, editor.TokenRegexp:
		return ui.RGB(24, 116, 70)
	case editor.TokenNumber, editor.TokenConstant:
		return ui.RGB(153, 83, 22)
	case editor.TokenComment:
		return ui.RGB(108, 118, 130)
	case editor.TokenInvalid:
		return ui.RGB(193, 44, 44)
	default:
		return base
	}
}
func codeLine(c *ui.Context, document DiffDocument, line int, colors ...ui.Color) {
	base := c.Theme().Text
	if len(colors) > 0 {
		base = colors[0]
	}
	codeLineWithNavigation(c, document, line, base, nil)
}
func codeLineWithNavigation(c *ui.Context, document DiffDocument, line int, base ui.Color, navigation *CodeNavigation) {
	text := document.Lines[line]
	if (navigation == nil || navigation.Client == nil || document.Analysis == nil) && len(document.Changes) == 0 {
		spans := []ui.Span{}
		start := 0
		if line < len(document.Tokens) {
			for _, token := range document.Tokens[line] {
				end := min(token.End, len(text))
				if end > start {
					spans = append(spans, ui.Span{Text: text[start:end], Color: tokenColor(token.Kind, base)})
					start = end
				}
			}
		}
		if start < len(text) {
			spans = append(spans, ui.Span{Text: text[start:], Color: base})
		}
		ui.RichText(c, spans...).Selectable().TextColor(base).Font("monospace").FontSize(12).SingleLine()
		return
	}
	ui.RichText(c).Selectable().TextColor(base).Font("monospace").FontSize(12).SingleLine().Children(func() {
		// Inline text retains exact shaped hit areas, including CJK and horizontal scrolling.
		boundaries := []int{0}
		if change, ok := document.Changes[line]; ok {
			boundaries = append(boundaries, change.Start, change.End)
		}
		for _, loc := range identifierPattern.FindAllStringIndex(text, -1) {
			boundaries = append(boundaries, loc[0], loc[1])
		}
		if line < len(document.Tokens) {
			for _, token := range document.Tokens[line] {
				boundaries = append(boundaries, min(token.End, len(text)))
			}
		}
		boundaries = append(boundaries, len(text))
		sort.Ints(boundaries)
		for i := 0; i+1 < len(boundaries); i++ {
			start, end := boundaries[i], boundaries[i+1]
			if start == end {
				continue
			}
			color := base
			offset := 0
			if line < len(document.Tokens) {
				for _, token := range document.Tokens[line] {
					if start >= offset && start < token.End {
						color = tokenColor(token.Kind, base)
						break
					}
					offset = token.End
				}
			}
			// Split non-identifiers by syntax boundaries too, so strings/comments keep their colors.
			part := ui.Text(c.Key(start), text[start:end]).TextColor(color)
			if navigation != nil && navigation.Client != nil {
				if caret := navigation.Client.editorCaretAt(navigation.Source, navigation.Version, line+1, start+1); caret != nil {
					accent := ideaLink
					part.Focusable().FocusRing(false)
					drawCaret := part.Focused() || caret.PendingFocus
					part.Background(ui.Hex("#394b5a")).DrawOver(func(p *ui.Painter, r ui.Rect) {
						if drawCaret {
							p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, accent, 0)
						}
					})
					if caret.PendingFocus {
						part.Focusable().Focus()
						caret.PendingFocus = false
					}
				}
			}
			if change, ok := document.Changes[line]; ok && start < change.End && end > change.Start {
				part.Background(ui.Hex("#66532d"))
			}
			var ref CodeReference
			ok := false
			if navigation != nil && navigation.Client != nil && document.Analysis != nil {
				ref, ok = document.Analysis.reference(line, start)
			}
			if ok && ref.Column == start+1 && ref.Name == text[start:end] {
				hovered := part.Hovered()
				if definitionModifiers(c.Modifiers(), primaryShortcutModifier()) {
					part.Cursor(ui.CursorPointer)
					if hovered {
						part.TextColor(c.Theme().Accent).Underline()
					}
				}
				part.Label(fmt.Sprintf("Go to %s:%d:%d %s", navigation.Source.Path, line+1, start+1, ref.Name)).Tooltip(definitionClickHint()).HandleInput(func(event ui.InputEvent) bool {
					// Capture only the navigation gesture; ordinary text selection and Copy stay native.
					if isDefinitionPress(event) {
						return true
					}
					if event.Kind == ui.InputPointerUp && event.Button == 0 && !event.Canceled && hovered && definitionModifiers(event.Mods, primaryShortcutModifier()) {
						navigation.Client.navigateSymbolAt(navigation.Source, document, ref, navigation.Version)
						return true
					}
					return false
				})
			}
		}
	})
}

func isDefinitionPress(event ui.InputEvent) bool {
	return event.Kind == ui.InputPointerDown && event.Button == 0 && definitionModifiers(event.Mods, primaryShortcutModifier())
}

// Additional modifiers belong to selection and alternate IDE gestures, not definition navigation.
func definitionModifiers(mods, primary ui.Modifiers) bool {
	return mods&(ui.Ctrl|ui.Super|ui.Alt|ui.Shift) == primary
}

func definitionClickHint() string {
	if primaryShortcutModifier() == ui.Super {
		return "⌘+Click to Go to Definition"
	}
	return "Ctrl+Click to Go to Definition"
}
