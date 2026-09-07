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
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// SkipDir reports whether a directory should be skipped when walking a workload
// or an extension catalog. These hold dependencies or repository metadata rather
// than first-party source.
func SkipDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules":
		return true
	default:
		return false
	}
}

// File is one parsed Go source file and the path it was read from.
type File struct {
	Path   string
	Syntax *ast.File
}

// ParseDir parses every non-test Go file under root using syntax alone. It is
// the fallback for workloads whose packages cannot be type-checked, and the
// source of the initial file list before go/packages is consulted.
func ParseDir(fset *token.FileSet, root string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if SkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		syntax, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		files = append(files, File{Path: path, Syntax: syntax})
		return nil
	})
	return files, err
}

// DirectImportPaths returns the sorted, deduplicated import paths named by the
// files themselves. Only direct imports count: a module the workload does not
// name cannot contribute conditions.
func DirectImportPaths(files []File) []string {
	seen := make(map[string]bool)
	for _, file := range files {
		for _, spec := range file.Syntax.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err == nil {
				seen[path] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	slices.Sort(result)
	return result
}

// NodeError prefixes err with the source position of node so a diagnostic
// points at the declaration that caused it.
func NodeError(fset *token.FileSet, node ast.Node, err error) error {
	return fmt.Errorf("%s: %w", fset.Position(node.Pos()), err)
}
