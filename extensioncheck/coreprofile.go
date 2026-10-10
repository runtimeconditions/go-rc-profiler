package extensioncheck

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Set by the release build from the separately versioned core schema artifact.
// Plain go build leaves it empty and generated profile validation fails closed.
// The schema source is never checked into this profiler repository.
var installedCoreSchemaBase64 string

func loadCoreProfileSchema(packages []*VerifiedGoPackage) (*jsonschema.Schema, error) {
	if installedCoreSchemaBase64 == "" {
		return nil, fmt.Errorf("installed profiler has no verified core profile schema")
	}
	data, err := base64.StdEncoding.DecodeString(installedCoreSchemaBase64)
	if err != nil {
		return nil, fmt.Errorf("installed core profile schema encoding: %w", err)
	}
	document, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	if stringValue(document, "$schema") != "https://json-schema.org/draft/2020-12/schema" {
		return nil, fmt.Errorf("core profile schema must use JSON Schema Draft 2020-12")
	}
	id := stringValue(document, "$id")
	version := stringValue(document, "x-runtimeconditions-version")
	if id == "" || version == "" {
		return nil, fmt.Errorf("core profile schema has no $id or version")
	}
	digest, err := semanticDigest(document)
	if err != nil {
		return nil, err
	}
	for _, binding := range packages {
		identity := object(binding.Model, "coreProfileSchema")
		if stringValue(identity, "id") != id || stringValue(identity, "semanticSha256") != digest || stringValue(identity, "version") != version {
			if !compatibleCoreProfileInput(identity, id, version, digest) {
				return nil, fmt.Errorf("core profile schema identity, version, or digest differs from model in %s", binding.ImportPath)
			}
		}
	}
	return compileDraft2020(document, id)
}

// Published v0.1.0 bindings were normalized against core 0.2.0. Core 0.4.0
// retains that profile structure and relaxes extension identifier syntax to
// accept complete definition URIs. Validate output with the installed 0.4.0
// schema while accepting only this exact, approved older generator input.
// Both digests are required; a version label alone cannot authorize migration.
func compatibleCoreProfileInput(identity map[string]any, installedID, installedVersion, installedDigest string) bool {
	return installedID == "https://runtimeconditions.io/schemas/profile/0.4.0/runtimeconditions.profile.schema.yaml" &&
		installedVersion == "0.4.0" &&
		installedDigest == "ed447dccefd7507d905b0b6177b1386d8ee96a1d053b731939a9a7ae973d5af1" &&
		stringValue(identity, "id") == "https://runtimeconditions.io/schemas/profile/0.2.0/runtimeconditions.profile.schema.yaml" &&
		stringValue(identity, "version") == "0.2.0" &&
		stringValue(identity, "semanticSha256") == "a090a8016d045f9c3fa872a67f8df293b77ca2809a1bea5ae9fa31a27a06109a"
}

func compileDraft2020(document map[string]any, uri string) (*jsonschema.Schema, error) {
	if dialect := stringValue(document, "$schema"); dialect != "" && dialect != "https://json-schema.org/draft/2020-12/schema" {
		return nil, fmt.Errorf("schema %s uses unsupported dialect %s", uri, dialect)
	}
	if err := localReferencesOnly(document); err != nil {
		return nil, fmt.Errorf("schema %s: %w", uri, err)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource(uri, resource); err != nil {
		return nil, err
	}
	return compiler.Compile(uri)
}

func localReferencesOnly(value any) error {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if key == "$ref" || key == "$dynamicRef" {
				ref, ok := child.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					return fmt.Errorf("external schema reference %v is unavailable", child)
				}
			}
			if err := localReferencesOnly(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range item {
			if err := localReferencesOnly(child); err != nil {
				return err
			}
		}
	}
	return nil
}
