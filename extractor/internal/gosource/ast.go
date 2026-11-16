package gosource

import (
	"go/ast"
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
