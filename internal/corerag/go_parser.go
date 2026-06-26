package corerag

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// GoParser extracts Go symbols and call edges using the standard library
// go/parser (no cgo). Type information is not resolved across packages, so
// call edges are static-approximate (callee names rather than resolved
// symbols); this matches the documented limitation for all graph-based code
// retrieval (CORE doc §7.4). Call targets are resolved to known symbol IDs by
// the graph builder, which matches by name within the repo.
type GoParser struct{}

// Name returns "go".
func (GoParser) Name() string { return "go" }

// Extract parses src and returns symbols (functions, methods, types) plus
// call/import edges.
func (GoParser) Extract(_ context.Context, path, src string) ([]Symbol, []Edge, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse go file %s: %w", path, err)
	}

	var symbols []Symbol
	var edges []Edge

	// Import edges: file -> imported package (synthetic targets resolved later).
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p == "" {
			continue
		}
		edges = append(edges, Edge{
			From:   pathID(path),
			To:     importID(p),
			Kind:   EdgeImport,
			Weight: 1.0,
		})
	}

	// Declarations: functions, methods, type declarations.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sy := goFuncSymbol(fset, path, d)
			if sy.ID != "" {
				if d.Body != nil {
					bs := fset.Position(d.Body.Pos()).Offset
					be := fset.Position(d.Body.End()).Offset
					if bs >= 0 && be <= len(src) && be > bs {
						sy.Body = src[bs:be]
					}
				}
				symbols = append(symbols, sy)
				edges = append(edges, collectCallEdges(path, sy, d.Body)...)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				sy := goTypeSymbol(fset, path, ts, d.Doc)
				if sy.ID != "" {
					symbols = append(symbols, sy)
				}
			}
		}
	}

	return symbols, edges, nil
}

// goFuncSymbol builds a Symbol for a function/method declaration.
func goFuncSymbol(fset *token.FileSet, path string, d *ast.FuncDecl) Symbol {
	name := d.Name.Name
	kind := "function"
	recv := ""
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = "method"
		recv = typeString(d.Recv.List[0].Type)
		name = recv + "." + name
	}
	sig := formatGoSig(d)
	start, end := fset.Position(d.Pos()).Offset, fset.Position(d.End()).Offset
	return Symbol{
		ID:        symbolID(path, name),
		Path:      path,
		Name:      name,
		Kind:      kind,
		Signature: sig,
		Doc:       docText(d.Doc),
		ByteStart: start,
		ByteEnd:   end,
	}
}

// goTypeSymbol builds a Symbol for a type declaration (struct/interface/etc).
func goTypeSymbol(fset *token.FileSet, path string, ts *ast.TypeSpec, doc *ast.CommentGroup) Symbol {
	start, end := fset.Position(ts.Pos()).Offset, fset.Position(ts.End()).Offset
	kindDetail := "type"
	switch ts.Type.(type) {
	case *ast.StructType:
		kindDetail = "struct"
	case *ast.InterfaceType:
		kindDetail = "interface"
	}
	return Symbol{
		ID:        symbolID(path, ts.Name.Name),
		Path:      path,
		Name:      ts.Name.Name,
		Kind:      "type",
		Signature: "type " + ts.Name.Name + " " + kindDetail,
		Doc:       docText(doc),
		ByteStart: start,
		ByteEnd:   end,
	}
}

// collectCallEdges walks a function body and emits call edges from the
// enclosing symbol to each callee (by name).
func collectCallEdges(path string, sy Symbol, body *ast.BlockStmt) []Edge {
	var edges []Edge
	if body == nil {
		return edges
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := calleeName(call.Fun)
		if callee == "" {
			return true
		}
		edges = append(edges, Edge{
			From:   sy.ID,
			To:     callTargetID(callee),
			Kind:   EdgeCall,
			Weight: 1.0,
		})
		return true
	})
	return edges
}

// calleeName extracts a best-effort callee identifier from a call expression's
// Fun operand (identifiers, selectors); dynamic calls are ignored.
func calleeName(fun ast.Expr) string {
	switch e := fun.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// formatGoSig renders "func name(params) results" without the body.
func formatGoSig(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(typeString(d.Recv.List[0].Type))
		b.WriteString(") ")
	}
	b.WriteString(d.Name.Name)
	b.WriteString(paramsString(d.Type.Params))
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		b.WriteString(" ")
		b.WriteString(paramsString(d.Type.Results))
	}
	return b.String()
}

// paramsString renders a field list as "(a int, b string)".
func paramsString(fl *ast.FieldList) string {
	if fl == nil {
		return "()"
	}
	var parts []string
	for _, f := range fl.List {
		typ := typeString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typ)
			continue
		}
		for _, n := range f.Names {
			parts = append(parts, n.Name+" "+typ)
		}
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// typeString renders an AST type expression to a readable string.
func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.InterfaceType:
		return "interface{...}"
	case *ast.StructType:
		return "struct{...}"
	case *ast.FuncType:
		return "func" + paramsString(t.Params)
	}
	return "any"
}

// docText joins a comment group into a single string.
func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var lines []string
	for _, c := range cg.List {
		lines = append(lines, strings.TrimSpace(strings.TrimPrefix(c.Text, "//")))
	}
	return strings.Join(lines, "\n")
}

// --- ID helpers -------------------------------------------------------------

func pathID(path string) string     { return "file:" + path }
func symbolID(path, name string) string { return "sym:" + path + "#" + name }
func importID(pkg string) string    { return "imp:" + pkg }
func callTargetID(callee string) string { return "call:" + callee }
