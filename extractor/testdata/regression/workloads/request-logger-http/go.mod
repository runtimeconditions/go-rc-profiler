module github.com/runtimeconditions/rc-demos/apps/request-logger-http

go 1.25.0

require (
	github.com/runtimeconditions/extensions/common-integrations/go v0.0.0
	github.com/runtimeconditions/extensions/env-configuration/go v0.0.0
)

replace github.com/runtimeconditions/extensions/common-integrations/go => ../../extensions/common-integrations/go

replace github.com/runtimeconditions/extensions/env-configuration/go => ../../extensions/env-configuration/go
