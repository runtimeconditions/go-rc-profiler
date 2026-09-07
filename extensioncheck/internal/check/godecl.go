package check

import (
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/gosrc"
)

// validateGoDeclarations checks a binding manifest against the Go package it
// claims to describe: that the named functions, methods, and constants exist,
// and that every argument index is in range and of the right type.
func (c *Checker) validateGoDeclarations(node *catalog.Node) {
	if c.opts.Language != "go" || node.Binding == nil {
		return
	}
	pkg, err := gosrc.ReadPackage(node.GoDir)
	if err != nil {
		c.collector.Addf(node.GoDir, "%v", err)
		return
	}
	if node.Binding.Go.Package != "" && pkg.Name != node.Binding.Go.Package {
		c.collector.Addf(node.GoDir, "go package name %s does not match binding package %s", pkg.Name, node.Binding.Go.Package)
	}
	for name, value := range node.Binding.Go.Constants {
		actual, ok := pkg.Constants[name]
		if !ok {
			c.collector.Addf(node.GoDir, "binding constant %s is not declared in Go package", name)
			continue
		}
		if actual != value {
			c.collector.Addf(node.GoDir, "binding constant %s value %q does not match Go value %q", name, value, actual)
		}
	}
	for _, declaration := range node.Binding.Go.Declarations {
		c.validateGoDeclaration(node.GoDir, pkg, declaration)
	}
	for _, option := range node.Binding.Go.Options {
		c.validateGoOption(node.GoDir, pkg, option)
	}
	for _, constructor := range node.Binding.Go.Constructors {
		if constructor.Function == "" || constructor.Receiver == "" {
			c.collector.Addf(node.GoDir, "constructors require function and receiver")
			continue
		}
		if _, ok := pkg.Funcs[constructor.Function]; !ok {
			c.collector.Addf(node.GoDir, "binding constructor function %s is not declared in Go package", constructor.Function)
		}
	}
}

func (c *Checker) validateGoDeclaration(path string, pkg gosrc.Package, declaration catalog.Declaration) {
	if declaration.Function != "" {
		fn, ok := pkg.Funcs[declaration.Function]
		if !ok {
			c.collector.Addf(path, "binding declaration function %s is not declared in Go package", declaration.Function)
		} else {
			c.validateFunctionIndexes(path, fn, declaration.NameArg, nil, nil, nil)
		}
	}
	if declaration.Method != "" {
		key := declaration.Receiver + "." + declaration.Method
		if _, ok := pkg.Methods[key]; !ok {
			c.collector.Addf(path, "binding declaration method %s is not declared in Go package", key)
		}
	}
	for _, option := range declaration.Options {
		c.validateGoOption(path, pkg, option)
	}
}

func (c *Checker) validateGoOption(path string, pkg gosrc.Package, option catalog.Option) {
	fn, ok := pkg.Funcs[option.Function]
	if !ok {
		c.collector.Addf(path, "binding option function %s is not declared in Go package", option.Function)
		return
	}
	c.validateFunctionIndexes(path, fn, nil, option.StringArgs, option.ValueArg, option.EngineArg)
	if option.TypeArg != nil && *option.TypeArg >= fn.TypeParamCount {
		c.collector.Addf(path, "binding option %s typeArg %d is out of range", option.Function, *option.TypeArg)
	}
	for _, nested := range option.Options {
		c.validateGoOption(path, pkg, nested)
	}
}

// validateFunctionIndexes checks each argument index a manifest declares. Indexes
// that carry a name or a string key must point at a string parameter; those that
// carry a value or an engine may point at any type.
func (c *Checker) validateFunctionIndexes(path string, fn gosrc.Func, nameArg *int, stringArgs map[string]int, valueArg *int, engineArg *int) {
	if nameArg != nil {
		c.validateParamIndex(path, fn, *nameArg, "nameArg", true)
	}
	for name, index := range stringArgs {
		c.validateParamIndex(path, fn, index, "stringArgs."+name, true)
	}
	if valueArg != nil {
		c.validateParamIndex(path, fn, *valueArg, "valueArg", false)
	}
	if engineArg != nil {
		c.validateParamIndex(path, fn, *engineArg, "engineArg", false)
	}
}

func (c *Checker) validateParamIndex(path string, fn gosrc.Func, index int, field string, requireString bool) {
	if index < 0 || index >= len(fn.Params) {
		c.collector.Addf(path, "binding %s for function %s index %d is out of range", field, fn.Name, index)
		return
	}
	param := fn.Params[index]
	if requireString && param.Type != "string" {
		c.collector.Addf(path, "binding %s for function %s points at non-string parameter %s %s", field, fn.Name, param.Name, param.Type)
	}
}
