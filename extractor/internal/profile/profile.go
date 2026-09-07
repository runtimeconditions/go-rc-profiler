// Package profile defines the Runtime Conditions Profile document model.
//
// The types here are the shared currency of extraction: the declarative
// binding path and the SDK mapping path both produce Conditions, and the
// parent extractor package re-exports these types as its public API. The YAML
// tags are the serialized contract, so changing them changes every emitted
// profile.
package profile

// Profile is the complete document emitted for one workload.
type Profile struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   Metadata    `yaml:"metadata"`
	Workload   Workload    `yaml:"workload"`
	Extensions []string    `yaml:"extensions,omitempty"`
	Conditions []Condition `yaml:"conditions"`
}

// Metadata names the profile itself.
type Metadata struct {
	Name string `yaml:"name"`
}

// Workload identifies the deployable unit the profile describes.
type Workload struct {
	URI     string `yaml:"uri,omitempty"`
	Version string `yaml:"version,omitempty"`
}

// Condition is a single declared runtime integration.
type Condition struct {
	Name          string         `yaml:"name,omitempty"`
	Kind          string         `yaml:"kind"`
	Interface     Interface      `yaml:"interface"`
	Optional      bool           `yaml:"optional,omitempty"`
	Configuration *Configuration `yaml:"configuration,omitempty"`
}

// Interface describes how the workload talks to the dependency.
type Interface struct {
	Type        string      `yaml:"type"`
	Engine      string      `yaml:"engine,omitempty"`
	BucketClass string      `yaml:"bucketClass,omitempty"`
	Spec        *APISpec    `yaml:"spec,omitempty"`
	Operations  []Operation `yaml:"operations,omitempty"`
	Subjects    []Subject   `yaml:"subjects,omitempty"`
}

// APISpec points at the contract document for an API interface.
type APISpec struct {
	Format  string `yaml:"format"`
	URI     string `yaml:"uri"`
	Version string `yaml:"version,omitempty"`
}

// Operation is one call the workload makes against the dependency.
//
// Fields is inlined, which is what lets extension vocabulary contribute
// operation keys the profiler has no compile-time knowledge of.
type Operation struct {
	Method            string         `yaml:"method,omitempty"`
	Path              string         `yaml:"path,omitempty"`
	RequestBodySchema any            `yaml:"requestBodySchema,omitempty"`
	ResponseSchema    any            `yaml:"responseSchema,omitempty"`
	Fields            map[string]any `yaml:",inline"`
}

// Subject is one named channel on a messaging interface.
type Subject struct {
	Name          string `yaml:"name"`
	Direction     string `yaml:"direction"`
	PayloadSchema any    `yaml:"payloadSchema,omitempty"`
}

// Configuration records how the dependency is addressed at runtime. Env and
// Alternatives are mutually exclusive: a condition either has one fixed set of
// inputs or a choice between several.
type Configuration struct {
	Env          []EnvInput                 `yaml:"env,omitempty"`
	Alternatives []ConfigurationAlternative `yaml:"alternatives,omitempty"`
}

// ConfigurationAlternative is one acceptable set of environment inputs.
type ConfigurationAlternative struct {
	Env []EnvInput `yaml:"env"`
}

// EnvInput binds an interface property to an environment variable.
type EnvInput struct {
	Property  string `yaml:"property"`
	Name      string `yaml:"name"`
	Sensitive bool   `yaml:"sensitive,omitempty"`
	Required  *bool  `yaml:"required,omitempty"`
}

// Clone deep-copies the configuration so a template declared once in a binding
// manifest can seed many conditions without them sharing slices.
func (c *Configuration) Clone() *Configuration {
	if c == nil {
		return nil
	}
	clone := &Configuration{}
	if len(c.Env) > 0 {
		clone.Env = append([]EnvInput(nil), c.Env...)
	}
	if len(c.Alternatives) > 0 {
		clone.Alternatives = make([]ConfigurationAlternative, 0, len(c.Alternatives))
		for _, alternative := range c.Alternatives {
			clone.Alternatives = append(clone.Alternatives, ConfigurationAlternative{
				Env: append([]EnvInput(nil), alternative.Env...),
			})
		}
	}
	return clone
}
