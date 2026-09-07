package gosource

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
)

// Unparen strips redundant parentheses so callers can type-switch on the
// expression that actually matters.
func Unparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

// StringLiteral reports the value of a quoted string literal. It resolves
// nothing: an identifier, even one bound to a constant, is not a literal.
func StringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := Unparen(expr).(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

// ExprString renders a type expression for diagnostics. It handles the forms a
// schema type argument can take and falls back to the node type for anything
// else, since the message exists to tell an author what was rejected.
func ExprString(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return ExprString(typed.X) + "." + typed.Sel.Name
	case *ast.StarExpr:
		return "*" + ExprString(typed.X)
	case *ast.ArrayType:
		return "[]" + ExprString(typed.Elt)
	default:
		return fmt.Sprintf("%T", expr)
	}
}
