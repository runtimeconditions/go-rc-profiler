package check

import (
	"path/filepath"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
)

// scope is the kind and interface type a binding option applies to. An option
// declared on a declaration inherits that declaration's scope; one declared at
// the top level carries its own.
type scope struct {
	kind          string
	interfaceType string
}

// validateBinding checks a binding manifest's metadata and then its language
// half against the resolved vocabulary.
func (c *Checker) validateBinding(node *catalog.Node, resolved catalog.Vocabulary) {
	if node.Binding == nil {
		if node.BindingDir != "" || c.opts.RequireLanguagePackage {
			c.collector.Addf(node.Dir, "missing %s binding manifest", languageDisplayName(c.opts.Language))
		}
		return
	}
	binding := node.Binding
	if binding.APIVersion != "runtimeconditions.io/v1alpha1" {
		c.collector.Addf(node.BindingPath, "apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if binding.Kind != "RuntimeConditionsBinding" && binding.Kind != "RuntimeConditionsPackage" {
		c.collector.Addf(node.BindingPath, "kind must be RuntimeConditionsBinding or RuntimeConditionsPackage")
	}
	if binding.Kind == "RuntimeConditionsPackage" && filepath.Base(node.BindingPath) == catalog.GoBindingsManifest {
		c.collector.Addf(node.BindingPath, "kind RuntimeConditionsBinding is required for %s", catalog.GoBindingsManifest)
	}
	if binding.Kind == "RuntimeConditionsBinding" && filepath.Base(node.BindingPath) == catalog.GoPackageBindingManifest {
		c.collector.Addf(node.BindingPath, "kind RuntimeConditionsPackage is required for %s", catalog.GoPackageBindingManifest)
	}
	if binding.Metadata.Language == "" {
		c.collector.Addf(node.BindingPath, "metadata.language is required")
	} else if binding.Metadata.Language != c.opts.Language {
		c.collector.Addf(node.BindingPath, "metadata.language must be %s", c.opts.Language)
	}
	bindingID := binding.ExtensionID()
	if bindingID == "" {
		c.collector.Addf(node.BindingPath, "extension id is required")
	} else if bindingID != node.ID {
		c.collector.Addf(node.BindingPath, "binding extension id %s does not match extension definition %s", bindingID, node.ID)
	}
	c.validateBindingDefinitionPath(node)

	switch c.opts.Language {
	case "go":
		c.validateGoBinding(node, resolved)
	default:
		c.collector.Addf(node.BindingPath, "unsupported binding language %s", c.opts.Language)
	}
}

// validateBindingDefinitionPath checks that the definition a manifest points at
// is the definition it was discovered alongside.
func (c *Checker) validateBindingDefinitionPath(node *catalog.Node) {
	definition := node.Binding.ExtensionDefinitionPath()
	if definition == "" {
		return
	}
	resolvedPath := definition
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(node.BindingPath), resolvedPath)
	}
	absDefinition, err := filepath.Abs(resolvedPath)
	if err != nil {
		c.collector.Addf(node.BindingPath, "%v", err)
		return
	}
	absNodeDefinition, err := filepath.Abs(node.DefinitionPath)
	if err != nil {
		c.collector.Addf(node.DefinitionPath, "%v", err)
		return
	}
	if filepath.Clean(absDefinition) != filepath.Clean(absNodeDefinition) {
		c.collector.Addf(node.BindingPath, "binding extension definition %s does not match %s", absDefinition, absNodeDefinition)
	}
}

func (c *Checker) validateGoBinding(node *catalog.Node, resolved catalog.Vocabulary) {
	binding := node.Binding
	if binding.Go.ImportPath == "" {
		c.collector.Addf(node.BindingPath, "go.importPath is required")
	}
	if binding.Go.Package == "" {
		c.collector.Addf(node.BindingPath, "go.package is required")
	}
	if len(binding.Go.Declarations) == 0 && len(binding.Go.Options) == 0 {
		c.collector.Addf(node.BindingPath, "go.declarations or go.options must not be empty")
	}
	for name, value := range binding.Go.Constants {
		if resolved.FieldValueValueCount(value) == 0 {
			c.collector.Addf(node.BindingPath, "constant %s value %q is not defined by resolved field values", name, value)
		}
	}
	for _, declaration := range binding.Go.Declarations {
		c.validateBindingDeclaration(node.BindingPath, resolved, declaration)
	}
	for _, option := range binding.Go.Options {
		c.validateBindingOption(node.BindingPath, resolved, option, scopesFromOption(option, resolved))
	}
}

func (c *Checker) validateBindingDeclaration(path string, resolved catalog.Vocabulary, declaration catalog.Declaration) {
	if declaration.Function == "" && declaration.Method == "" {
		c.collector.Addf(path, "declaration must specify function or method")
	}
	c.collector.ExpectExactlyOne(path, resolved.KindCount(declaration.Kind), "declaration kind %s", declaration.Kind)
	if declaration.InterfaceType != "" {
		c.collector.ExpectExactlyOne(path, resolved.InterfaceTypeCount(declaration.Kind, declaration.InterfaceType), "declaration interfaceType %s/%s", declaration.Kind, declaration.InterfaceType)
	}
	for _, value := range declaration.Values {
		c.validateBindingValue(path, resolved, declaration.Kind, declaration.InterfaceType, value)
	}
	for _, option := range declaration.Options {
		c.validateBindingOption(path, resolved, option, []scope{{kind: declaration.Kind, interfaceType: declaration.InterfaceType}})
	}
}

