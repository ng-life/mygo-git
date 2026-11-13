package main

import (
	"regexp"
	"strings"
)

var typeDeclaration = regexp.MustCompile(`\b(class|interface|struct|enum|record|type)\s+([\pL_$][\pL\pN_$]*)`)
var functionDeclaration = regexp.MustCompile(`\b(?:function|def)\s+([\pL_$][\pL\pN_$]*)\s*\(`)
var methodDeclaration = regexp.MustCompile(`(?:^|[;}])\s*(?:(?:public|private|protected|internal|static|async|virtual|override|abstract|final|sealed|export)\s+)*(?:[\w.$<>?\[\],]+\s+)([\pL_$][\pL\pN_$]*)\s*\(`)
var variableDeclaration = regexp.MustCompile(`\b(?:var|let|const)\s+([\pL_$][\pL\pN_$]*)(?:\s*:\s*([\w.]+))?`)
var typedDeclaration = regexp.MustCompile(`\b([\pL_$][\pL\pN_$.<>?\[\]]*)\s+([\pL_$][\pL\pN_$]*)\s*(?:=|;|,|\))`)
var arrowDeclaration = regexp.MustCompile(`\b(?:const|let|var)\s+([\pL_$][\pL\pN_$]*)\s*=\s*(?:async\s*)?(\([^)]*\)|[\pL_$][\pL\pN_$]*)\s*=>`)
var assignmentType = regexp.MustCompile(`=\s*(?:new\s+)?([\pL_$][\pL\pN_$.]*)\s*[(\{]`)
var importFrom = regexp.MustCompile(`(?:from\s+|import\s*|require\s*\()\s*["']([^"']+)["']`)
var namespaceImport = regexp.MustCompile(`^\s*(?:using|import)\s+(?:static\s+)?([\w.]+)\s*;?`)

type codeScope struct {
	start, end int
	name       string
}

