package check

import (
	"bytes"
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// compileConditionSchema compiles the JSON Schema an extension declares inline.
func compileConditionSchema(value any) (*jsonschema.Schema, error) {
	parsed, err := jsonSchemaValue(value)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	location := "runtimeconditions-extension-schema.json"
	if err := compiler.AddResource(location, parsed); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

// jsonSchemaValue converts a YAML-decoded value into the JSON representation the
// schema compiler and validator expect.
func jsonSchemaValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}
