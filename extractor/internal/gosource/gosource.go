// Package gosource reads the Go source of a target workload without executing
// it.
//
// It covers the four things every extraction path needs: the parsed files, the
// type information the Go toolchain can supply for them, the module graph that
// resolves their imports to directories on disk, and the small AST helpers that
// turn expressions into compile-time constants. Nothing here knows what a
// Runtime Condition is.
package gosource

import (
	"fmt"
	"go/ast"
	"go/token"
)

// File is one parsed Go source file and the path it was read from.
type File struct {
	Path   string
	Syntax *ast.File
}

// NodeError prefixes err with the source position of node so a diagnostic
// points at the declaration that caused it.
func NodeError(fset *token.FileSet, node ast.Node, err error) error {
	return fmt.Errorf("%s: %w", fset.Position(node.Pos()), err)
}
