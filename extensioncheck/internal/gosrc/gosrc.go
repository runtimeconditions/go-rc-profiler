// Package gosrc reads the declarations a Go package exposes, so that binding
// manifests can be checked against the API they claim to describe.
package gosrc

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// Package is what a Go package offers a binding manifest: its name, the
// functions and methods it declares, and its string constants.
type Package struct {
	Name      string
	Funcs     map[string]Func
	Methods   map[string]Func
	Constants map[string]string
}

// Func is a declared function or method.
type Func struct {
	Name           string
	Params         []Param
	TypeParamCount int
}

// Param is one parameter of a declared function.
type Param struct {
	Name     string
	Type     string
	Variadic bool
}

// ReadPackage reads every non-test Go file beneath dir. Methods are keyed by
// receiver type and name, as "Receiver.Method".
func ReadPackage(dir string) (Package, error) {
	fset := token.NewFileSet()
	pkg := Package{
		Funcs:     make(map[string]Func),
		Methods:   make(map[string]Func),
		Constants: make(map[string]string),
	}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if pkg.Name == "" {
			pkg.Name = file.Name.Name
		} else if pkg.Name != file.Name.Name {
			return fmt.Errorf("mixed package names %s and %s", pkg.Name, file.Name.Name)
		}
		pkg.collect(file)
		return nil
	})
	return pkg, err
}

func (p *Package) collect(file *ast.File) {
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			fn := Func{
				Name:           typed.Name.Name,
				Params:         functionParams(typed.Type),
				TypeParamCount: typeParamCount(typed.Type),
			}
			if typed.Recv == nil || len(typed.Recv.List) == 0 {
				p.Funcs[typed.Name.Name] = fn
			} else {
				receiver := receiverTypeName(typed.Recv.List[0].Type)
				p.Methods[receiver+"."+typed.Name.Name] = fn
			}
		case *ast.GenDecl:
			if typed.Tok != token.CONST {
				continue
			}
			p.collectConstants(typed)
		}
	}
}

func (p *Package) collectConstants(decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range valueSpec.Names {
			if i >= len(valueSpec.Values) {
				continue
			}
			if value, ok := stringLiteral(valueSpec.Values[i]); ok {
				p.Constants[name.Name] = value
			}
		}
	}
}

func functionParams(fn *ast.FuncType) []Param {
	if fn.Params == nil {
		return nil
	}
	var params []Param
	for _, field := range fn.Params.List {
		typ := fieldTypeName(field.Type)
		variadic := false
		if ellipsis, ok := field.Type.(*ast.Ellipsis); ok {
			variadic = true
			typ = fieldTypeName(ellipsis.Elt)
		}
		if len(field.Names) == 0 {
			params = append(params, Param{Type: typ, Variadic: variadic})
			continue
		}
		for _, name := range field.Names {
			params = append(params, Param{Name: name.Name, Type: typ, Variadic: variadic})
		}
	}
	return params
}

func typeParamCount(fn *ast.FuncType) int {
	if fn.TypeParams == nil {
		return 0
	}
	count := 0
	for _, field := range fn.TypeParams.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func receiverTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	default:
		return fieldTypeName(expr)
	}
}

func fieldTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return fieldTypeName(typed.X) + "." + typed.Sel.Name
	case *ast.StarExpr:
		return "*" + fieldTypeName(typed.X)
	case *ast.ArrayType:
		return "[]" + fieldTypeName(typed.Elt)
	case *ast.Ellipsis:
		return fieldTypeName(typed.Elt)
	case *ast.InterfaceType:
		return "interface"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}
