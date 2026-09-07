package binding

import (
	"fmt"
	"go/ast"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/schema"
)

// compiler turns a matched declaration call into a condition. It holds the
// workload scope needed to resolve constants and the deriver needed to turn type
// arguments into schemas.
type compiler struct {
	scope  *gosource.PackageScope
	schema *schema.Deriver
}

func newCompiler(scope *gosource.PackageScope) *compiler {
	return &compiler{scope: scope, schema: schema.NewDeriver(scope)}
}

// conditionFor builds the condition a declaration call produces.
//
// It also reports any other bindings whose standalone options were applied,
// since using an option pulls that option's extension into the profile.
func (c *compiler) conditionFor(call *ast.CallExpr, binding *Binding, declaration Declaration, imports *fileImports) (profile.Condition, []*Binding, error) {
	name, err := c.conditionName(call, declaration)
	if err != nil {
		return profile.Condition{}, nil, err
	}

	condition := profile.Condition{
		Name:          name,
		Kind:          declaration.Kind,
		Interface:     profile.Interface{Type: declaration.InterfaceType},
		Configuration: declaration.Configuration.Clone(),
	}
	for _, value := range declaration.Values {
		if err := applyValue(&condition, value.Target, value.Value); err != nil {
			return profile.Condition{}, nil, err
		}
	}

	borrowed := make(map[*Binding]bool)
	for index, arg := range call.Args {
		if declaration.NameArg != nil && index == *declaration.NameArg {
			continue
		}
		subcall, ok := gosource.Unparen(arg).(*ast.CallExpr)
		if !ok {
			continue
		}
		match, ok := imports.conditionOption(subcall, binding, declaration, condition)
		if !ok {
			continue
		}
		if err := c.applyOption(&condition, match.option, subcall, imports, match.binding); err != nil {
			return profile.Condition{}, nil, err
		}
		if match.binding != binding {
			borrowed[match.binding] = true
		}
	}

	optionBindings := make([]*Binding, 0, len(borrowed))
	for optionBinding := range borrowed {
		optionBindings = append(optionBindings, optionBinding)
	}
	return condition, optionBindings, nil
}

// conditionName resolves the condition's name, which is either fixed by the
// manifest or read from an argument that must be a compile-time string.
func (c *compiler) conditionName(call *ast.CallExpr, declaration Declaration) (string, error) {
	if declaration.NameArg == nil {
		return declaration.Name, nil
	}
	if *declaration.NameArg >= len(call.Args) {
		return "", fmt.Errorf("%s requires a name argument", declaration.displayName())
	}
	name, ok := c.scope.StringValue(call.Args[*declaration.NameArg])
	if !ok {
		return "", fmt.Errorf("%s name must be a string literal or string const", declaration.displayName())
	}
	return name, nil
}

// applyValue writes a fixed manifest value onto a condition.
func applyValue(condition *profile.Condition, target string, value string) error {
	switch target {
	case "interface.bucketClass":
		condition.Interface.BucketClass = value
	default:
		return fmt.Errorf("unsupported binding target %q", target)
	}
	return nil
}
