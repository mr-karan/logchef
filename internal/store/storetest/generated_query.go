package storetest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// GeneratedQuery reads a private SQL string constant without changing generated code.
func GeneratedQuery(t *testing.T, path, name string) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		constants, ok := declaration.(*ast.GenDecl)
		if !ok || constants.Tok != token.CONST {
			continue
		}
		for _, spec := range constants.Specs {
			value := spec.(*ast.ValueSpec)
			for i, identifier := range value.Names {
				if identifier.Name != name {
					continue
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s is not a string literal", name)
				}
				query, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				return query
			}
		}
	}
	t.Fatalf("query constant %s not found in %s", name, path)
	return ""
}