func analyzeGeneric(a *CodeAnalysis, document DiffDocument) {
	if a.Language == "Python" {
		analyzePython(a, document)
		return
	}
	var scopes []codeScope
	var braceStack []int
	for line, text := range a.Code {
		for _, ch := range text {
			if ch == '{' {
				braceStack = append(braceStack, line+1)
			}
			if ch == '}' && len(braceStack) > 0 {
				start := braceStack[len(braceStack)-1]
				braceStack = braceStack[:len(braceStack)-1]
				scopes = append(scopes, codeScope{start: start, end: line + 1})
			}
		}
	}
	for _, start := range braceStack {
		scopes = append(scopes, codeScope{start: start, end: len(a.Code) + 1})
	}
	scopeAt := func(line int) (int, int) {
		start, end := 0, len(a.Code)+1
		for _, scope := range scopes {
			if scope.start <= line && line <= scope.end && scope.start >= start {
				start, end = scope.start, scope.end
			}
		}
		return start, end
	}
	var types []codeScope
	for i, text := range a.Code {
		if match := typeDeclaration.FindStringSubmatchIndex(text); match != nil {
			name := text[match[4]:match[5]]
			start, end := i+1, len(a.Code)+1
			braceLine := -1
			for j := i; j < len(a.Code) && j < i+8; j++ {
				if strings.Contains(a.Code[j], "{") {
					braceLine = j + 1
					break
				}
			}
			if braceLine > 0 {
				start = braceLine
				end = braceLine
				for _, scope := range scopes {
					if scope.start == braceLine && scope.end > end {
						end = scope.end
					}
				}
			}
			if a.Language == "Python" {
				start = i + 1
				end = pythonScopeEnd(a.Code, i)
			}
			types = append(types, codeScope{start: start, end: end, name: name})
			a.Definitions = append(a.Definitions, CodeDefinition{Name: name, Kind: "type", Type: name, Line: i + 1, Column: match[4] + 1, ScopeEnd: len(a.Code) + 1, Arity: -1})
		}
	}
	a.TypeScopes = types
	for i, text := range a.Code {
		line := i + 1
		start, end := scopeAt(line)
		container := ""
		for _, typ := range types {
			if typ.start <= line && line <= typ.end {
				container = typ.name
			}
		}
		add := func(name string, column int, kind, typ string, arity int) {
			if codeKeywords[name] {
				return
			}
			a.Definitions = append(a.Definitions, CodeDefinition{Name: name, Kind: kind, Type: typ, Container: container, Line: line, Column: column + 1, ScopeStart: start, ScopeEnd: end, Arity: arity})
		}
		if a.Language == "JavaScript" || a.Language == "TypeScript" {
			if arrow := arrowDeclaration.FindStringSubmatchIndex(text); arrow != nil {
				savedStart, savedEnd := start, end
				start, end = 0, len(a.Code)+1
				for _, scope := range scopes {
					if scope.start < line && line <= scope.end && scope.start >= start {
						start, end = scope.start, scope.end
					}
				}
				parameters := text[arrow[4]:arrow[5]]
				arity := 1
				if strings.HasPrefix(parameters, "(") {
					arity = argumentCount(parameters)
				}
				add(text[arrow[2]:arrow[3]], arrow[2], "function", "", arity)
				start, end = savedStart, savedEnd
			}
		}
		match := functionDeclaration.FindStringSubmatchIndex(text)
		if match == nil {
			match = methodDeclaration.FindStringSubmatchIndex(text)
		}
		if match != nil {
			name := text[match[2]:match[3]]
			kind := "function"
			if container != "" {
				kind = "method"
			}
			// Method declarations are visible throughout their type, not only their body.
			savedStart, savedEnd := start, end
			start, end = 0, len(a.Code)+1
			add(name, match[2], kind, "", argumentCount(text[strings.Index(text[match[3]:], "(")+match[3]:]))
			start, end = savedStart, savedEnd
		}
		for _, match := range variableDeclaration.FindAllStringSubmatchIndex(text, -1) {
			typ := ""
			if match[4] >= 0 {
				typ = text[match[4]:match[5]]
			}
			if inferred := assignmentType.FindStringSubmatch(text[match[3]:]); typ == "" && inferred != nil {
				typ = inferred[1]
			}
			add(text[match[2]:match[3]], match[2], "variable", typ, -1)
		}
		for _, match := range typedDeclaration.FindAllStringSubmatchIndex(text, -1) {
			typ, name := text[match[2]:match[3]], text[match[4]:match[5]]
			if codeKeywords[typ] && typ != "int" && typ != "string" && typ != "bool" {
				continue
			}
			kind := "variable"
			for _, owner := range types {
				if owner.name == container && start == owner.start {
					kind = "field"
				}
			}
			if kind == "field" {
				savedStart, savedEnd := start, end
				start, end = 0, len(a.Code)+1
				add(name, match[4], kind, typ, -1)
				start, end = savedStart, savedEnd
			} else {
				add(name, match[4], kind, typ, -1)
			}
		}
		if i < len(document.Lines) {
			original := document.Lines[i]
			if imp := importFrom.FindStringSubmatch(original); imp != nil {
				header := strings.TrimSpace(strings.SplitN(text, "from", 2)[0])
				header = strings.TrimSpace(strings.TrimPrefix(header, "import"))
				header = strings.Trim(header, "{} ")
				for _, binding := range strings.Split(header, ",") {
					fields := strings.Fields(strings.Trim(binding, "{} "))
					if len(fields) == 0 {
						continue
					}
					name, alias := fields[0], fields[0]
					if len(fields) >= 3 && fields[len(fields)-2] == "as" {
						alias = fields[len(fields)-1]
					}
					if name == "*" {
						name = ""
					}
					a.Imports = append(a.Imports, CodeImport{Alias: alias, Name: name, Path: imp[1]})
				}
			}
			if imp := namespaceImport.FindStringSubmatch(original); imp != nil {
				parts := strings.Split(imp[1], ".")
				a.Imports = append(a.Imports, CodeImport{Alias: parts[len(parts)-1], Path: strings.ReplaceAll(imp[1], ".", "/")})
			}
		}
	}
}

func pythonScopeEnd(lines []string, start int) int {
	indent := len(lines[start]) - len(strings.TrimLeft(lines[start], " \t"))
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		next := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
		if next <= indent {
			return i
		}
	}
	return len(lines) + 1
}
