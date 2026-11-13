package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"
)

type CodeSource struct{ Repository, Path, Revision string }
type CodeDefinition struct {
	Name, Kind, Container, Type               string
	Line, Column, ScopeStart, ScopeEnd, Arity int
}
type CodeImport struct{ Alias, Name, Path string }
type CodeReference struct {
	Name, Qualifier     string
	Line, Column, Arity int
	Call                bool
}
type CodeAnalysis struct {
	Original    []string
	TypeScopes  []codeScope
	Package     string
	Language    string
	Definitions []CodeDefinition
	Imports     []CodeImport
	Bindings    map[string]int // source line:column -> declaration index (Go lexical resolution)
	Code        []string       // comments and strings masked, preserving byte positions
}

var identifierPattern = regexp.MustCompile(`[\pL_$][\pL\pN_$]*`)

func bindingKey(line, column int) string { return stringKey(line, column) }
func stringKey(a, b int) string          { return strconv.Itoa(a) + ":" + strconv.Itoa(b) }
func analyzeDocument(document DiffDocument, filename string) *CodeAnalysis {
	if len(languageGlobs(document.Language)) == 0 {
		return nil
	}
	analysis := &CodeAnalysis{Language: document.Language, Original: document.Lines, Bindings: map[string]int{}}
	analysis.Code = make([]string, len(document.Lines))
	for i, line := range document.Lines {
		masked := []byte(line)
		start := 0
		if i < len(document.Tokens) {
			for _, token := range document.Tokens[i] {
				end := min(token.End, len(masked))
				if token.Kind == editor.TokenComment || token.Kind == editor.TokenString || token.Kind == editor.TokenRegexp {
					for j := start; j < end; j++ {
						masked[j] = ' '
					}
				}
				start = end
			}
		}
		analysis.Code[i] = string(masked)
	}
	if document.Language == "Go" {
		analyzeGo(analysis, strings.Join(document.Lines, "\n"), filename)
	} else {
		analyzeGeneric(analysis, document)
	}
	return analysis
}
func (a *CodeAnalysis) reference(line, column int) (CodeReference, bool) {
	if line < 0 || line >= len(a.Code) {
		return CodeReference{}, false
	}
	text := a.Code[line]
	for _, loc := range identifierPattern.FindAllStringIndex(text, -1) {
		if column < loc[0] || column >= loc[1] {
			continue
		}
		name := text[loc[0]:loc[1]]
		if codeKeywords[name] {
			return CodeReference{}, false
		}
		ref := CodeReference{Name: name, Line: line + 1, Column: loc[0] + 1, Arity: -1}
		prefix := strings.TrimSpace(text[:loc[0]])
		if strings.HasSuffix(prefix, ".") || strings.HasSuffix(prefix, "::") || strings.HasSuffix(prefix, "->") {
			prefix = strings.TrimRight(prefix, ".>:-? ")
			matches := identifierPattern.FindAllString(prefix, -1)
			if len(matches) > 0 {
				ref.Qualifier = matches[len(matches)-1]
			}
		}
		rest := strings.TrimSpace(text[loc[1]:])
		ref.Call = strings.HasPrefix(rest, "(")
		if ref.Call {
			ref.Arity = argumentCount(strings.TrimSpace(a.Original[line][loc[1]:]))
		}
		return ref, true
	}
	return CodeReference{}, false
}
func argumentCount(text string) int {
	depth, count, has := 0, 0, false
	var quote rune
	escaped := false
	for _, r := range text {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' || r == '`' {
			quote = r
			has = true
			continue
		}
		switch r {
		case '(', '[', '{':
			if depth > 0 {
				has = true
			}
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if !has {
					return 0
				}
				return count + 1
			}
		case ',':
			if depth == 1 {
				count++
			}
		default:
			if depth == 1 && !unicode.IsSpace(r) {
				has = true
			}
		}
	}
	return -1
}

func (a *CodeAnalysis) localTargets(ref CodeReference) []CodeDefinition {
	if index, ok := a.Bindings[bindingKey(ref.Line, ref.Column)]; ok && ref.Qualifier == "" {
		return []CodeDefinition{a.Definitions[index]}
	}
	expected := a.receiverType(ref)
	for _, imp := range a.Imports {
		if imp.Alias == ref.Qualifier && ref.Qualifier != "" {
			return nil
		}
	}
	bestScope := -1
	var result []CodeDefinition
	for _, def := range a.Definitions {
		if def.Name != ref.Name || ref.Line < def.ScopeStart || ref.Line > def.ScopeEnd {
			continue
		}
		if ref.Qualifier != "" {
			if expected == "" || def.Container != expected {
				continue
			}
		} else if def.Container != "" && (def.Kind == "method" || def.Kind == "field") {
			owner := ""
			for _, scope := range a.TypeScopes {
				if scope.start <= ref.Line && ref.Line <= scope.end {
					owner = scope.name
				}
			}
			if owner != def.Container {
				continue
			}
		}
		if def.ScopeStart > 0 && def.Line > ref.Line && def.Kind == "variable" {
			continue
		}
		if ref.Call && def.Kind != "function" && def.Kind != "method" && def.Kind != "type" {
			continue
		}
		if ref.Call && ref.Arity >= 0 && def.Arity >= 0 && ref.Arity != def.Arity {
			continue
		}
		if def.ScopeStart > bestScope {
			result = nil
			bestScope = def.ScopeStart
		}
		if def.ScopeStart == bestScope {
			result = append(result, def)
		}
	}
	return result
}
func (a *CodeAnalysis) receiverType(ref CodeReference) string {
	if ref.Qualifier == "" {
		return ""
	}
	if ref.Qualifier == "this" || ref.Qualifier == "self" {
		for _, scope := range a.TypeScopes {
			if scope.start <= ref.Line && ref.Line <= scope.end {
				return scope.name
			}
		}
		for _, def := range a.Definitions {
			if def.ScopeStart <= ref.Line && ref.Line <= def.ScopeEnd && def.Container != "" {
				return def.Container
			}
		}
	}
	best := -1
	name := ""
	for _, def := range a.Definitions {
		if def.Name == ref.Qualifier && def.ScopeStart <= ref.Line && ref.Line <= def.ScopeEnd && def.ScopeStart >= best {
			best = def.ScopeStart
			name = def.Type
			if def.Kind == "type" {
				name = def.Name
			}
		}
	}
	if name == "" {
		r, _ := utf8.DecodeRuneInString(ref.Qualifier)
		if unicode.IsUpper(r) {
			name = ref.Qualifier
		}
	}
	return strings.TrimPrefix(name, "*")
}

var codeKeywords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("if else for foreach while switch case return new throw catch try finally using import package namespace public private protected internal static async await class interface enum struct record type func function def var let const true false null nil this self super base break continue void int string bool boolean float double long short byte char decimal get set extends implements override virtual abstract sealed readonly final export default from as in is not and or pass with yield do sizeof typeof nameof make len append range go defer select chan map error") {
		m[w] = true
	}
	return m
}()
