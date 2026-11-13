package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

func analyzeGo(a *CodeAnalysis, text, filename string) {
	fs := token.NewFileSet()
	file, _ := parser.ParseFile(fs, filename, text, parser.AllErrors)
	if file == nil {
		return
	}
	a.Package = file.Name.Name
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		alias := path.Base(p)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		a.Imports = append(a.Imports, CodeImport{Alias: alias, Path: p})
	}
	positions := map[token.Pos]int{}
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		start, end, container := 0, len(a.Code)+1, ""
		for _, parent := range stack {
			switch n := parent.(type) {
			case *ast.BlockStmt:
				start = fs.Position(n.Pos()).Line
				end = fs.Position(n.End()).Line
			case *ast.FuncDecl:
				if n.Recv != nil && len(n.Recv.List) > 0 {
					container = goType(n.Recv.List[0].Type)
				}
			case *ast.TypeSpec:
				container = n.Name.Name
			}
		}
		add := func(id *ast.Ident, kind, typ string, arity int, scopeStart, scopeEnd int) {
			if id == nil || id.Name == "_" {
				return
			}
			pos := fs.Position(id.Pos())
			positions[id.Pos()] = len(a.Definitions)
			a.Definitions = append(a.Definitions, CodeDefinition{Name: id.Name, Kind: kind, Container: container, Type: typ, Line: pos.Line, Column: pos.Column, ScopeStart: scopeStart, ScopeEnd: scopeEnd, Arity: arity})
		}
		switch n := node.(type) {
		case *ast.FuncDecl:
			kind := "function"
			if n.Recv != nil && len(n.Recv.List) > 0 {
				kind = "method"
				container = goType(n.Recv.List[0].Type)
			}
			arity := 0
			for _, field := range n.Type.Params.List {
				arity += max(1, len(field.Names))
				if _, ok := field.Type.(*ast.Ellipsis); ok {
					arity = -1
					break
				}
			}
			add(n.Name, kind, "", arity, 0, len(a.Code)+1)
		case *ast.TypeSpec:
			add(n.Name, "type", n.Name.Name, -1, start, end)
		case *ast.ValueSpec:
			typ := goType(n.Type)
			for i, id := range n.Names {
				if typ == "" && i < len(n.Values) {
					typ = goExpressionType(n.Values[i])
				}
				add(id, "variable", typ, -1, start, end)
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, expr := range []ast.Expr{n.Key, n.Value} {
					if id, ok := expr.(*ast.Ident); ok {
						add(id, "variable", "", -1, fs.Position(n.Pos()).Line, fs.Position(n.End()).Line)
					}
				}
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for i, expr := range n.Lhs {
					if id, ok := expr.(*ast.Ident); ok {
						typ := ""
						if i < len(n.Rhs) {
							typ = goExpressionType(n.Rhs[i])
						}
						add(id, "variable", typ, -1, start, end)
					}
				}
			}
		case *ast.Field:
			kind := "field"
			arity := -1
			if fn, ok := n.Type.(*ast.FuncType); ok {
				kind = "method"
				arity = 0
				for _, field := range fn.Params.List {
					arity += max(1, len(field.Names))
				}
			}
			fieldStart, fieldEnd := start, end
			for _, parent := range stack {
				if fn, ok := parent.(*ast.FuncDecl); ok {
					kind = "parameter"
					fieldStart = fs.Position(fn.Pos()).Line
					fieldEnd = fs.Position(fn.End()).Line
				}
			}
			for _, id := range n.Names {
				add(id, kind, goType(n.Type), arity, fieldStart, fieldEnd)
			}
		}
		stack = append(stack, node)
		return true
	})
	ast.Inspect(file, func(node ast.Node) bool {
		id, ok := node.(*ast.Ident)
		if ok && id.Obj != nil {
			if index, found := positions[id.Obj.Pos()]; found {
				pos := fs.Position(id.Pos())
				a.Bindings[bindingKey(pos.Line, pos.Column)] = index
			}
		}
		return true
	})
}
func goType(expr ast.Expr) string {
	switch n := expr.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.StarExpr:
		return goType(n.X)
	case *ast.SelectorExpr:
		return n.Sel.Name
	case *ast.IndexExpr:
		return goType(n.X)
	}
	return ""
}
func goExpressionType(expr ast.Expr) string {
	switch n := expr.(type) {
	case *ast.CompositeLit:
		return goType(n.Type)
	case *ast.UnaryExpr:
		return goExpressionType(n.X)
	case *ast.CallExpr:
		if id, ok := n.Fun.(*ast.Ident); ok {
			if id.Name == "new" && len(n.Args) > 0 {
				return goType(n.Args[0])
			}
			return strings.TrimPrefix(id.Name, "New")
		}
	}
	return ""
}
