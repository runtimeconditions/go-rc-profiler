package binding

import (
	"go/ast"
	"go/token"
	"slices"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
)

// ExtractConditions walks the workload for calls that declare conditions and
// returns them together with the sorted extension IDs they draw on.
//
// A declaration call is not descended into. Its arguments are options belonging
// to the condition being built, not declarations in their own right.
func ExtractConditions(
	fset *token.FileSet,
	scope *gosource.PackageScope,
	files []gosource.File,
	bindings []*Binding,
) ([]profile.Condition, []string, error) {
	compiler := newCompiler(scope)
	extensions := make(map[string]bool)
	var conditions []profile.Condition

	for _, file := range files {
		imports := importsForFile(file.Syntax, bindings, scope.Semantic)
		if !imports.declaresAnything() {
			continue
		}

		var walkErr error
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			if walkErr != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			binding, declaration, ok := imports.declarationForCall(call)
			if !ok {
				return true
			}
			condition, optionBindings, err := compiler.conditionFor(call, binding, declaration, imports)
			if err != nil {
				walkErr = gosource.NodeError(fset, call, err)
				return false
			}
			extensions[binding.ExtensionID] = true
			for _, optionBinding := range optionBindings {
				extensions[optionBinding.ExtensionID] = true
			}
			conditions = append(conditions, condition)
			return false
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}

	ids := make([]string, 0, len(extensions))
	for id := range extensions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return conditions, ids, nil
}