func (c *Checker) validateBindingValue(path string, resolved catalog.Vocabulary, kind string, interfaceType string, value catalog.Value) {
	switch value.Target {
	case "interface.bucketClass":
		c.collector.ExpectExactlyOne(path, resolved.InterfaceFieldCount(kind, interfaceType, "bucketClass"), "binding value target %s for %s/%s", value.Target, kind, interfaceType)
		c.collector.ExpectExactlyOne(path, resolved.FieldValueCount("interface.bucketClass", kind, interfaceType, value.Value), "binding value %s=%s for %s/%s", value.Target, value.Value, kind, interfaceType)
	default:
		c.collector.Addf(path, "unsupported binding value target %s", value.Target)
	}
}

// validateBindingOption checks that an option's target exists in the resolved
// vocabulary for every scope it applies to. An interface.type option narrows the
// scopes in place, so options nested beneath it are checked against the type it
// selected.
func (c *Checker) validateBindingOption(path string, resolved catalog.Vocabulary, option catalog.Option, optionScopes []scope) {
	switch option.Target {
	case "interface.spec":
		for _, scope := range optionScopes {
			c.collector.ExpectExactlyOne(path, resolved.InterfaceFieldCount(scope.kind, scope.interfaceType, "spec"), "binding option %s for %s/%s", option.Target, scope.kind, scope.interfaceType)
		}
	case "interface.operations[]":
		for _, scope := range optionScopes {
			c.collector.ExpectExactlyOne(path, resolved.InterfaceFieldCount(scope.kind, scope.interfaceType, "operations"), "binding option %s for %s/%s", option.Target, scope.kind, scope.interfaceType)
			c.collector.ExpectExactlyOne(path, resolved.FieldValueCount("interface.operations[].method", scope.kind, scope.interfaceType, option.Method), "binding option method %s for %s/%s", option.Method, scope.kind, scope.interfaceType)
		}
	case "interface.type":
		for i, scope := range optionScopes {
			c.collector.ExpectExactlyOne(path, resolved.InterfaceTypeCount(scope.kind, option.Value), "binding option interface.type %s/%s", scope.kind, option.Value)
			optionScopes[i].interfaceType = option.Value
			if option.EngineArg != nil {
				c.collector.ExpectExactlyOne(path, resolved.InterfaceFieldCount(scope.kind, option.Value, "engine"), "binding option engine for %s/%s", scope.kind, option.Value)
			}
		}
	case "configuration.env[]":
		c.validateConfigurationBindingOption(path, resolved, optionScopes, "configuration.env[].property")
	case "configuration.alternatives[]":
		c.validateConfigurationBindingOption(path, resolved, optionScopes, "configuration.alternatives[].env[].property")
	case "requestBodySchema", "responseSchema", "env.sensitive", "env.required":
	case "":
		c.collector.Addf(path, "binding option %s is missing target", option.Function)
	default:
		c.collector.Addf(path, "unsupported binding option target %s", option.Target)
	}
	for _, nested := range option.Options {
		c.validateBindingOption(path, resolved, nested, optionScopes)
	}
}

func (c *Checker) validateConfigurationBindingOption(path string, resolved catalog.Vocabulary, optionScopes []scope, propertyField string) {
	if len(optionScopes) == 0 {
		c.collector.Addf(path, "configuration binding option requires appliesToKinds/appliesToInterfaceTypes or a declaration scope")
		return
	}
	for _, scope := range optionScopes {
		c.collector.ExpectExactlyOne(path, resolved.ConditionFieldCount(scope.kind, scope.interfaceType, "configuration"), "binding option configuration for %s/%s", scope.kind, scope.interfaceType)
		c.collector.ExpectExactlyOne(path, resolved.FieldValueDefinitionCount(propertyField, scope.kind, scope.interfaceType), "binding option property field %s for %s/%s", propertyField, scope.kind, scope.interfaceType)
	}
}

// scopesFromOption expands a top-level option's declared applicability into the
// scopes it must be valid for. Interface types that the vocabulary does not
// define for a kind are dropped, leaving the pairing to be reported elsewhere.
func scopesFromOption(option catalog.Option, resolved catalog.Vocabulary) []scope {
	if len(option.AppliesToKinds) == 0 {
		return nil
	}
	var scopes []scope
	if len(option.AppliesToInterfaceTypes) == 0 {
		for _, kind := range option.AppliesToKinds {
			scopes = append(scopes, scope{kind: kind})
		}
		return scopes
	}
	for _, kind := range option.AppliesToKinds {
		for _, interfaceType := range option.AppliesToInterfaceTypes {
			if resolved.InterfaceTypeCount(kind, interfaceType) == 1 {
				scopes = append(scopes, scope{kind: kind, interfaceType: interfaceType})
			}
		}
	}
	return scopes
}

func languageDisplayName(language string) string {
	if language == "go" {
		return "Go"
	}
	return language
}
