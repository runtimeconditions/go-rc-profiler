// Package sdkmap extracts conditions from ordinary SDK calls.
//
// Where the binding package needs a workload to declare its dependencies, this
// package infers them: an SDK ships a mapping describing which of its calls
// establish a dependency and which perform operations on one, and the profiler
// replays that description against the workload's source.
//
// Because a mapping is installed metadata rather than first-party source, it is
// only trusted after its digests, module version, and extension reference all
// check out. Anything short of that stops profiling instead of narrowing it.
package sdkmap

// indexPath is where a module publishes the index of its mappings.
const indexPath = "runtimeconditions/index.yaml"

// Mapping is one verified SDK mapping document.
type Mapping struct {
	Path      string
	Extension ExtensionReference
	Calls     []Call
}

// ExtensionReference pins the exact extension release a mapping was authored
// against. All three fields must match an installed definition.
type ExtensionReference struct {
	ID             string `yaml:"id"`
	Version        string `yaml:"version"`
	SemanticSHA256 string `yaml:"semanticSha256"`
}

// Call maps one SDK symbol onto a condition, a piece of tracked state, or both.
//
// A call that produces state establishes a dependency, such as opening a
// connection. A call that requires state performs an operation on one. Requiring
// state is what keeps operations attached to the dependency they belong to.
type Call struct {
	ExtensionID       string                 `yaml:"-"`
	ID                string                 `yaml:"id"`
	Symbol            Symbol                 `yaml:"symbol"`
	ReceiverState     string                 `yaml:"receiverState"`
	ArgumentState     *ArgumentState         `yaml:"argumentState"`
	ConditionTemplate ConditionTemplate      `yaml:"conditionTemplate"`
	OperationBindings map[string]ValueSource `yaml:"operationBindings"`
	Produces          *StateProduction       `yaml:"produces"`
}

// Symbol identifies the SDK function or method a call refers to. Exactly one of
// Function and Method is set.
type Symbol struct {
	Package  string `yaml:"package"`
	Function string `yaml:"function"`
	Receiver string `yaml:"receiver"`
	Method   string `yaml:"method"`
}

// ConditionTemplate is the condition a call contributes, with the fixed parts
// filled in. Operation values that vary per call site come from
// Call.OperationBindings.
type ConditionTemplate struct {
	Kind          string         `yaml:"kind"`
	InterfaceType string         `yaml:"interfaceType"`
	Operation     map[string]any `yaml:"operation"`
}

// ValueSource says where a value comes from: an argument at the call site, or
// state carried by the receiver. An optional value that cannot be resolved is
// omitted rather than discarding the whole condition.
type ValueSource struct {
	Argument *ArgumentSource `yaml:"argument"`
	State    string          `yaml:"state"`
	Optional bool            `yaml:"optional"`
}

// ArgumentSource locates a value in a call's arguments, by parameter name or
// position, optionally reaching into a struct literal field.
type ArgumentSource struct {
	Parameter string `yaml:"parameter"`
	Position  *int   `yaml:"position"`
	Field     string `yaml:"field"`
}

// ArgumentState requires that an argument carry state of a given type, for calls
// that take their dependency as a parameter rather than a receiver.
type ArgumentState struct {
	StateType string         `yaml:"stateType"`
	Argument  ArgumentSource `yaml:"argument"`
}

// StateProduction describes the state a call establishes.
//
// DependencyIdentity controls grouping. "new" starts a fresh identity, so
// operations on the result collapse into one condition. "inherit" carries the
// identity of the receiver or argument forward, keeping a derived handle
// attached to the dependency it came from.
type StateProduction struct {
	StateType          string                 `yaml:"stateType"`
	DependencyIdentity string                 `yaml:"dependencyIdentity"`
	Bindings           map[string]ValueSource `yaml:"bindings"`
}

// document is the on-disk mapping.
type document struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name           string `yaml:"name"`
		Module         string `yaml:"module"`
		ModuleVersion  string `yaml:"moduleVersion"`
		Language       string `yaml:"language"`
		SemanticSHA256 string `yaml:"semanticSha256"`
	} `yaml:"metadata"`
	Extension ExtensionReference `yaml:"extension"`
	Go        struct {
		Calls []Call `yaml:"calls"`
	} `yaml:"go"`
}

// index is the module-level list of mappings, digest-pinned.
type index struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Module        string `yaml:"module"`
		ModuleVersion string `yaml:"moduleVersion"`
		Language      string `yaml:"language"`
	} `yaml:"metadata"`
	Mappings []struct {
		Name   string `yaml:"name"`
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
	} `yaml:"mappings"`
}
