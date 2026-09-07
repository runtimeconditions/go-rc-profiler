package gosource

import (
	"go/ast"
	"go/constant"
	"go/types"
)

// Semantic is the type information for a whole workload, flattened across its
// packages so callers can ask about any expression without first locating the
// package that owns it.
//
// A nil *Semantic is valid and answers "unknown" to everything, which is how
// the syntax-only extraction path degrades.
type Semantic struct {
	types      map[ast.Expr]types.TypeAndValue
	uses       map[*ast.Ident]types.Object
	defs       map[*ast.Ident]types.Object
	selections map[*ast.SelectorExpr]*types.Selection
}

func newSemantic() *Semantic {
	return &Semantic{
		types:      make(map[ast.Expr]types.TypeAndValue),
		uses:       make(map[*ast.Ident]types.Object),
		defs:       make(map[*ast.Ident]types.Object),
		selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
}

func (s *Semantic) absorb(info *types.Info) {
	for expr, value := range info.Types {
		s.types[expr] = value
	}
	for ident, object := range info.Uses {
		s.uses[ident] = object
	}
	for ident, object := range info.Defs {
		if object != nil {
			s.defs[ident] = object
		}
	}
	for selector, selection := range info.Selections {
		s.selections[selector] = selection
	}
}

// StringValue reports the compile-time string an expression resolves to,
// following named constants across package boundaries.
func (s *Semantic) StringValue(expr ast.Expr) (string, bool) {
	if s == nil {
		return "", false
	}
	object, ok := s.ObjectForExpr(expr).(*types.Const)
	if !ok || object.Val().Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(object.Val()), true
}

// TypeOf reports the resolved type of an expression, falling back to the type
// of the object it names.
func (s *Semantic) TypeOf(expr ast.Expr) (types.Type, bool) {
	if s == nil {
		return nil, false
	}
	if value, ok := s.types[expr]; ok && value.Type != nil {
		return value.Type, true
	}
	if object := s.ObjectForExpr(expr); object != nil && object.Type() != nil {
		return object.Type(), true
	}
	return nil, false
}

// ObjectForExpr resolves the declared object an identifier or selector names.
func (s *Semantic) ObjectForExpr(expr ast.Expr) types.Object {
	if s == nil {
		return nil
	}
	switch typed := Unparen(expr).(type) {
	case *ast.Ident:
		if object := s.uses[typed]; object != nil {
			return object
		}
		return s.defs[typed]
	case *ast.SelectorExpr:
		return s.uses[typed.Sel]
	default:
		return nil
	}
}

// IsDefinition reports whether an identifier declares a new object rather than
// referring to an existing one. Reassignment to an existing variable invalidates
// any value previously tracked for it.
func (s *Semantic) IsDefinition(ident *ast.Ident) bool {
	if s == nil {
		return false
	}
	return s.defs[ident] != nil
}

// ReceiverTypeForSelector reports the package path and type name of the value a
// method is being called on, which is how a method call is attributed to the
// module that declares its receiver.
func (s *Semantic) ReceiverTypeForSelector(selector *ast.SelectorExpr) (string, string, bool) {
	if s == nil {
		return "", "", false
	}
	if selection := s.selections[selector]; selection != nil {
		if pkgPath, typeName, ok := NamedTypeIdentity(selection.Recv()); ok {
			return pkgPath, typeName, true
		}
	}
	if typ, ok := s.TypeOf(selector.X); ok {
		return NamedTypeIdentity(typ)
	}
	return "", "", false
}

// NamedTypeIdentity reduces a type to the package path and name of the named
// type underneath it, seeing through aliases and pointer indirection.
func NamedTypeIdentity(typ types.Type) (string, string, bool) {
	typ = types.Unalias(typ)
	for {
		pointer, ok := typ.(*types.Pointer)
		if !ok {
			break
		}
		typ = types.Unalias(pointer.Elem())
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return "", "", false
	}
	return named.Obj().Pkg().Path(), named.Obj().Name(), true
}
