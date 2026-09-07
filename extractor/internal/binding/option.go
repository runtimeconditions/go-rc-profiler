package binding

import (
	"fmt"
	"go/ast"
	"strconv"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
)

// applyOption writes one option call onto the condition it refines. The
// manifest's target names the profile field, so the set of cases here is the
// full vocabulary an option may reach.
func (c *compiler) applyOption(condition *profile.Condition, option Option, call *ast.CallExpr, imports *fileImports, binding *Binding) error {
	switch option.Target {
	case "interface.spec":
		spec, err := c.apiSpec(option, call)
		if err != nil {
			return err
		}
		condition.Interface.Spec = &spec
		return nil

	case "interface.operations[]":
		operation, err := c.operation(option, call, imports, binding)
		if err != nil {
			return err
		}
		condition.Interface.Operations = append(condition.Interface.Operations, operation)
		return nil

	case "interface.type":
		condition.Interface.Type = option.Value
		if option.EngineArg == nil {
			return nil
		}
		if *option.EngineArg >= len(call.Args) {
			return fmt.Errorf("%s requires an engine argument", option.Function)
		}
		engine, ok := c.constantValue(call.Args[*option.EngineArg], imports, binding)
		if !ok {
			return fmt.Errorf("%s engine must be a string literal, string const, or binding constant", option.Function)
		}
		condition.Interface.Engine = engine
		return nil

	case "configuration.env[]":
		env, err := c.envInput(option, call, imports, binding)
		if err != nil {
			return err
		}
		if condition.Configuration != nil && len(condition.Configuration.Alternatives) > 0 {
			return fmt.Errorf("%s cannot be combined with configuration alternatives", option.Function)
		}
		if condition.Configuration == nil {
			condition.Configuration = &profile.Configuration{}
		}
		condition.Configuration.Env = append(condition.Configuration.Env, env)
		return nil

	case "configuration.alternatives[]":
		alternative, err := c.envAlternative(option, call, imports, binding)
		if err != nil {
			return err
		}
		if condition.Configuration != nil && len(condition.Configuration.Env) > 0 {
			return fmt.Errorf("%s cannot be combined with configuration env", option.Function)
		}
		if condition.Configuration == nil {
			condition.Configuration = &profile.Configuration{}
		}
		condition.Configuration.Alternatives = append(condition.Configuration.Alternatives, alternative)
		return nil

	case "requestBodySchema", "responseSchema":
		return fmt.Errorf("%s is only valid as an operation option", option.Function)

	default:
		value, err := c.optionValue(option, call, imports, binding)
		if err != nil {
			return err
		}
		return applyValue(condition, option.Target, value)
	}
}

// apiSpec reads the contract document an API interface points at.
func (c *compiler) apiSpec(option Option, call *ast.CallExpr) (profile.APISpec, error) {
	format, err := c.stringArg(call, option, "format", true)
	if err != nil {
		return profile.APISpec{}, err
	}
	uri, err := c.stringArg(call, option, "uri", true)
	if err != nil {
		return profile.APISpec{}, err
	}
	version, err := c.stringArg(call, option, "version", false)
	if err != nil {
		return profile.APISpec{}, err
	}
	return profile.APISpec{Format: format, URI: uri, Version: version}, nil
}

// operation reads one API operation and the request or response schemas nested
// inside it. Arguments that are not recognized nested options are ignored, so an
// operation can carry unrelated values.
func (c *compiler) operation(option Option, call *ast.CallExpr, imports *fileImports, binding *Binding) (profile.Operation, error) {
	path, err := c.stringArg(call, option, "path", true)
	if err != nil {
		return profile.Operation{}, err
	}
	operation := profile.Operation{Method: option.Method, Path: path}
	for _, arg := range call.Args[1:] {
		subcall, ok := gosource.Unparen(arg).(*ast.CallExpr)
		if !ok {
			continue
		}
		match, ok := imports.nestedOption(subcall, binding, option.Options)
		if !ok {
			continue
		}
		if err := c.applyOperationOption(&operation, match.option, match.typeArgs); err != nil {
			return profile.Operation{}, err
		}
	}
	return operation, nil
}

// applyOperationOption attaches a schema derived from the option's type
// argument. These options are generic, so the payload type arrives as a type
// argument rather than a value.
func (c *compiler) applyOperationOption(operation *profile.Operation, option Option, typeArgs []ast.Expr) error {
	if option.TypeArg == nil {
		return fmt.Errorf("%s requires typeArg in binding manifest", option.Function)
	}
	if *option.TypeArg >= len(typeArgs) {
		return fmt.Errorf("%s requires a type argument", option.Function)
	}
	derived, err := c.schema.ForExpr(typeArgs[*option.TypeArg])
	if err != nil {
		return err
	}
	switch option.Target {
	case "requestBodySchema":
		operation.RequestBodySchema = derived
	case "responseSchema":
		operation.ResponseSchema = derived
	default:
		return fmt.Errorf("unsupported operation option target %q", option.Target)
	}
	return nil
}

