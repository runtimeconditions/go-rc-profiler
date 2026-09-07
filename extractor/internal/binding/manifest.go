// Package binding extracts conditions that a workload declares explicitly.
//
// An extension ships a binding manifest describing the Go API it offers: which
// functions declare a condition, which options refine one, and what each maps
// onto in the profile. Extraction is therefore driven entirely by manifests
// rather than by knowledge of any particular extension, and adding vocabulary
// never requires changing this package.
package binding

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
	"gopkg.in/yaml.v3"
)

const (
	// BindingsManifest is the manifest name used by an extension catalog.
	BindingsManifest = "runtimeconditions.bindings.yaml"
	// PackageManifest is the manifest name a Go package ships alongside its
	// own source, so importing the package is enough to be understood.
	PackageManifest = "runtimeconditions.package.yaml"
	// defaultExtensionFile is the extension definition assumed to sit next to a
	// manifest that does not name one.
	defaultExtensionFile = "runtimeconditions.extension.yaml"
)

// Binding is one extension's Go API, as declared by its manifest.
type Binding struct {
	ManifestPath            string
	ExtensionID             string
	ExtensionDefinitionPath string
	ImportPath              string
	PackageName             string
	Constants               map[string]string
	Constructors            []Constructor
	Declarations            []Declaration
	Options                 []Option
}

// Constructor is a function returning a value whose methods declare conditions.
// Receiver names the type it returns, so method calls on the result can be
// attributed back to this binding.
type Constructor struct {
	Function string `yaml:"function"`
	Receiver string `yaml:"receiver"`
}

// Declaration is a call that produces one condition. It is either a package
// function or a method on a constructed receiver.
type Declaration struct {
	Function      string                 `yaml:"function"`
	Receiver      string                 `yaml:"receiver"`
	Method        string                 `yaml:"method"`
	Name          string                 `yaml:"name"`
	Kind          string                 `yaml:"kind"`
	InterfaceType string                 `yaml:"interfaceType"`
	NameArg       *int                   `yaml:"nameArg"`
	Configuration *profile.Configuration `yaml:"configuration"`
	Values        []Value                `yaml:"values"`
	Options       []Option               `yaml:"options"`
}

// Value is a fixed field the declaration always sets.
type Value struct {
	Target string `yaml:"target"`
	Value  string `yaml:"value"`
}

// Option is a call that refines the condition it is passed to. Target names the
// profile field it writes, and the value comes from a literal in the manifest or
// from an argument at a declared position.
//
// Options nest: an operation option carries its own request and response
// options, and an alternatives option carries the env options inside it.
type Option struct {
	Function                string         `yaml:"function"`
	Target                  string         `yaml:"target"`
	Value                   string         `yaml:"value"`
	ValueArg                *int           `yaml:"valueArg"`
	TypeArg                 *int           `yaml:"typeArg"`
	EngineArg               *int           `yaml:"engineArg"`
	Method                  string         `yaml:"method"`
	AppliesToKinds          []string       `yaml:"appliesToKinds"`
	AppliesToInterfaceTypes []string       `yaml:"appliesToInterfaceTypes"`
	StringArgs              map[string]int `yaml:"stringArgs"`
	Options                 []Option       `yaml:"options"`
}

// document is the on-disk manifest. Two kinds share this shape: a catalog
// binding names its extension under metadata, a package manifest names it under
// extension.
type document struct {
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
	Go struct {
		ImportPath   string            `yaml:"importPath"`
		Package      string            `yaml:"package"`
		Constants    map[string]string `yaml:"constants"`
		Constructors []Constructor     `yaml:"constructors"`
		Declarations []Declaration     `yaml:"declarations"`
		Options      []Option          `yaml:"options"`
	} `yaml:"go"`
}

// Read loads and validates a binding manifest.
//
// Every requirement here is refused rather than defaulted, because a manifest
// that half-parses produces a profile that is quietly missing conditions.
func Read(path string) (*Binding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed document
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if parsed.Kind != "RuntimeConditionsBinding" && parsed.Kind != "RuntimeConditionsPackage" {
		return nil, fmt.Errorf("%s: unsupported binding kind %q", path, parsed.Kind)
	}

	extensionID := parsed.Metadata.Extension
	extensionDefinition := parsed.Metadata.ExtensionDefinition
	if parsed.Kind == "RuntimeConditionsPackage" {
		extensionID = parsed.Extension.ID
		extensionDefinition = parsed.Extension.Definition
	}
	if extensionID == "" {
		if parsed.Kind == "RuntimeConditionsPackage" {
			return nil, fmt.Errorf("%s: extension.id is required", path)
		}
		return nil, fmt.Errorf("%s: metadata.extension is required", path)
	}
	if parsed.Metadata.Language == "" {
		return nil, fmt.Errorf("%s: metadata.language is required", path)
	}
	if parsed.Metadata.Language != "go" {
		return nil, fmt.Errorf("%s: metadata.language must be go", path)
	}
	if parsed.Go.ImportPath == "" {
		return nil, fmt.Errorf("%s: go.importPath is required", path)
	}
	if len(parsed.Go.Declarations) == 0 && len(parsed.Go.Options) == 0 {
		return nil, fmt.Errorf("%s: go.declarations or go.options must not be empty", path)
	}

	definitionPath, err := resolveExtensionDefinition(path, extensionDefinition)
	if err != nil {
		return nil, err
	}
	return &Binding{
		ManifestPath:            path,
		ExtensionID:             extensionID,
		ExtensionDefinitionPath: definitionPath,
		ImportPath:              parsed.Go.ImportPath,
		PackageName:             parsed.Go.Package,
		Constants:               parsed.Go.Constants,
		Constructors:            parsed.Go.Constructors,
		Declarations:            parsed.Go.Declarations,
		Options:                 parsed.Go.Options,
	}, nil
}

// resolveExtensionDefinition locates the extension definition a manifest binds
// to, defaulting to the conventional filename beside it. The definition must
// exist: a binding is only meaningful against the vocabulary it claims.
func resolveExtensionDefinition(manifestPath string, declared string) (string, error) {
	definitionPath := declared
	if definitionPath == "" {
		definitionPath = filepath.Join(filepath.Dir(manifestPath), defaultExtensionFile)
	}
	if !filepath.IsAbs(definitionPath) {
		definitionPath = filepath.Join(filepath.Dir(manifestPath), definitionPath)
	}
	if _, err := os.Stat(definitionPath); err != nil {
		return "", fmt.Errorf("%s: extension definition %q: %w", manifestPath, definitionPath, err)
	}
	return definitionPath, nil
}

// hasDeclaration, hasOption, and hasConstant answer whether a dot-imported name
// belongs to this binding, since a dot import offers no package qualifier to
// match on.

func (b *Binding) hasDeclaration(name string) bool {
	for _, declaration := range b.Declarations {
		if declaration.Function == name {
			return true
		}
	}
	return false
}

func (b *Binding) hasOption(name string) bool {
	if hasOption(b.Options, name) {
		return true
	}
	for _, declaration := range b.Declarations {
		if hasOption(declaration.Options, name) {
			return true
		}
	}
	return false
}

func (b *Binding) hasConstant(name string) bool {
	_, ok := b.Constants[name]
	return ok
}

func hasOption(options []Option, name string) bool {
	for _, option := range options {
		if option.Function == name || hasOption(option.Options, name) {
			return true
		}
	}
	return false
}

// displayName identifies a declaration in diagnostics, whichever form it takes.
func (d Declaration) displayName() string {
	if d.Function != "" {
		return d.Function
	}
	if d.Method != "" {
		return d.Method
	}
	return "declaration"
}
