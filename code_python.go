package main

import (
	"regexp"
	"strings"
)

var pythonFunction = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([\pL_][\pL\pN_]*)\s*\(([^)]*)\)`)
var pythonAssignment = regexp.MustCompile(`^\s*(?:(self)\.)?([\pL_][\pL\pN_]*)(?:\s*:\s*([\w.]+))?\s*=`)
var pythonFrom = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import\s+(.+)`)

func analyzePython(a *CodeAnalysis, document DiffDocument) {
	var functions []codeScope
	for i, text := range a.Code {
		if match := typeDeclaration.FindStringSubmatchIndex(text); match != nil {
			name := text[match[4]:match[5]]
			a.TypeScopes = append(a.TypeScopes, codeScope{start: i + 1, end: pythonScopeEnd(a.Code, i), name: name})
			a.Definitions = append(a.Definitions, CodeDefinition{Name: name, Kind: "type", Type: name, Line: i + 1, Column: match[4] + 1, ScopeEnd: len(a.Code) + 1, Arity: -1})
		}
		match := pythonFunction.FindStringSubmatchIndex(text)
		if match == nil {
			continue
		}
		owner := ""
		for _, scope := range a.TypeScopes {
			if scope.start <= i+1 && i+1 <= scope.end {
				owner = scope.name
			}
		}
		name := text[match[2]:match[3]]
		end := pythonScopeEnd(a.Code, i)
		kind := "function"
		if owner != "" {
			kind = "method"
		}
		scopeStart := 0
		for _, fn := range functions {
			if fn.start <= i+1 && i+1 <= fn.end {
				scopeStart = fn.start
			}
		}
		arity := 0
		params := strings.Split(text[match[4]:match[5]], ",")
		for _, param := range params {
			param = strings.TrimSpace(param)
			if param == "" {
				continue
			}
			parts := strings.FieldsFunc(param, func(r rune) bool { return r == ':' || r == '=' })
			id := strings.TrimSpace(parts[0])
			if id == "self" || id == "cls" {
				continue
			}
			typ := ""
			if len(parts) > 1 {
				typ = strings.TrimSpace(parts[1])
			}
			a.Definitions = append(a.Definitions, CodeDefinition{Name: id, Kind: "parameter", Type: typ, Container: owner, Line: i + 1, Column: strings.Index(text, id) + 1, ScopeStart: i + 1, ScopeEnd: end, Arity: -1})
			arity++
		}
		a.Definitions = append(a.Definitions, CodeDefinition{Name: name, Kind: kind, Container: owner, Line: i + 1, Column: match[2] + 1, ScopeStart: scopeStart, ScopeEnd: len(a.Code) + 1, Arity: arity})
		functions = append(functions, codeScope{start: i + 1, end: end})
	}
	for i, text := range a.Code {
		if match := pythonAssignment.FindStringSubmatchIndex(text); match != nil {
			name, typ := text[match[4]:match[5]], ""
			if match[6] >= 0 {
				typ = text[match[6]:match[7]]
			}
			if guess := assignmentType.FindStringSubmatch(text); typ == "" && guess != nil {
				typ = guess[1]
			}
			start, end := 0, len(a.Code)+1
			for _, fn := range functions {
				if fn.start <= i+1 && i+1 <= fn.end && fn.start >= start {
					start, end = fn.start, fn.end
				}
			}
			owner, kind := "", "variable"
			if match[2] >= 0 {
				kind = "field"
				start, end = 0, len(a.Code)+1
				for _, scope := range a.TypeScopes {
					if scope.start <= i+1 && i+1 <= scope.end {
						owner = scope.name
					}
				}
			}
			a.Definitions = append(a.Definitions, CodeDefinition{Name: name, Kind: kind, Type: typ, Container: owner, Line: i + 1, Column: match[4] + 1, ScopeStart: start, ScopeEnd: end, Arity: -1})
		}
		if imp := pythonFrom.FindStringSubmatch(text); imp != nil {
			for _, part := range strings.Split(imp[2], ",") {
				fields := strings.Fields(part)
				if len(fields) == 0 {
					continue
				}
				alias := fields[0]
				if len(fields) == 3 && fields[1] == "as" {
					alias = fields[2]
				}
				a.Imports = append(a.Imports, CodeImport{Alias: alias, Name: fields[0], Path: strings.ReplaceAll(imp[1], ".", "/")})
			}
		}
	}
}
