package gosource

import (
	"go/ast"
	"go/token"
)

// PackageScope is the workload's own vocabulary: the struct types it declares
// and the string constants it defines, plus whatever type information was
// available. Declarations reference these by name, so extraction has to be able
// to resolve them without leaving the workload.
type PackageScope struct {
	Semantic *Semantic

	structs      map[string]*ast.StructType
	stringConsts map[string]string
}

// NewPackageScope returns an empty scope. Semantic may be nil, in which case
// resolution falls back to syntax alone.
func NewPackageScope(semantic *Semantic) *PackageScope {
	return &PackageScope{
		Semantic:     semantic,
		structs:      make(map[string]*ast.StructType),
		stringConsts: make(map[string]string),
	}
}

// Collect records the struct types and string constants declared at the top
// level of a file. Call it for every file before resolving anything.
func (s *PackageScope) Collect(file *ast.File) {
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch general.Tok {
		case token.TYPE:
			s.collectTypes(general)
		case token.CONST:
			s.collectStringConsts(general)
		}
	}
}

func (s *PackageScope) collectTypes(general *ast.GenDecl) {
	for _, spec := range general.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if structType, ok := typeSpec.Type.(*ast.StructType); ok {
			s.structs[typeSpec.Name.Name] = structType
		}
	}
}

func (s *PackageScope) collectStringConsts(general *ast.GenDecl) {
	for _, spec := range general.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range valueSpec.Names {
			if i >= len(valueSpec.Values) {
				continue
			}
			if value, ok := StringLiteral(valueSpec.Values[i]); ok {
				s.stringConsts[name.Name] = value
			}
		}
	}
}

// Struct returns a struct type declared by the workload.
func (s *PackageScope) Struct(name string) (*ast.StructType, bool) {
	structType, ok := s.structs[name]
	return structType, ok
}

// StringValue resolves an expression to a compile-time string, trying a literal
// first, then type information, then the workload's own string constants.
func (s *PackageScope) StringValue(expr ast.Expr) (string, bool) {
	if value, ok := StringLiteral(expr); ok {
		return value, true
	}
	if value, ok := s.Semantic.StringValue(expr); ok {
		return value, true
	}
	ident, ok := Unparen(expr).(*ast.Ident)
	if !ok {
		return "", false
	}
	value, ok := s.stringConsts[ident.Name]
	return value, ok
}
