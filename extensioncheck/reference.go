package extensioncheck

import (
	"cmp"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

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
	return r.ProfileString(), nil
}

// ProfileString is the reference as a profile writes it: <short-url>:<version>.
// The short URL is the extension id without its trailing
// /<version>/runtimeconditions.extension.yaml. An id that is not an https URL
// ending that way is kept whole, and a reference without a version is the bare id.
func (r ExtensionReference) ProfileString() string {
	if r.Version == "" {
		return r.ID
	}
	if short, ok := r.shortID(); ok {
		return short + ":" + r.Version
	}
	return r.ID + ":" + r.Version
}

func (r ExtensionReference) shortID() (string, bool) {
	tail := "/" + r.Version + "/" + DefaultExtensionFile
	if r.Version != "" && strings.HasPrefix(r.ID, "https://") && strings.HasSuffix(r.ID, tail) {
		return strings.TrimSuffix(r.ID, tail), true
	}
	return "", false
}