// envAlternative reads one acceptable set of environment inputs. Unlike
// elsewhere, every argument must be a recognized nested option: a silently
// dropped input would change which configurations the profile claims to accept.
func (c *compiler) envAlternative(option Option, call *ast.CallExpr, imports *fileImports, binding *Binding) (profile.ConfigurationAlternative, error) {
	if len(call.Args) == 0 {
		return profile.ConfigurationAlternative{}, fmt.Errorf("%s requires at least one env input", option.Function)
	}
	alternative := profile.ConfigurationAlternative{}
	for _, arg := range call.Args {
		subcall, ok := gosource.Unparen(arg).(*ast.CallExpr)
		if !ok {
			return profile.ConfigurationAlternative{}, fmt.Errorf("%s arguments must match nested option calls declared by the package manifest", option.Function)
		}
		match, ok := imports.nestedOption(subcall, binding, option.Options)
		if !ok {
			return profile.ConfigurationAlternative{}, fmt.Errorf("%s arguments must match nested option calls declared by the package manifest", option.Function)
		}
		env, err := c.envInput(match.option, subcall, imports, match.binding)
		if err != nil {
			return profile.ConfigurationAlternative{}, err
		}
		alternative.Env = append(alternative.Env, env)
	}
	return alternative, nil
}

// envInput reads a property-to-variable binding and the flags refining it.
func (c *compiler) envInput(option Option, call *ast.CallExpr, imports *fileImports, binding *Binding) (profile.EnvInput, error) {
	property, err := c.stringArg(call, option, "property", true)
	if err != nil {
		return profile.EnvInput{}, err
	}
	name, err := c.stringArg(call, option, "name", true)
	if err != nil {
		return profile.EnvInput{}, err
	}
	env := profile.EnvInput{Property: property, Name: name}
	for _, arg := range call.Args[2:] {
		subcall, ok := gosource.Unparen(arg).(*ast.CallExpr)
		if !ok {
			continue
		}
		match, ok := imports.nestedOption(subcall, binding, option.Options)
		if !ok {
			continue
		}
		if err := applyEnvInputOption(&env, match.option); err != nil {
			return profile.EnvInput{}, err
		}
	}
	return env, nil
}

// applyEnvInputOption sets a boolean flag on an env input. Sensitivity and
// requiredness are fixed by the manifest, never read from the call.
func applyEnvInputOption(env *profile.EnvInput, option Option) error {
	switch option.Target {
	case "env.sensitive":
		value, err := strconv.ParseBool(option.Value)
		if err != nil {
			return fmt.Errorf("%s has invalid boolean value %q", option.Function, option.Value)
		}
		env.Sensitive = value
	case "env.required":
		value, err := strconv.ParseBool(option.Value)
		if err != nil {
			return fmt.Errorf("%s has invalid boolean value %q", option.Function, option.Value)
		}
		env.Required = &value
	default:
		return fmt.Errorf("unsupported env input option target %q", option.Target)
	}
	return nil
}

// stringArg reads a named argument whose position the manifest declares. An
// optional argument that is absent yields the empty string; a required one that
// is absent or not a compile-time string is an error.
func (c *compiler) stringArg(call *ast.CallExpr, option Option, name string, required bool) (string, error) {
	index, ok := option.StringArgs[name]
	if !ok {
		if required {
			return "", fmt.Errorf("%s binding is missing stringArgs.%s", option.Function, name)
		}
		return "", nil
	}
	if index >= len(call.Args) {
		if required {
			return "", fmt.Errorf("%s requires %s argument", option.Function, name)
		}
		return "", nil
	}
	value, ok := c.scope.StringValue(call.Args[index])
	if !ok {
		return "", fmt.Errorf("%s %s must be a string literal or string const", option.Function, name)
	}
	return value, nil
}

// optionValue resolves the value an option writes, either fixed by the manifest
// or read from a declared argument position.
func (c *compiler) optionValue(option Option, call *ast.CallExpr, imports *fileImports, binding *Binding) (string, error) {
	if option.Value != "" {
		return option.Value, nil
	}
	if option.ValueArg == nil {
		return "", fmt.Errorf("%s binding must declare value or valueArg", option.Function)
	}
	if *option.ValueArg >= len(call.Args) {
		return "", fmt.Errorf("%s requires a value argument", option.Function)
	}
	value, ok := c.constantValue(call.Args[*option.ValueArg], imports, binding)
	if !ok {
		return "", fmt.Errorf("%s value must be a string literal, string const, or binding constant", option.Function)
	}
	return value, nil
}

// constantValue resolves an argument to a compile-time string, additionally
// accepting a constant published by the expected binding. That is what lets a
// manifest expose named values, such as an engine, as exported identifiers.
func (c *compiler) constantValue(expr ast.Expr, imports *fileImports, expected *Binding) (string, bool) {
	if value, ok := c.scope.StringValue(expr); ok {
		return value, true
	}
	switch typed := gosource.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		ident, ok := gosource.Unparen(typed.X).(*ast.Ident)
		if !ok {
			return "", false
		}
		binding := imports.aliases[ident.Name]
		if binding != expected {
			return "", false
		}
		value, ok := binding.Constants[typed.Sel.Name]
		return value, ok
	case *ast.Ident:
		for _, binding := range imports.dot {
			if binding != expected {
				continue
			}
			if value, ok := binding.Constants[typed.Name]; ok {
				return value, true
			}
		}
	}
	return "", false
}
