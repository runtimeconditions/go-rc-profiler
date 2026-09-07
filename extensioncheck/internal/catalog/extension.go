package catalog

import (
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultExtensionFile is the conventional file name for an extension definition.
const DefaultExtensionFile = "runtimeconditions.extension.yaml"

// ExtensionDefinition is a RuntimeConditionsExtensionDefinition document. It
// declares the vocabulary an extension contributes and the extensions it builds on.
type ExtensionDefinition struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		ID string `yaml:"id"`
	} `yaml:"metadata"`
	Spec ExtensionSpec `yaml:"spec"`
}

// ExtensionSpec is the vocabulary an extension definition contributes.
type ExtensionSpec struct {
	Dependencies    []string           `yaml:"dependencies"`
	Kinds           []Kind             `yaml:"kinds"`
	InterfaceTypes  []InterfaceType    `yaml:"interfaceTypes"`
	ConditionFields []ConditionField   `yaml:"conditionFields"`
	InterfaceFields []InterfaceField   `yaml:"interfaceFields"`
	FieldValues     []FieldValue       `yaml:"fieldValues"`
	Schemas         []ValidationSchema `yaml:"schemas"`
}

// Kind declares a condition kind.
type Kind struct {
	Name string `yaml:"name"`
}

// InterfaceType declares an interface type available to a kind.
type InterfaceType struct {
	Name       string `yaml:"name"`
	TargetKind string `yaml:"targetKind"`
}

// ConditionField declares a top-level condition field and the kinds it applies to.
type ConditionField struct {
	Name                    string   `yaml:"name"`
	AppliesToKinds          []string `yaml:"appliesToKinds"`
	AppliesToInterfaceTypes []string `yaml:"appliesToInterfaceTypes"`
}

// InterfaceField declares a field available on one kind and interface type pair.
type InterfaceField struct {
	Name       string `yaml:"name"`
	TargetKind string `yaml:"targetKind"`
	TargetType string `yaml:"targetType"`
}

// FieldValue enumerates the values a field accepts.
type FieldValue struct {
	Field      string   `yaml:"field"`
	TargetKind string   `yaml:"targetKind"`
	TargetType string   `yaml:"targetType"`
	Values     []string `yaml:"values"`
}

// ValidationSchema is a JSON Schema applied to matching conditions.
type ValidationSchema struct {
	ID                     string `yaml:"id"`
	Description            string `yaml:"description"`
	AppliesToKind          string `yaml:"appliesToKind"`
	AppliesToInterfaceType string `yaml:"appliesToInterfaceType"`
	Schema                 any    `yaml:"schema"`
}

// ReadExtensionDefinition reads path and reports whether it holds an extension
// definition. Documents of any other kind are skipped rather than rejected, since
// a catalog root holds many kinds of YAML.
func ReadExtensionDefinition(path string) (ExtensionDefinition, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ExtensionDefinition{}, false, err
	}
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return ExtensionDefinition{}, false, err
	}
	if probe.Kind != "RuntimeConditionsExtensionDefinition" {
		return ExtensionDefinition{}, false, nil
	}
	var definition ExtensionDefinition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return ExtensionDefinition{}, false, err
	}
	return definition, true, nil
}

// DefinitionID returns the identifier def declares.
func DefinitionID(def ExtensionDefinition) string {
	return def.Metadata.ID
}

// ValidExtensionID reports whether id is an absolute HTTP or HTTPS URI.
func ValidExtensionID(id string) bool {
	parsed, err := url.Parse(id)
	if err != nil {
		return false
	}
	return parsed.IsAbs() && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

// IsYAML reports whether path names a YAML document.
func IsYAML(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}
