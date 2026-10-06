package extractor

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

// GeneratedOptions contains only profile identity supplied by the caller. The
// generated-binding path has no syntax-only, validation, catalog, or SDK
// mapping bypasses.
type GeneratedOptions struct {
	Name            string
	WorkloadURI     string
	WorkloadVersion string
}

// ExtractGeneratedDir reconstructs generated declarations from Go type
// information and validates the complete profile before returning it.
func ExtractGeneratedDir(dir string, opts GeneratedOptions) (map[string]any, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	fset, files, semantic, err := gosource.Load(absDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 || semantic == nil {
		return nil, fmt.Errorf("no type-checked Go source found in %s", absDir)
	}
	packages, err := extensioncheck.ResolveImportedGoPackages(context.Background(), absDir, nil)
	if err != nil {
		return nil, err
	}
	reader := newGeneratedReader(fset, semantic, packages)
	var conditions []any
	var sources [][]string
	for _, file := range files {
		if err := reader.rejectDeclarationAliases(file.Syntax); err != nil {
			return nil, err
		}
		var readErr error
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			if readErr != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function, ok := semantic.ObjectForExpr(call.Fun).(*types.Func)
			if !ok || function.Pkg() == nil {
				return true
			}
			declaration := reader.declarations[function.Pkg().Path()+"\x00"+function.Name()]
			if declaration == nil {
				return true
			}
			condition, used, err := reader.readDeclaration(call, function, declaration)
			if err != nil {
				readErr = gosource.NodeError(fset, call, fmt.Errorf("%s (%s): %w", function.Name(), manifestString(manifestObject(declaration, "modelRef"), "coordinate"), err))
				return false
			}
			conditions = append(conditions, condition)
			sources = append(sources, used)
			return true
		})
		if readErr != nil {
			return nil, readErr
		}
	}
	if len(conditions) == 0 {
		return nil, fmt.Errorf("no generated binding declarations found in %s", absDir)
	}
	profile := map[string]any{
		"apiVersion": "runtimeconditions.io/v1alpha1",
		"kind":       "RuntimeConditionsProfile",
		"metadata":   map[string]any{"name": opts.Name},
		"workload":   map[string]any{"uri": opts.WorkloadURI, "version": opts.WorkloadVersion},
		"conditions": conditions,
	}
	return extensioncheck.FinalizeGeneratedProfile(context.Background(), absDir, profile, sources)
}
