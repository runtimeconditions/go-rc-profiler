package catalog

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// GoBindingsManifest names the manifest an extension publishes for its own
	// Go bindings.
	GoBindingsManifest = "runtimeconditions.bindings.yaml"
	// GoPackageBindingManifest names the manifest a Go package publishes to
	// declare the conditions its API produces.
	GoPackageBindingManifest = "runtimeconditions.package.yaml"
)

// BindingDocument is a RuntimeConditionsBinding or RuntimeConditionsPackage
// manifest, which maps a Go API onto the vocabulary of an extension.
type BindingDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Extension           string `yaml:"extension"`
		ExtensionDefinition string `yaml:"extensionDefinition"`
		Package             string `yaml:"package"`
		Language            string `yaml:"language"`
	} `yaml:"metadata"`
	Extension struct {
		ID         string `yaml:"id"`
		Definition string `yaml:"definition"`
	} `yaml:"extension"`
	Go BindingGo `yaml:"go"`
}

// BindingGo is the Go half of a binding manifest.
type BindingGo struct {
	ImportPath   string            `yaml:"importPath"`
	Package      string            `yaml:"package"`
	Constants    map[string]string `yaml:"constants"`
	Constructors []Constructor     `yaml:"constructors"`
	Declarations []Declaration     `yaml:"declarations"`
	Options      []Option          `yaml:"options"`
}

// Constructor names a function whose result carries declaration methods.
type Constructor struct {
	Function string `yaml:"function"`
	Receiver string `yaml:"receiver"`
}

// Declaration maps a Go function or method onto a condition.
type Declaration struct {
	Function      string  `yaml:"function"`
	Receiver      string  `yaml:"receiver"`
	Method        string  `yaml:"method"`
	Name          string  `yaml:"name"`
	Kind          string  `yaml:"kind"`
	InterfaceType string  `yaml:"interfaceType"`
	NameArg       *int    `yaml:"nameArg"`
	Values        []Value `yaml:"values"`
	Options       []Option
}

// Value fixes a condition field to a constant.
type Value struct {
	Target string `yaml:"target"`
	Value  string `yaml:"value"`
}

// Option maps a Go function onto a field of the condition it refines.
type Option struct {
	Function                string            `yaml:"function"`
	Target                  string            `yaml:"target"`
	Value                   string            `yaml:"value"`
	ValueArg                *int              `yaml:"valueArg"`
	TypeArg                 *int              `yaml:"typeArg"`
	EngineArg               *int              `yaml:"engineArg"`
	Method                  string            `yaml:"method"`
	AppliesToKinds          []string          `yaml:"appliesToKinds"`
	AppliesToInterfaceTypes []string          `yaml:"appliesToInterfaceTypes"`
	StringArgs              map[string]int    `yaml:"stringArgs"`
	Options                 []Option          `yaml:"options"`
	Configuration           any               `yaml:"configuration"`
	Values                  []Value           `yaml:"values"`
	Declarations            []BindingDocument `yaml:"declarations"`
}

// ExtensionID returns the extension the manifest binds to.
func (b *BindingDocument) ExtensionID() string {
	if b.Kind == "RuntimeConditionsPackage" {
		return b.Extension.ID
	}
	return b.Metadata.Extension
}

// ExtensionDefinitionPath returns the definition path the manifest declares,
// which may be relative to the manifest itself.
func (b *BindingDocument) ExtensionDefinitionPath() string {
	if b.Kind == "RuntimeConditionsPackage" {
		return b.Extension.Definition
	}
	return b.Metadata.ExtensionDefinition
}

// ReadBindingDocument reads a binding or package manifest from path.
func ReadBindingDocument(path string) (*BindingDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document BindingDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

// FindBindingManifest returns the binding manifest held in dir, if there is one.
func FindBindingManifest(dir string) (string, bool, error) {
	for _, name := range []string{GoBindingsManifest, GoPackageBindingManifest} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, err
		}
		return path, true, nil
	}
	return "", false, nil
}
