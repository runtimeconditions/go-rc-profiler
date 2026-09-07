package extractor

import "github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"

// The Runtime Conditions Profile document model. These are aliases rather than
// distinct types, so a profile produced anywhere inside the extractor is the
// same value callers receive, with no conversion and no chance of the public
// shape drifting from the one that is actually emitted.
type (
	// RuntimeConditionsProfile is the YAML shape emitted by the profiler.
	RuntimeConditionsProfile = profile.Profile

	Metadata                 = profile.Metadata
	Workload                 = profile.Workload
	Condition                = profile.Condition
	Interface                = profile.Interface
	APISpec                  = profile.APISpec
	Operation                = profile.Operation
	Subject                  = profile.Subject
	Configuration            = profile.Configuration
	ConfigurationAlternative = profile.ConfigurationAlternative
	EnvInput                 = profile.EnvInput
)
