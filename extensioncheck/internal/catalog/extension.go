package catalog

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultExtensionFile is the conventional file name for an extension definition.
const DefaultExtensionFile = "runtimeconditions.extension.yaml"

// ExtensionReference identifies an extension release. Manifests and dependencies
// use an id/version object; profiles also accept the scalar reference form.
type ExtensionReference struct {
	ID      string `yaml:"id" json:"id"`
	Version string `yaml:"version" json:"version"`
}

func (r ExtensionReference) Valid() bool    { return r.ID != "" }
func (r ExtensionReference) String() string { return r.ID + "@" + r.Version }
func (r ExtensionReference) Compare(other ExtensionReference) int {
	if order := cmp.Compare(r.ID, other.ID); order != 0 {
		return order
	}
	return cmp.Compare(r.Version, other.Version)
}

func (r *ExtensionReference) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		var fields map[string]any
		if err := node.Decode(&fields); err != nil {
			return err
		}
		id, idOK := fields["id"].(string)
		version, versionOK := fields["version"].(string)
		if len(fields) != 2 || !idOK || id == "" || !versionOK || version == "" {
			return fmt.Errorf("extension reference must contain non-empty string id and version")
		}
		r.ID, r.Version = id, version
		return nil
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || node.Value == "" {
		return fmt.Errorf("extension reference must be an id/version object or a non-empty string")
	}
	value := node.Value
	separator := strings.LastIndex(value, ":")
	schemeEnd := strings.Index(value, "://")
	if separator > 0 && separator > schemeEnd+2 && separator > strings.LastIndex(value, "/") {
		if separator == len(value)-1 {
			return fmt.Errorf("extension reference version suffix must be non-empty")
		}
		r.ID, r.Version = value[:separator], value[separator+1:]
	} else {
		r.ID, r.Version = value, ""
	}
	return nil
}

func (r ExtensionReference) MarshalYAML() (any, error) {
	if r.Version == "" {
		return r.ID, nil
	}
	return r.ID + ":" + r.Version, nil
}

// ExtensionDefinition is a RuntimeConditionsExtensionDefinition document. It
// declares the vocabulary an extension contributes and the extensions it builds on.
type ExtensionDefinition struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		ID      string `yaml:"id"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	Spec ExtensionSpec `yaml:"spec"`
}

// ExtensionSpec is the vocabulary an extension definition contributes.
type ExtensionSpec struct {
	Dependencies    []ExtensionReference `yaml:"dependencies"`
	Kinds           []Kind               `yaml:"kinds"`
	InterfaceTypes  []InterfaceType      `yaml:"interfaceTypes"`
	ConditionFields []ConditionField     `yaml:"conditionFields"`
	InterfaceFields []InterfaceField     `yaml:"interfaceFields"`
	FieldValues     []FieldValue         `yaml:"fieldValues"`
	Schemas         []ValidationSchema   `yaml:"schemas"`
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
		Kind     string         `yaml:"kind"`
		Metadata map[string]any `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return ExtensionDefinition{}, false, err
	}
	if probe.Kind != "RuntimeConditionsExtensionDefinition" {
		return ExtensionDefinition{}, false, nil
	}
	identifier, idOK := probe.Metadata["id"].(string)
	version, versionOK := probe.Metadata["version"].(string)
	_, hasVersion := probe.Metadata["version"]
	_, hasURI := probe.Metadata["uri"]
	if !idOK || identifier == "" || (hasVersion && (!versionOK || version == "")) || hasURI {
		return ExtensionDefinition{}, false, fmt.Errorf("metadata.id is required and metadata.version, when present, must be a non-empty string")
	}
	var definition ExtensionDefinition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return ExtensionDefinition{}, false, err
	}
	return definition, true, nil
}

// DefinitionID returns the identifier def declares.
func DefinitionID(def ExtensionDefinition) ExtensionReference {
	if def.Metadata.ID == "" {
		return ExtensionReference{}
	}
	return ExtensionReference{ID: def.Metadata.ID, Version: def.Metadata.Version}
}

// IsYAML reports whether path names a YAML document.
func IsYAML(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}
