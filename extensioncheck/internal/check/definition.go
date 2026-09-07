package check

import (
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
)

// validateDefinition checks a definition against itself: required metadata,
// resolvable dependencies, and no name declared twice.
func (c *Checker) validateDefinition(node *catalog.Node) {
	def := node.Definition
	if def.APIVersion != "runtimeconditions.io/v1alpha1" {
		c.collector.Addf(node.DefinitionPath, "apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if def.Kind != "RuntimeConditionsExtensionDefinition" {
		c.collector.Addf(node.DefinitionPath, "kind must be RuntimeConditionsExtensionDefinition")
	}
	if def.Metadata.ID == "" {
		c.collector.Addf(node.DefinitionPath, "metadata.id is required")
	} else if !catalog.ValidExtensionID(def.Metadata.ID) {
		c.collector.Addf(node.DefinitionPath, "metadata.id must be an absolute HTTP or HTTPS URI")
	}
	if node.ID != catalog.DefinitionID(def) {
		c.collector.Addf(node.DefinitionPath, "extension id %s does not match metadata", node.ID)
	}
	if len(def.Spec.Dependencies) == 0 &&
		len(def.Spec.Kinds) == 0 &&
		len(def.Spec.InterfaceTypes) == 0 &&
		len(def.Spec.ConditionFields) == 0 &&
		len(def.Spec.InterfaceFields) == 0 &&
		len(def.Spec.FieldValues) == 0 &&
		len(def.Spec.Schemas) == 0 {
		c.collector.Addf(node.DefinitionPath, "spec must define at least one vocabulary item or schema")
	}
	for _, dependency := range def.Spec.Dependencies {
		if !catalog.ValidExtensionID(dependency) {
			c.collector.Addf(node.DefinitionPath, "invalid dependency extension id %q", dependency)
		}
		if c.catalog.Nodes[dependency] == nil {
			c.collector.Addf(node.DefinitionPath, "dependency %s cannot be resolved", dependency)
		}
	}
	c.checkSelfDuplicates(node)
}

// checkSelfDuplicates reports vocabulary a single definition declares twice.
func (c *Checker) checkSelfDuplicates(node *catalog.Node) {
	seen := make(map[string]bool)
	check := func(key string) {
		if seen[key] {
			c.collector.Addf(node.DefinitionPath, "duplicate vocabulary definition %s", key)
		}
		seen[key] = true
	}
	for _, kind := range node.Definition.Spec.Kinds {
		if kind.Name == "" {
			c.collector.Addf(node.DefinitionPath, "kind name is required")
			continue
		}
		check("kind:" + kind.Name)
	}
	for _, item := range node.Definition.Spec.InterfaceTypes {
		if item.Name == "" || item.TargetKind == "" {
			c.collector.Addf(node.DefinitionPath, "interfaceTypes entries require name and targetKind")
			continue
		}
		check("interfaceType:" + item.TargetKind + ":" + item.Name)
	}
	for _, item := range node.Definition.Spec.ConditionFields {
		if item.Name == "" {
			c.collector.Addf(node.DefinitionPath, "conditionFields entries require name")
			continue
		}
		if len(item.AppliesToKinds) == 0 {
			c.collector.Addf(node.DefinitionPath, "condition field %s must declare appliesToKinds", item.Name)
		}
		check("conditionField:" + item.Name + ":" + strings.Join(item.AppliesToKinds, ",") + ":" + strings.Join(item.AppliesToInterfaceTypes, ","))
	}
	for _, item := range node.Definition.Spec.InterfaceFields {
		if item.Name == "" || item.TargetKind == "" || item.TargetType == "" {
			c.collector.Addf(node.DefinitionPath, "interfaceFields entries require name, targetKind, and targetType")
			continue
		}
		check("interfaceField:" + item.TargetKind + ":" + item.TargetType + ":" + item.Name)
	}
	for _, item := range node.Definition.Spec.FieldValues {
		if item.Field == "" || item.TargetKind == "" || len(item.Values) == 0 {
			c.collector.Addf(node.DefinitionPath, "fieldValues entries require field, targetKind, and values")
			continue
		}
		check("fieldValue:" + item.TargetKind + ":" + item.TargetType + ":" + item.Field)
		valueSeen := make(map[string]bool)
		for _, value := range item.Values {
			if valueSeen[value] {
				c.collector.Addf(node.DefinitionPath, "duplicate field value %q for %s", value, item.Field)
			}
			valueSeen[value] = true
		}
	}
	for _, schema := range node.Definition.Spec.Schemas {
		if schema.ID == "" {
			c.collector.Addf(node.DefinitionPath, "schema id is required")
		}
		if schema.Description == "" {
			c.collector.Addf(node.DefinitionPath, "schema %s description is required", schema.ID)
		}
		if schema.Schema == nil {
			c.collector.Addf(node.DefinitionPath, "schema %s schema object is required", schema.ID)
		} else if _, err := compileConditionSchema(schema.Schema); err != nil {
			c.collector.Addf(node.DefinitionPath, "schema %s is invalid: %v", schema.ID, err)
		}
		check("schema:" + schema.ID)
	}
}

// validateVocabulary checks that everything a definition references resolves to
// exactly one declaration across its dependency closure.
func (c *Checker) validateVocabulary(node *catalog.Node, resolved catalog.Vocabulary) {
	path := node.DefinitionPath
	for _, item := range node.Definition.Spec.InterfaceTypes {
		c.collector.ExpectExactlyOne(path, resolved.KindCount(item.TargetKind), "interface type %s targetKind %s", item.Name, item.TargetKind)
	}
	for _, item := range node.Definition.Spec.InterfaceFields {
		c.collector.ExpectExactlyOne(path, resolved.KindCount(item.TargetKind), "interface field %s targetKind %s", item.Name, item.TargetKind)
		c.collector.ExpectExactlyOne(path, resolved.InterfaceTypeCount(item.TargetKind, item.TargetType), "interface field %s targetType %s/%s", item.Name, item.TargetKind, item.TargetType)
	}
	for _, item := range node.Definition.Spec.ConditionFields {
		for _, kind := range item.AppliesToKinds {
			c.collector.ExpectExactlyOne(path, resolved.KindCount(kind), "condition field %s appliesToKind %s", item.Name, kind)
			for _, targetType := range item.AppliesToInterfaceTypes {
				c.collector.ExpectExactlyOne(path, resolved.InterfaceTypeCount(kind, targetType), "condition field %s appliesToInterfaceType %s/%s", item.Name, kind, targetType)
			}
		}
	}
	for _, item := range node.Definition.Spec.FieldValues {
		c.collector.ExpectExactlyOne(path, resolved.KindCount(item.TargetKind), "field values %s targetKind %s", item.Field, item.TargetKind)
		if item.TargetType != "" {
			c.collector.ExpectExactlyOne(path, resolved.InterfaceTypeCount(item.TargetKind, item.TargetType), "field values %s targetType %s/%s", item.Field, item.TargetKind, item.TargetType)
		}
		c.validateFieldValuePath(path, resolved, item)
	}
	for _, schema := range node.Definition.Spec.Schemas {
		if schema.AppliesToKind != "" {
			c.collector.ExpectExactlyOne(path, resolved.KindCount(schema.AppliesToKind), "schema %s appliesToKind %s", schema.ID, schema.AppliesToKind)
		}
		if schema.AppliesToKind != "" && schema.AppliesToInterfaceType != "" {
			c.collector.ExpectExactlyOne(path, resolved.InterfaceTypeCount(schema.AppliesToKind, schema.AppliesToInterfaceType), "schema %s appliesToInterfaceType %s/%s", schema.ID, schema.AppliesToKind, schema.AppliesToInterfaceType)
		}
	}
	c.checkResolvedConflicts(path, resolved)
}

// validateFieldValuePath resolves the field a fieldValues entry enumerates. The
// leading segment says where to look: interface fields, the configuration
// condition field, or a condition field named directly.
func (c *Checker) validateFieldValuePath(path string, resolved catalog.Vocabulary, item catalog.FieldValue) {
	switch {
	case strings.HasPrefix(item.Field, "interface."):
		field := firstPathSegment(strings.TrimPrefix(item.Field, "interface."))
		if field == "type" {
			return
		}
		c.collector.ExpectExactlyOne(path, resolved.InterfaceFieldCount(item.TargetKind, item.TargetType, field), "fieldValues field %s interface field %s/%s/%s", item.Field, item.TargetKind, item.TargetType, field)
	case strings.HasPrefix(item.Field, "configuration."):
		c.collector.ExpectExactlyOne(path, resolved.ConditionFieldCount(item.TargetKind, item.TargetType, "configuration"), "fieldValues field %s condition field configuration for %s/%s", item.Field, item.TargetKind, item.TargetType)
	default:
		field := firstPathSegment(item.Field)
		c.collector.ExpectExactlyOne(path, resolved.ConditionFieldCount(item.TargetKind, item.TargetType, field), "fieldValues field %s condition field %s for %s/%s", item.Field, field, item.TargetKind, item.TargetType)
	}
}

// checkResolvedConflicts reports vocabulary that two extensions in the resolved
// set both define.
func (c *Checker) checkResolvedConflicts(path string, resolved catalog.Vocabulary) {
	for key, count := range resolved.Counts() {
		if count > 1 {
			c.collector.Addf(path, "resolved extension set contains vocabulary conflict for %s", key)
		}
	}
	for _, conflict := range resolved.ConditionFieldConflicts() {
		c.collector.Addf(path, "resolved extension set contains vocabulary conflict for %s", conflict)
	}
}

// firstPathSegment returns the leading segment of a dotted field path, dropping
// any list suffix.
func firstPathSegment(path string) string {
	path = strings.TrimSuffix(path, "[]")
	if index := strings.Index(path, "."); index >= 0 {
		return strings.TrimSuffix(path[:index], "[]")
	}
	return strings.TrimSuffix(path, "[]")
}
