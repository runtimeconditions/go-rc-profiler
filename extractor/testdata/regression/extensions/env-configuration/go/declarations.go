// Package envconfiguration provides typed no-op declaration helpers for the
// Environment Configuration Runtime Conditions extension.
package envconfiguration

import common "github.com/runtimeconditions/extensions/common-integrations/go"

// EnvOption configures an environment variable mapping declaration.
type EnvOption interface {
	envConfigurationEnvOption()
}

// ConditionOption configures workload-facing inputs for a Condition.
type ConditionOption interface {
	common.APIOption
	common.DatastoreOption
	common.CacheOption
	envConfigurationConditionOption()
}

type envOption struct{}

func (envOption) envConfigurationEnvOption() {}

type conditionOption struct{}

func (conditionOption) CommonIntegrationsAPIOption()       {}
func (conditionOption) CommonIntegrationsDatastoreOption() {}
func (conditionOption) CommonIntegrationsCacheOption()     {}
func (conditionOption) envConfigurationConditionOption()   {}

var (
	_ common.APIOption       = conditionOption{}
	_ common.DatastoreOption = conditionOption{}
	_ common.CacheOption     = conditionOption{}
)

// Env declares that a Condition property is supplied through an environment
// variable with the provided name.
func Env(property, name string, options ...EnvOption) ConditionOption {
	return conditionOption{}
}

// EnvAlternative declares one acceptable set of environment variables for a
// Condition. Platform adapters may choose any complete alternative they can
// satisfy.
func EnvAlternative(inputs ...ConditionOption) ConditionOption {
	return conditionOption{}
}

// Sensitive marks an environment variable mapping as sensitive.
func Sensitive() EnvOption {
	return envOption{}
}

// Optional marks an environment variable mapping as optional.
func Optional() EnvOption {
	return envOption{}
}
