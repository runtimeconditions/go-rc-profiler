package catalog

// ProfileDocument is a generated Runtime Conditions Profile, checked against the
// vocabulary its declared extensions resolve to.
type ProfileDocument struct {
	APIVersion string             `yaml:"apiVersion"`
	Kind       string             `yaml:"kind"`
	Extensions []string           `yaml:"extensions"`
	Conditions []ProfileCondition `yaml:"conditions"`
}

// ProfileCondition is one declared runtime dependency.
type ProfileCondition struct {
	Name          string                `yaml:"name"`
	Kind          string                `yaml:"kind"`
	Interface     ProfileInterface      `yaml:"interface"`
	Optional      bool                  `yaml:"optional"`
	Configuration *ProfileConfiguration `yaml:"configuration"`
}

// ProfileInterface describes how a workload reaches a dependency.
type ProfileInterface struct {
	Type        string             `yaml:"type"`
	Engine      string             `yaml:"engine"`
	BucketClass string             `yaml:"bucketClass"`
	Spec        *ProfileAPISpec    `yaml:"spec"`
	Operations  []ProfileOperation `yaml:"operations"`
	Subjects    []ProfileSubject   `yaml:"subjects"`
}

// ProfileAPISpec locates the API description a dependency publishes.
type ProfileAPISpec struct {
	Format  string `yaml:"format"`
	URI     string `yaml:"uri"`
	Version string `yaml:"version"`
}

// ProfileOperation is a single request a workload makes.
type ProfileOperation struct {
	Method            string `yaml:"method"`
	Path              string `yaml:"path"`
	RequestBodySchema any    `yaml:"requestBodySchema"`
	ResponseSchema    any    `yaml:"responseSchema"`
}

// ProfileSubject is a messaging subject a workload publishes or subscribes to.
type ProfileSubject struct {
	Name          string `yaml:"name"`
	Direction     string `yaml:"direction"`
	PayloadSchema any    `yaml:"payloadSchema"`
}

// ProfileConfiguration is how a workload is told where a dependency lives.
type ProfileConfiguration struct {
	Env          []ProfileEnvInput                 `yaml:"env"`
	Alternatives []ProfileConfigurationAlternative `yaml:"alternatives"`
}

// ProfileConfigurationAlternative is one acceptable configuration shape.
type ProfileConfigurationAlternative struct {
	Env []ProfileEnvInput `yaml:"env"`
}

// ProfileEnvInput binds a connection property to an environment variable.
type ProfileEnvInput struct {
	Property string `yaml:"property"`
	Name     string `yaml:"name"`
}
