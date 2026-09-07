package gosource

import (
	"fmt"
	"go/token"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Load type-checks the workload rooted at dir and returns its files together
// with the resolved type information.
//
// This is what lifts extraction above pattern matching: it is the only way to
// tell a call to an SDK function from a call to a local function of the same
// name, or to follow a constant to its value across packages. It fails if any
// package reports an error, because partial type information yields a silently
// narrowed profile rather than an honest one.
func Load(dir string) (*token.FileSet, []File, *Semantic, error) {
	fset := token.NewFileSet()
	config := &packages.Config{
		Dir:   dir,
		Fset:  fset,
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Tests: false,
	}
	loaded, err := packages.Load(config, "./...")
	if err != nil {
		return nil, nil, nil, err
	}
	var failures []string
	for _, pkg := range loaded {
		for _, pkgErr := range pkg.Errors {
			failures = append(failures, pkgErr.Error())
		}
	}
	if len(failures) > 0 {
		return nil, nil, nil, fmt.Errorf("go/packages load failed: %s", strings.Join(failures, "; "))
	}

	semantic := newSemantic()
	var files []File
	for _, pkg := range loaded {
		if pkg.TypesInfo == nil {
			continue
		}
		semantic.absorb(pkg.TypesInfo)
		for _, syntax := range pkg.Syntax {
			position := fset.Position(syntax.Package)
			files = append(files, File{Path: position.Filename, Syntax: syntax})
		}
	}
	slices.SortFunc(files, func(left File, right File) int {
		return strings.Compare(left.Path, right.Path)
	})
	return fset, files, semantic, nil
}
