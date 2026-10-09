package extensioncheck

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestGeneratedVocabularyEmitsDirectOwners(t *testing.T) {
	const base = "example:base"
	const additive = "example:additive"
	model := map[string]any{"extensions": []any{map[string]any{"id": base, "version": "1"}, map[string]any{"id": additive, "version": "1"}}, "vocabulary": map[string]any{
		"owners": []any{
			map[string]any{"coordinate": "kind:service", "category": "kind", "kind": "service", "owner": base},
			map[string]any{"coordinate": "interface:service:http", "category": "interface", "kind": "service", "interfaceType": "http", "owner": base},
		},
		"conditionFields": []any{
			map[string]any{"coordinate": "field:region", "owner": additive, "kind": "service", "interfaceType": "http", "segments": []any{map[string]any{"name": "region"}}},
		},
		"valueDomains": []any{
			map[string]any{"coordinate": "domain:region", "owner": additive, "kind": "service", "interfaceType": "http", "segments": []any{map[string]any{"name": "region"}}, "values": []any{map[string]any{"value": "eu"}}},
		},
	}}
	packages := map[string]*VerifiedGoPackage{"example.com/bindings": {Model: model}}
	condition := map[string]any{"kind": "service", "interface": map[string]any{"type": "http"}, "region": "eu"}
	got, err := validateGeneratedVocabulary([]any{condition}, [][]string{{"example.com/bindings"}}, packages)
	if err != nil {
		t.Fatal(err)
	}
	if want := []ExtensionReference{{ID: additive, Version: "1"}, {ID: base, Version: "1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("direct contributors: got %v, want %v", got, want)
	}
	condition["region"] = "unknown"
	if _, err := validateGeneratedVocabulary([]any{condition}, [][]string{{"example.com/bindings"}}, packages); err == nil || !strings.Contains(err.Error(), "outside domain") {
		t.Fatalf("expected value domain rejection, got %v", err)
	}
	delete(condition, "region")
	condition["unowned"] = true
	if _, err := validateGeneratedVocabulary([]any{condition}, [][]string{{"example.com/bindings"}}, packages); err == nil || !strings.Contains(err.Error(), "no vocabulary owner") {
		t.Fatalf("expected unknown vocabulary rejection, got %v", err)
	}
}

func TestGeneratedVocabularyClaimsSchemaRootFields(t *testing.T) {
	const owner = "urn:example:schema-owner"
	model := map[string]any{"extensions": []any{map[string]any{"id": owner, "version": "1"}}, "vocabulary": map[string]any{"owners": []any{
		map[string]any{"coordinate": "kind:deployment", "category": "kind", "kind": "deployment", "owner": owner},
		map[string]any{"coordinate": "interface:deployment:process", "category": "interface", "kind": "deployment", "interfaceType": "process", "owner": owner},
	}}}
	manifest := map[string]any{"rootBindings": []any{map[string]any{
		"role": "schema-field", "modelRef": map[string]any{"coordinate": "schema:deployment", "jsonPointer": "/properties/configuration"},
		"scope": map[string]any{"kind": "deployment", "interfaceType": "process"},
		"path":  []any{map[string]any{"name": "configuration"}},
	}}}
	packages := map[string]*VerifiedGoPackage{"example.com/binding": {
		ImportedGoPackage: ImportedGoPackage{ExtensionID: owner, ExtensionVersion: "1"}, Model: model, Manifest: manifest,
	}}
	condition := map[string]any{"kind": "deployment", "interface": map[string]any{"type": "process"}, "configuration": map[string]any{"image": "fixture"}}
	got, err := validateGeneratedVocabulary([]any{condition}, [][]string{{"example.com/binding"}}, packages)
	if err != nil || !reflect.DeepEqual(got, []ExtensionReference{{ID: owner, Version: "1"}}) {
		t.Fatalf("schema-field contributor: got %v, error %v", got, err)
	}
}

func TestGeneratedSchemaValidationIncludesUnscopedDependencies(t *testing.T) {
	const base = "example:base"
	const root = "example:root"
	baseSchema := map[string]any{"coordinate": "base#schema", "owner": base, "exact": map[string]any{
		"type": "object", "required": []any{"kind"}, "properties": map[string]any{"kind": map[string]any{"const": "service"}},
	}}
	rootSchema := map[string]any{"coordinate": "root#schema", "owner": root, "kind": "service", "interfaceType": "http", "exact": map[string]any{
		"type": "object", "required": []any{"region"}, "properties": map[string]any{"region": map[string]any{"const": "eu"}},
	}}
	packages := map[ExtensionReference]*VerifiedGoPackage{
		{ID: base, Version: "1"}: {Model: map[string]any{"extensions": []any{map[string]any{"id": base, "version": "1"}}, "schemas": []any{baseSchema}}},
		{ID: root, Version: "1"}: {Model: map[string]any{"extensions": []any{map[string]any{"id": root, "version": "1"}}, "schemas": []any{rootSchema}}},
	}
	condition := map[string]any{"kind": "service", "interface": map[string]any{"type": "http"}, "region": "eu"}
	if err := validateGeneratedSchemas([]any{condition}, map[ExtensionReference]bool{{ID: base, Version: "1"}: true, {ID: root, Version: "1"}: true}, packages); err != nil {
		t.Fatal(err)
	}
	condition["kind"] = "other"
	if err := validateGeneratedSchemas([]any{condition}, map[ExtensionReference]bool{{ID: base, Version: "1"}: true, {ID: root, Version: "1"}: true}, packages); err == nil || !strings.Contains(err.Error(), "base#schema") {
		t.Fatalf("expected unscoped dependency schema rejection, got %v", err)
	}
}

func TestGeneratedProfileRequiresInstalledCoreSchema(t *testing.T) {
	previous := installedCoreSchemaBase64
	installedCoreSchemaBase64 = ""
	t.Cleanup(func() { installedCoreSchemaBase64 = previous })
	if _, err := loadCoreProfileSchema(nil); err == nil || !strings.Contains(err.Error(), "installed profiler has no verified core profile schema") {
		t.Fatalf("expected missing installed schema to fail closed, got %v", err)
	}
	profile := map[string]any{"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsProfile", "conditions": []any{}}
	result, err := FinalizeGeneratedProfile(t.Context(), t.TempDir(), profile, nil)
	if result != nil || err == nil || !strings.Contains(err.Error(), "installed profiler has no verified core profile schema") {
		t.Fatalf("finalization must return no profile without external schema, got result %v, error %v", result, err)
	}
}

func TestInstalledCoreSchemaIdentityMatchesModel(t *testing.T) {
	previous := installedCoreSchemaBase64
	t.Cleanup(func() { installedCoreSchemaBase64 = previous })
	data := []byte("$schema: https://json-schema.org/draft/2020-12/schema\n$id: https://example.com/core.yaml\nx-runtimeconditions-version: 0.1.0\ntype: object\n")
	installedCoreSchemaBase64 = base64.StdEncoding.EncodeToString(data)
	document, err := parseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := semanticDigest(document)
	if err != nil {
		t.Fatal(err)
	}
	model := map[string]any{"coreProfileSchema": map[string]any{
		"id": "https://example.com/core.yaml", "version": "0.1.0", "semanticSha256": digest,
	}}
	if _, err := loadCoreProfileSchema([]*VerifiedGoPackage{{Model: model}}); err != nil {
		t.Fatal(err)
	}
	model["coreProfileSchema"].(map[string]any)["semanticSha256"] = strings.Repeat("0", 64)
	if _, err := loadCoreProfileSchema([]*VerifiedGoPackage{{Model: model}}); err == nil || !strings.Contains(err.Error(), "differs from model") {
		t.Fatalf("expected model/schema digest mismatch, got %v", err)
	}
}

func TestReleaseCoreSchemaMatchesVersionedSpec(t *testing.T) {
	if installedCoreSchemaBase64 == "" {
		t.Skip("plain go test has no release core schema")
	}
	model := map[string]any{"coreProfileSchema": map[string]any{
		"id":             "https://runtimeconditions.io/schemas/profile/0.4.0/runtimeconditions.profile.schema.yaml",
		"version":        "0.4.0",
		"semanticSha256": "ed447dccefd7507d905b0b6177b1386d8ee96a1d053b731939a9a7ae973d5af1",
	}}
	if _, err := loadCoreProfileSchema([]*VerifiedGoPackage{{Model: model}}); err != nil {
		t.Fatalf("installed release core schema does not match the versioned spec: %v", err)
	}
}

func TestGeneratedVocabularyRejectsConflictingClosureOwnership(t *testing.T) {
	makePackage := func(id string) *VerifiedGoPackage {
		return &VerifiedGoPackage{ImportedGoPackage: ImportedGoPackage{ExtensionID: id, ExtensionVersion: "1"}, Model: map[string]any{
			"extensions": []any{map[string]any{"id": id, "version": "1"}},
			"vocabulary": map[string]any{"owners": []any{map[string]any{
				"coordinate": "kind:service", "category": "kind", "kind": "service", "owner": id,
			}}},
		}}
	}
	if err := verifyVocabularyOwnership([]*VerifiedGoPackage{makePackage("first"), makePackage("second")}); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("expected closure ownership conflict, got %v", err)
	}
}
