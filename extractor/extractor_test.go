package extractor

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/binding"
	"gopkg.in/yaml.v3"
)

func TestExtractDirWithCrossExtensionEnvOptions(t *testing.T) {
	dir := t.TempDir()
	commonPath := extensionModulePath(t, "common-integrations")
	envPath := extensionModulePath(t, "env-configuration")
	writeModule(t, dir, map[string]string{
		"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
		"github.com/runtimeconditions/extensions/env-configuration/go":   envPath,
	})

	source := `package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

const (
	todoPath = "/todos"
)

type CreateTodoRequest struct {
	UserID int ` + "`json:\"userId\"`" + `
	Title string ` + "`json:\"title\"`" + `
}

type Todo struct {
	ID int ` + "`json:\"id\"`" + `
	UserID int ` + "`json:\"userId\"`" + `
	Title string ` + "`json:\"title\"`" + `
	Completed bool ` + "`json:\"completed\"`" + `
}

var _ = common.API("todos-api",
	common.POST(todoPath, common.Request[CreateTodoRequest](), common.Response[Todo]()),
	env.Env("baseUrl", "TODOS_API_URL"),
)

var _ = common.Datastore("primary-db", common.Relational(common.MySQL))
var _ = common.Cache("todo-cache",
	common.KeyValue(common.Redis),
	env.EnvAlternative(env.Env("url", "REDIS_URL")),
	env.EnvAlternative(env.Env("hostname", "REDIS_HOST"), env.Env("port", "REDIS_PORT")),
)
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := ExtractDir(dir, Options{
		Name:            "todos",
		WorkloadURI:     "github.com/example/todos",
		WorkloadVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(profile.Extensions, []string{
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml",
	}) {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	resolvedExtensions := resolveExtensionsForTest(t, profile.Extensions, map[string]string{
		"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml":   filepath.Join(envPath, "..", "env-configuration-v1alpha1.yaml"),
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml": filepath.Join(envPath, "..", "..", "common-integrations", "common-integrations-v1alpha1.yaml"),
	})
	if !slices.Contains(resolvedExtensions, "https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml") {
		t.Fatalf("resolved extensions do not include common integrations: %#v", resolvedExtensions)
	}
	if len(profile.Conditions) != 3 {
		t.Fatalf("expected 3 conditions, got %d", len(profile.Conditions))
	}

	api := profile.Conditions[0]
	if api.Name != "todos-api" || api.Kind != "api" || api.Interface.Type != "http" {
		t.Fatalf("unexpected api condition: %#v", api)
	}
	if got := api.Interface.Operations[0].RequestBodySchema.(map[string]any)["userId"]; got != "integer" {
		t.Fatalf("unexpected request schema userId: %#v", got)
	}
	if got := api.Interface.Operations[0].ResponseSchema.(map[string]any)["completed"]; got != "boolean" {
		t.Fatalf("unexpected response schema completed: %#v", got)
	}
	if api.Configuration == nil || len(api.Configuration.Env) != 1 || api.Configuration.Env[0].Name != "TODOS_API_URL" {
		t.Fatalf("unexpected api configuration: %#v", api.Configuration)
	}

	datastore := profile.Conditions[1]
	if datastore.Kind != "datastore" || datastore.Interface.Type != "relational" || datastore.Interface.Engine != "mysql" {
		t.Fatalf("unexpected datastore condition: %#v", datastore)
	}

	cache := profile.Conditions[2]
	if cache.Kind != "cache" || cache.Interface.Type != "key_value" || cache.Interface.Engine != "redis" {
		t.Fatalf("unexpected cache condition: %#v", cache)
	}
	if cache.Configuration == nil || len(cache.Configuration.Alternatives) != 2 {
		t.Fatalf("unexpected cache configuration: %#v", cache.Configuration)
	}

}

func TestExtractDirWithCommonIntegrationsOnly(t *testing.T) {
	dir := t.TempDir()
	commonPath := extensionModulePath(t, "common-integrations")
	writeModule(t, dir, map[string]string{
		"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
	})

	source := `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

var _ = common.Cache("todo-cache", common.KeyValue(common.Redis))
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := ExtractDir(dir, Options{
		Name:            "todos",
		WorkloadURI:     "github.com/example/todos",
		WorkloadVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(profile.Extensions, []string{"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml"}) {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	if len(profile.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(profile.Conditions))
	}
	condition := profile.Conditions[0]
	if condition.Kind != "cache" || condition.Interface.Type != "key_value" || condition.Interface.Engine != "redis" {
		t.Fatalf("unexpected condition: %#v", condition)
	}
	if condition.Configuration != nil {
		t.Fatalf("common-only condition should not include env configuration: %#v", condition.Configuration)
	}
}

func TestExtractDirWithUnusedEnvConfigurationPackage(t *testing.T) {
	dir := t.TempDir()
	commonPath := extensionModulePath(t, "common-integrations")
	envPath := extensionModulePath(t, "env-configuration")
	writeModule(t, dir, map[string]string{
		"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
		"github.com/runtimeconditions/extensions/env-configuration/go":   envPath,
	})

	source := `package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

var _ = env.Sensitive()
var _ = common.Cache("todo-cache", common.KeyValue(common.Redis))
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := ExtractDir(dir, Options{
		Name:            "todos",
		WorkloadURI:     "github.com/example/todos",
		WorkloadVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(profile.Extensions, []string{"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml"}) {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	if len(profile.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(profile.Conditions))
	}
	if profile.Conditions[0].Configuration != nil {
		t.Fatalf("unused env package should not configure condition: %#v", profile.Conditions[0].Configuration)
	}
}

func TestCheckedInExtensionBindingsRoundTripProfiles(t *testing.T) {
	commonPath := extensionModulePath(t, "common-integrations")
	envPath := extensionModulePath(t, "env-configuration")
	catalog := loadExtensionCatalogForTest(t)

	tests := []struct {
		name             string
		source           string
		replacements     map[string]string
		targetBindingDir string
		wantExtensions   []string
		wantResolved     []string
	}{
		{
			name: "common-integrations",
			source: `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

const todoPath = "/todos/{id}"

type Todo struct {
	ID int ` + "`json:\"id\"`" + `
	Title string ` + "`json:\"title\"`" + `
	Completed bool ` + "`json:\"completed\"`" + `
}

var _ = common.API("todos-api",
	common.Spec("openapi", "catalog://api/default/todos-api", "1.0.0"),
	common.GET(todoPath, common.Response[Todo]()),
)
var _ = common.Datastore("primary-db", common.Relational(common.Postgres))
var _ = common.Cache("request-cache", common.KeyValue(common.Redis))
`,
			replacements: map[string]string{
				"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
			},
			targetBindingDir: commonPath,
			wantExtensions: []string{
				"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
			},
			wantResolved: []string{
				"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
			},
		},
		{
			name: "env-configuration",
			source: `package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

var _ = common.API("todos-api",
	common.Spec("openapi", "catalog://api/default/todos-api", "1.0.0"),
	common.GET("/todos/{id}"),
	env.Env("baseUrl", "TODOS_API_URL"),
)
var _ = common.Cache("request-cache",
	common.KeyValue(common.Redis),
	env.EnvAlternative(env.Env("url", "REDIS_URL")),
	env.EnvAlternative(
		env.Env("hostname", "REDIS_HOST"),
		env.Env("port", "REDIS_PORT", env.Optional()),
	),
)
`,
			replacements: map[string]string{
				"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
				"github.com/runtimeconditions/extensions/env-configuration/go":   envPath,
			},
			targetBindingDir: envPath,
			wantExtensions: []string{
				"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
				"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml",
			},
			wantResolved: []string{
				"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
				"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeModule(t, dir, test.replacements)
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(test.source), 0o644); err != nil {
				t.Fatal(err)
			}

			profile, err := ExtractDir(dir, Options{
				Name:            "roundtrip-" + test.name,
				WorkloadURI:     "github.com/example/roundtrip-" + test.name,
				WorkloadVersion: "v0.1.0",
			})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(profile.Extensions, test.wantExtensions) {
				t.Fatalf("unexpected extensions: got %#v want %#v", profile.Extensions, test.wantExtensions)
			}

			var roundTripped RuntimeConditionsProfile
			data, err := yaml.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(data, &roundTripped); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*profile, roundTripped) {
				t.Fatalf("profile changed after YAML round trip:\noriginal: %#v\nroundtrip: %#v", *profile, roundTripped)
			}

			resolved := resolveExtensionDefinitionsForTest(t, profile.Extensions, catalog)
			if got := resolved.ids(); !slices.Equal(got, test.wantResolved) {
				t.Fatalf("unexpected resolved extensions: got %#v want %#v", got, test.wantResolved)
			}
			validateProfileVocabularyForTest(t, roundTripped, resolved)

			manifestPath, ok, err := binding.FindPackageManifest(test.targetBindingDir)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatalf("missing Runtime Conditions binding manifest in %s", test.targetBindingDir)
			}
			binding, err := binding.Read(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			validateGoBindingVocabularyForTest(t, binding, resolveExtensionDefinitionsForTest(t, []string{binding.ExtensionID}, catalog))
		})
	}
}

func TestDiscoverGoBindingsAcceptsBindingsManifestName(t *testing.T) {
	root := t.TempDir()
	bindingDir := filepath.Join(root, "example", "go")
	if err := os.MkdirAll(bindingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definition := filepath.Join(root, "example", "example-v1alpha1.yaml")
	if err := os.WriteFile(definition, []byte(`apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/example/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: example
`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(bindingDir, binding.BindingsManifest)
	if err := os.WriteFile(manifest, []byte(`apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/example/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../example-v1alpha1.yaml
  language: go

go:
  importPath: github.com/example/runtimeconditions/example/go
  package: example

  declarations:
    - function: Example
      nameArg: 0
      kind: example
`), 0o644); err != nil {
		t.Fatal(err)
	}

	bindings, err := binding.Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}
	if bindings[0].ExtensionID != "https://example.com/runtimeconditions/example/v1alpha1/runtimeconditions.extension.yaml" {
		t.Fatalf("unexpected extension id: %s", bindings[0].ExtensionID)
	}
}

func TestExtractDirWithThirdPartyPackageManifest(t *testing.T) {
	dir := t.TempDir()
	sdkPath := writePackageManifestSDK(t)
	mod := `module github.com/example/audit-logger

go 1.25.0

require github.com/example/eventstream v0.0.0

replace github.com/example/eventstream => ` + sdkPath + `
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}

	source := `package main

import (
	"context"

	"github.com/example/eventstream/service/events"
)

func writeAuditLog(ctx context.Context) error {
	client := events.NewClient(events.Config{})
	err := client.Publish(ctx, events.Event{})
	return err
}
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := ExtractDir(dir, Options{
		Name:            "audit-logger",
		WorkloadURI:     "github.com/example/audit-logger",
		WorkloadVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(profile.Extensions, []string{"https://example.com/runtimeconditions/event-sink/v1alpha1/runtimeconditions.extension.yaml"}) {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	if len(profile.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(profile.Conditions))
	}

	condition := profile.Conditions[0]
	if condition.Name != "audit-events" || condition.Kind != "example.event_sink" {
		t.Fatalf("unexpected condition identity: %#v", condition)
	}
	if condition.Interface.Type != "event.stream" {
		t.Fatalf("unexpected interface: %#v", condition.Interface)
	}
	if condition.Configuration == nil || len(condition.Configuration.Env) != 2 {
		t.Fatalf("unexpected configuration: %#v", condition.Configuration)
	}
	if condition.Configuration.Env[1].Name != "EVENTSTREAM_TOKEN" || !condition.Configuration.Env[1].Sensitive {
		t.Fatalf("unexpected credential configuration: %#v", condition.Configuration.Env[1])
	}
}

func TestExtractDirGoSemanticFixtures(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		workloadURI string
		golden      string
	}{
		{
			name:        "semantic-imported-symbols",
			fixture:     "imported-symbols",
			workloadURI: "github.com/example/runtimeconditions/semantic-imported-symbols",
			golden:      "testdata/golden/semantic-imported-symbols.golden.yaml",
		},
		{
			name:        "semantic-sdk-receiver",
			fixture:     "sdk-receiver",
			workloadURI: "github.com/example/runtimeconditions/semantic-sdk-receiver",
			golden:      "testdata/golden/semantic-sdk-receiver.golden.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, err := filepath.Abs(filepath.Join("testdata", "go-semantic", test.fixture))
			if err != nil {
				t.Fatal(err)
			}

			profile, err := ExtractDir(dir, Options{
				Name:              test.name,
				WorkloadURI:       test.workloadURI,
				WorkloadVersion:   "v0.1.0",
				RequireGoPackages: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := yaml.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(test.golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != string(want) {
				t.Fatalf("semantic fixture profile mismatch\n--- got ---\n%s\n--- want ---\n%s", data, want)
			}
		})
	}
}

func TestExtractDirWithGoPackagesSemanticResolution(t *testing.T) {
	dir := t.TempDir()
	commonPath := extensionModulePath(t, "common-integrations")
	envPath := extensionModulePath(t, "env-configuration")
	writeModule(t, dir, map[string]string{
		"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
		"github.com/runtimeconditions/extensions/env-configuration/go":   envPath,
	})
	writeFilesForTest(t, map[string]string{
		filepath.Join(dir, "settings", "settings.go"): `package settings

const (
	APIName = "todos-api"
	TodoPath = "/todos"
	BaseURLEnv = "TODOS_API_URL"
)
`,
		filepath.Join(dir, "models", "models.go"): `package models

type CreateTodoRequest struct {
	UserID int ` + "`json:\"userId\"`" + `
	Title string ` + "`json:\"title\"`" + `
}

type Todo struct {
	ID int ` + "`json:\"id\"`" + `
	Completed bool ` + "`json:\"completed\"`" + `
}
`,
		filepath.Join(dir, "main.go"): `package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
	"github.com/example/testapp/models"
	"github.com/example/testapp/settings"
)

type RequestAlias = models.CreateTodoRequest

var _ = common.API(settings.APIName,
	common.POST(settings.TodoPath, common.Request[RequestAlias](), common.Response[models.Todo]()),
	env.Env("baseUrl", settings.BaseURLEnv),
)
`,
	})

	profile, err := ExtractDir(dir, Options{
		Name:              "semantic-resolution",
		WorkloadURI:       "github.com/example/semantic-resolution",
		WorkloadVersion:   "v0.1.0",
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(profile.Extensions, []string{
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml",
	}) {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	if len(profile.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(profile.Conditions))
	}
	condition := profile.Conditions[0]
	if condition.Name != "todos-api" || condition.Interface.Operations[0].Path != "/todos" {
		t.Fatalf("semantic constants were not resolved: %#v", condition)
	}
	requestSchema, ok := condition.Interface.Operations[0].RequestBodySchema.(map[string]any)
	if !ok || requestSchema["userId"] != "integer" {
		t.Fatalf("semantic request schema was not resolved: %#v", condition.Interface.Operations[0].RequestBodySchema)
	}
	if condition.Configuration == nil || len(condition.Configuration.Env) != 1 || condition.Configuration.Env[0].Name != "TODOS_API_URL" {
		t.Fatalf("semantic env constant was not resolved: %#v", condition.Configuration)
	}
}

func TestExtractDirWithGoPackagesReceiverResolution(t *testing.T) {
	dir := t.TempDir()
	sdkPath := writePackageManifestSDK(t)
	writeModule(t, dir, map[string]string{
		"github.com/example/eventstream": sdkPath,
	})

	source := `package main

import (
	"context"

	"github.com/example/eventstream/service/events"
)

func writeAuditLog(ctx context.Context, client *events.Client) error {
	return client.Publish(ctx, events.Event{})
}
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := ExtractDir(dir, Options{
		Name:              "semantic-receiver",
		WorkloadURI:       "github.com/example/semantic-receiver",
		WorkloadVersion:   "v0.1.0",
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Conditions) != 1 {
		t.Fatalf("expected semantic receiver resolution to produce 1 condition, got %d", len(profile.Conditions))
	}
	if profile.Conditions[0].Kind != "example.event_sink" {
		t.Fatalf("unexpected condition: %#v", profile.Conditions[0])
	}
}

func TestExtractDirValidatesPackageManifestBeforeExtraction(t *testing.T) {
	dir := t.TempDir()
	sdkPath := writePackageManifestSDK(t)
	manifestPath := filepath.Join(sdkPath, "service", "events", binding.PackageManifest)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "https://example.com/runtimeconditions/event-sink/v1alpha1/runtimeconditions.extension.yaml", "https://example.com/runtimeconditions/other/v1alpha1/runtimeconditions.extension.yaml", 1))
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	writeModule(t, dir, map[string]string{
		"github.com/example/eventstream": sdkPath,
	})
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"context"
	"github.com/example/eventstream/service/events"
)

func writeAuditLog(ctx context.Context) error {
	client := events.NewClient(events.Config{})
	return client.Publish(ctx, events.Event{})
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ExtractDir(dir, Options{
		Name:            "invalid-package-manifest",
		WorkloadURI:     "github.com/example/invalid-package-manifest",
		WorkloadVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected package manifest validation error")
	}
	if !strings.Contains(err.Error(), "binding extension id https://example.com/runtimeconditions/other/v1alpha1/runtimeconditions.extension.yaml does not match extension definition https://example.com/runtimeconditions/event-sink/v1alpha1/runtimeconditions.extension.yaml") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractDirValidatesGeneratedProfile(t *testing.T) {
	dir := t.TempDir()
	sdkPath := writePackageManifestSDK(t)
	manifestPath := filepath.Join(sdkPath, "service", "events", binding.PackageManifest)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "property: token", "property: credential", 1))
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	writeModule(t, dir, map[string]string{
		"github.com/example/eventstream": sdkPath,
	})
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"context"
	"github.com/example/eventstream/service/events"
)

func writeAuditLog(ctx context.Context) error {
	client := events.NewClient(events.Config{})
	return client.Publish(ctx, events.Event{})
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ExtractDir(dir, Options{
		Name:            "invalid-generated-profile",
		WorkloadURI:     "github.com/example/invalid-generated-profile",
		WorkloadVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected generated profile validation error")
	}
	if !strings.Contains(err.Error(), "configuration.env[1].property credential") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractDirWithCheckedInAWSSDKPackageManifest(t *testing.T) {
	dir := t.TempDir()
	sdkPath := workspacePath(t, "spec", "examples", "sdks", "aws-sdk-go-v2")
	writeModule(t, dir, map[string]string{
		"github.com/runtimeconditions/spec/examples/sdks/aws-sdk-go-v2": sdkPath,
	})

	source := `package main

import (
	"context"

	"github.com/runtimeconditions/spec/examples/sdks/aws-sdk-go-v2/service/s3"
)

func writeObject(ctx context.Context) error {
	client := s3.NewFromConfig(s3.Config{})
	bucket := "audit-log-bucket"
	key := "events.json"
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key: &key,
	})
	return err
}
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, err := ExtractDir(dir, Options{
		Name:            "aws-sdk-example",
		WorkloadURI:     "github.com/example/aws-sdk-example",
		WorkloadVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(profile.Extensions, []string{"https://aws.example.com/runtimeconditions/object-store/v1alpha1/runtimeconditions.extension.yaml"}) {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	if len(profile.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(profile.Conditions))
	}
	condition := profile.Conditions[0]
	if condition.Kind != "aws.object_store" || condition.Interface.Type != "aws.s3" || condition.Interface.BucketClass != "standard" {
		t.Fatalf("unexpected AWS SDK condition: %#v", condition)
	}
	if condition.Configuration == nil || len(condition.Configuration.Env) != 4 {
		t.Fatalf("unexpected AWS SDK configuration: %#v", condition.Configuration)
	}
}

func TestExtractDirGoldenProfiles(t *testing.T) {
	commonPath := extensionModulePath(t, "common-integrations")
	envPath := extensionModulePath(t, "env-configuration")

	tests := []struct {
		name         string
		source       string
		replacements map[string]string
		golden       string
	}{
		{
			name: "common-profile",
			source: `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

type Todo struct {
	ID int ` + "`json:\"id\"`" + `
	Title string ` + "`json:\"title\"`" + `
	Completed bool ` + "`json:\"completed\"`" + `
}

var _ = common.API("todos-api",
	common.Spec("openapi", "catalog://api/default/todos", "1.0.0"),
	common.GET("/todos", common.Response[Todo]()),
)
var _ = common.Cache("todo-cache", common.KeyValue(common.Redis))
`,
			replacements: map[string]string{
				"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
			},
			golden: "testdata/golden/common-profile.golden.yaml",
		},
		{
			name: "env-transitive-profile",
			source: `package main

import (
	common "github.com/runtimeconditions/extensions/common-integrations/go"
	env "github.com/runtimeconditions/extensions/env-configuration/go"
)

var _ = common.Cache("todo-cache",
	common.KeyValue(common.Redis),
	env.EnvAlternative(env.Env("url", "REDIS_URL", env.Sensitive())),
	env.EnvAlternative(
		env.Env("hostname", "REDIS_HOST"),
		env.Env("port", "REDIS_PORT", env.Optional()),
	),
)
`,
			replacements: map[string]string{
				"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
				"github.com/runtimeconditions/extensions/env-configuration/go":   envPath,
			},
			golden: "testdata/golden/env-transitive-profile.golden.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeModule(t, dir, test.replacements)
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(test.source), 0o644); err != nil {
				t.Fatal(err)
			}

			profile, err := ExtractDir(dir, Options{
				Name:            test.name,
				WorkloadURI:     "github.com/example/" + test.name,
				WorkloadVersion: "v0.1.0",
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := yaml.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(test.golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != string(want) {
				t.Fatalf("profile golden mismatch\n--- got ---\n%s\n--- want ---\n%s", data, want)
			}
		})
	}
}

func TestExtractDirRejectsInvalidDeclarativeUse(t *testing.T) {
	dir := t.TempDir()
	commonPath := extensionModulePath(t, "common-integrations")
	writeModule(t, dir, map[string]string{
		"github.com/runtimeconditions/extensions/common-integrations/go": commonPath,
	})

	source := `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

var _ = common.API(42)
`

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ExtractDir(dir, Options{
		Name:            "invalid-declaration",
		WorkloadURI:     "github.com/example/invalid-declaration",
		WorkloadVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected invalid declarative use to fail")
	}
	if !strings.Contains(err.Error(), "API name must be a string literal or string const") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type extensionCatalogForTest map[string]extensionDefinitionForTest

type resolvedExtensionDefinitionsForTest struct {
	definitions map[string]extensionDefinitionForTest
}

type extensionDefinitionForTest struct {
	Path       string `yaml:"-"`
	Kind       string `yaml:"kind"`
	APIVersion string `yaml:"apiVersion"`
	Metadata   struct {
		ID string `yaml:"id"`
	} `yaml:"metadata"`
	Spec extensionSpecForTest `yaml:"spec"`
}

type extensionSpecForTest struct {
	Dependencies    []string                         `yaml:"dependencies"`
	Kinds           []extensionKindForTest           `yaml:"kinds"`
	InterfaceTypes  []extensionInterfaceTypeForTest  `yaml:"interfaceTypes"`
	ConditionFields []extensionConditionFieldForTest `yaml:"conditionFields"`
	InterfaceFields []extensionInterfaceFieldForTest `yaml:"interfaceFields"`
	FieldValues     []extensionFieldValueForTest     `yaml:"fieldValues"`
}

type extensionKindForTest struct {
	Name string `yaml:"name"`
}

type extensionInterfaceTypeForTest struct {
	Name       string `yaml:"name"`
	TargetKind string `yaml:"targetKind"`
}

type extensionConditionFieldForTest struct {
	Name                    string   `yaml:"name"`
	AppliesToKinds          []string `yaml:"appliesToKinds"`
	AppliesToInterfaceTypes []string `yaml:"appliesToInterfaceTypes"`
}

type extensionInterfaceFieldForTest struct {
	Name       string `yaml:"name"`
	TargetKind string `yaml:"targetKind"`
	TargetType string `yaml:"targetType"`
}

type extensionFieldValueForTest struct {
	Field      string   `yaml:"field"`
	TargetKind string   `yaml:"targetKind"`
	TargetType string   `yaml:"targetType"`
	Values     []string `yaml:"values"`
}

func loadExtensionCatalogForTest(t *testing.T) extensionCatalogForTest {
	t.Helper()
	root := workspacePath(t, "extensions")
	catalog := make(extensionCatalogForTest)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var probe struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal(data, &probe); err != nil {
			return err
		}
		if probe.Kind != "RuntimeConditionsExtensionDefinition" {
			return nil
		}
		var definition extensionDefinitionForTest
		if err := yaml.Unmarshal(data, &definition); err != nil {
			return err
		}
		definition.Path = path
		id := definition.id()
		if id == "" {
			return nil
		}
		if existing, ok := catalog[id]; ok {
			t.Fatalf("duplicate extension definition for %s:\n%s\n%s", id, existing.Path, path)
		}
		catalog[id] = definition
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func (d extensionDefinitionForTest) id() string {
	return d.Metadata.ID
}

func resolveExtensionDefinitionsForTest(t *testing.T, ids []string, catalog extensionCatalogForTest) resolvedExtensionDefinitionsForTest {
	t.Helper()
	resolved := resolvedExtensionDefinitionsForTest{definitions: make(map[string]extensionDefinitionForTest)}
	var visit func(string)
	visit = func(id string) {
		if _, ok := resolved.definitions[id]; ok {
			return
		}
		definition, ok := catalog[id]
		if !ok {
			t.Fatalf("missing extension definition for %s", id)
		}
		resolved.definitions[id] = definition
		for _, dependency := range definition.Spec.Dependencies {
			visit(dependency)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	return resolved
}

func (r resolvedExtensionDefinitionsForTest) ids() []string {
	ids := make([]string, 0, len(r.definitions))
	for id := range r.definitions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func validateProfileVocabularyForTest(t *testing.T, profile RuntimeConditionsProfile, resolved resolvedExtensionDefinitionsForTest) {
	t.Helper()
	for _, condition := range profile.Conditions {
		requireExactlyOneForTest(t, resolved.kindCount(condition.Kind), "kind %q", condition.Kind)
		requireExactlyOneForTest(t, resolved.interfaceTypeCount(condition.Kind, condition.Interface.Type), "interface.type %q for kind %q", condition.Interface.Type, condition.Kind)

		if condition.Interface.Spec != nil {
			requireExactlyOneForTest(t, resolved.interfaceFieldCount("spec", condition.Kind, condition.Interface.Type), "interface field spec for %s/%s", condition.Kind, condition.Interface.Type)
			requireExactlyOneForTest(t, resolved.fieldValueCount("interface.spec.format", condition.Kind, condition.Interface.Type, condition.Interface.Spec.Format), "interface.spec.format value %q for %s/%s", condition.Interface.Spec.Format, condition.Kind, condition.Interface.Type)
		}
		if len(condition.Interface.Operations) > 0 {
			requireExactlyOneForTest(t, resolved.interfaceFieldCount("operations", condition.Kind, condition.Interface.Type), "interface field operations for %s/%s", condition.Kind, condition.Interface.Type)
			for _, operation := range condition.Interface.Operations {
				requireExactlyOneForTest(t, resolved.fieldValueCount("interface.operations[].method", condition.Kind, condition.Interface.Type, operation.Method), "operation method %q for %s/%s", operation.Method, condition.Kind, condition.Interface.Type)
			}
		}
		if condition.Interface.Engine != "" {
			requireExactlyOneForTest(t, resolved.interfaceFieldCount("engine", condition.Kind, condition.Interface.Type), "interface field engine for %s/%s", condition.Kind, condition.Interface.Type)
			requireExactlyOneForTest(t, resolved.fieldValueCount("interface.engine", condition.Kind, condition.Interface.Type, condition.Interface.Engine), "interface.engine value %q for %s/%s", condition.Interface.Engine, condition.Kind, condition.Interface.Type)
		}
		if condition.Interface.BucketClass != "" {
			requireExactlyOneForTest(t, resolved.interfaceFieldCount("bucketClass", condition.Kind, condition.Interface.Type), "interface field bucketClass for %s/%s", condition.Kind, condition.Interface.Type)
			requireExactlyOneForTest(t, resolved.fieldValueCount("interface.bucketClass", condition.Kind, condition.Interface.Type, condition.Interface.BucketClass), "interface.bucketClass value %q for %s/%s", condition.Interface.BucketClass, condition.Kind, condition.Interface.Type)
		}
		if condition.Configuration != nil {
			requireExactlyOneForTest(t, resolved.conditionFieldCount("configuration", condition.Kind, condition.Interface.Type), "condition field configuration for %s/%s", condition.Kind, condition.Interface.Type)
			for _, env := range condition.Configuration.Env {
				requireExactlyOneForTest(t, resolved.fieldValueCount("configuration.env[].property", condition.Kind, condition.Interface.Type, env.Property), "configuration.env property %q for %s/%s", env.Property, condition.Kind, condition.Interface.Type)
			}
			for _, alternative := range condition.Configuration.Alternatives {
				for _, env := range alternative.Env {
					requireExactlyOneForTest(t, resolved.fieldValueCount("configuration.alternatives[].env[].property", condition.Kind, condition.Interface.Type, env.Property), "configuration alternative property %q for %s/%s", env.Property, condition.Kind, condition.Interface.Type)
				}
			}
		}
	}
}

func validateGoBindingVocabularyForTest(t *testing.T, binding *binding.Binding, resolved resolvedExtensionDefinitionsForTest) {
	t.Helper()
	if _, ok := resolved.definitions[binding.ExtensionID]; !ok {
		t.Fatalf("binding extension %s is not in the resolved extension set %#v", binding.ExtensionID, resolved.ids())
	}
	for _, declaration := range binding.Declarations {
		requireExactlyOneForTest(t, resolved.kindCount(declaration.Kind), "binding declaration kind %q", declaration.Kind)
		if declaration.InterfaceType != "" {
			requireExactlyOneForTest(t, resolved.interfaceTypeCount(declaration.Kind, declaration.InterfaceType), "binding declaration interface.type %q for kind %q", declaration.InterfaceType, declaration.Kind)
		}
		for _, option := range declaration.Options {
			validateDeclarationOptionVocabularyForTest(t, resolved, declaration.Kind, declaration.InterfaceType, option)
		}
	}
	for _, option := range binding.Options {
		validateStandaloneOptionVocabularyForTest(t, resolved, option)
	}
}

func validateDeclarationOptionVocabularyForTest(t *testing.T, resolved resolvedExtensionDefinitionsForTest, kind string, interfaceType string, option binding.Option) {
	t.Helper()
	switch option.Target {
	case "interface.spec":
		requireExactlyOneForTest(t, resolved.interfaceFieldCount("spec", kind, interfaceType), "binding option interface.spec for %s/%s", kind, interfaceType)
	case "interface.operations[]":
		requireExactlyOneForTest(t, resolved.interfaceFieldCount("operations", kind, interfaceType), "binding option interface.operations[] for %s/%s", kind, interfaceType)
		requireExactlyOneForTest(t, resolved.fieldValueCount("interface.operations[].method", kind, interfaceType, option.Method), "binding option method %q for %s/%s", option.Method, kind, interfaceType)
	case "interface.type":
		requireExactlyOneForTest(t, resolved.interfaceTypeCount(kind, option.Value), "binding option interface.type %q for kind %q", option.Value, kind)
		if option.EngineArg != nil {
			requireExactlyOneForTest(t, resolved.interfaceFieldCount("engine", kind, option.Value), "binding option engine for %s/%s", kind, option.Value)
		}
	case "requestBodySchema", "responseSchema":
	case "":
		t.Fatalf("binding option is missing target: %#v", option)
	default:
		if slices.Contains([]string{"configuration.env[]", "configuration.alternatives[]"}, option.Target) {
			requireSomeForTest(t, resolved.anyConditionFieldCount("configuration"), "binding option %s requires a configuration condition field", option.Target)
			return
		}
		t.Fatalf("unsupported binding option target in vocabulary test: %s", option.Target)
	}
	for _, nested := range option.Options {
		validateDeclarationOptionVocabularyForTest(t, resolved, kind, interfaceType, nested)
	}
}

func validateStandaloneOptionVocabularyForTest(t *testing.T, resolved resolvedExtensionDefinitionsForTest, option binding.Option) {
	t.Helper()
	for _, kind := range option.AppliesToKinds {
		requireExactlyOneForTest(t, resolved.kindCount(kind), "standalone option appliesToKind %q", kind)
	}
	for _, interfaceType := range option.AppliesToInterfaceTypes {
		requireSomeForTest(t, resolved.anyInterfaceTypeCount(interfaceType), "standalone option appliesToInterfaceType %q", interfaceType)
	}
	if slices.Contains([]string{"configuration.env[]", "configuration.alternatives[]"}, option.Target) {
		requireSomeForTest(t, resolved.anyConditionFieldCount("configuration"), "standalone option %s requires a configuration condition field", option.Target)
	}
	for _, nested := range option.Options {
		validateStandaloneOptionVocabularyForTest(t, resolved, nested)
	}
}

func requireExactlyOneForTest(t *testing.T, count int, format string, args ...any) {
	t.Helper()
	if count != 1 {
		t.Fatalf(format+": expected exactly one definition, got %d", append(args, count)...)
	}
}

func requireSomeForTest(t *testing.T, count int, format string, args ...any) {
	t.Helper()
	if count == 0 {
		t.Fatalf(format+": expected at least one definition", args...)
	}
}

func (r resolvedExtensionDefinitionsForTest) kindCount(name string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, kind := range definition.Spec.Kinds {
			if kind.Name == name {
				count++
			}
		}
	}
	return count
}

func (r resolvedExtensionDefinitionsForTest) interfaceTypeCount(kind string, name string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, interfaceType := range definition.Spec.InterfaceTypes {
			if interfaceType.TargetKind == kind && interfaceType.Name == name {
				count++
			}
		}
	}
	return count
}

func (r resolvedExtensionDefinitionsForTest) anyInterfaceTypeCount(name string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, interfaceType := range definition.Spec.InterfaceTypes {
			if interfaceType.Name == name {
				count++
			}
		}
	}
	return count
}

func (r resolvedExtensionDefinitionsForTest) conditionFieldCount(name string, kind string, interfaceType string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, field := range definition.Spec.ConditionFields {
			if field.Name == name && conditionFieldAppliesForTest(field, kind, interfaceType) {
				count++
			}
		}
	}
	return count
}

func (r resolvedExtensionDefinitionsForTest) anyConditionFieldCount(name string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, field := range definition.Spec.ConditionFields {
			if field.Name == name {
				count++
			}
		}
	}
	return count
}

func conditionFieldAppliesForTest(field extensionConditionFieldForTest, kind string, interfaceType string) bool {
	if !slices.Contains(field.AppliesToKinds, kind) {
		return false
	}
	return len(field.AppliesToInterfaceTypes) == 0 || slices.Contains(field.AppliesToInterfaceTypes, interfaceType)
}

func (r resolvedExtensionDefinitionsForTest) interfaceFieldCount(name string, kind string, interfaceType string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, field := range definition.Spec.InterfaceFields {
			if field.Name == name && field.TargetKind == kind && field.TargetType == interfaceType {
				count++
			}
		}
	}
	return count
}

func (r resolvedExtensionDefinitionsForTest) fieldValueCount(field string, kind string, interfaceType string, value string) int {
	count := 0
	for _, definition := range r.definitions {
		for _, fieldValue := range definition.Spec.FieldValues {
			if fieldValue.Field == field && fieldValue.TargetKind == kind && fieldValue.TargetType == interfaceType && slices.Contains(fieldValue.Values, value) {
				count++
			}
		}
	}
	return count
}

func writePackageManifestSDK(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "eventstream")
	packageDir := filepath.Join(root, "service", "events")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(root, "go.mod"): `module github.com/example/eventstream

go 1.25.0
`,
		filepath.Join(packageDir, "client.go"): `package events

import "context"

type Config struct{}
type Event struct{}
type Client struct{}

func NewClient(cfg Config) *Client {
	return &Client{}
}

func (c *Client) Publish(ctx context.Context, event Event) error {
	return nil
}
`,
		filepath.Join(packageDir, "runtimeconditions.extension.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/event-sink/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: example.event_sink
  interfaceTypes:
    - name: event.stream
      targetKind: example.event_sink
  conditionFields:
    - name: configuration
      appliesToKinds:
        - example.event_sink
      appliesToInterfaceTypes:
        - event.stream
  fieldValues:
    - field: configuration.env[].property
      targetKind: example.event_sink
      targetType: event.stream
      values:
        - endpoint
        - token
`,
		filepath.Join(packageDir, "runtimeconditions.package.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsPackage

metadata:
  package: github.com/example/eventstream/service/events
  language: go

extension:
  id: https://example.com/runtimeconditions/event-sink/v1alpha1/runtimeconditions.extension.yaml

go:
  importPath: github.com/example/eventstream/service/events
  package: events

  constructors:
    - function: NewClient
      receiver: Client

  declarations:
    - receiver: Client
      method: Publish
      name: audit-events
      kind: example.event_sink
      interfaceType: event.stream
      configuration:
        env:
          - property: endpoint
            name: EVENTSTREAM_URL
          - property: token
            name: EVENTSTREAM_TOKEN
            sensitive: true
`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeFilesForTest(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func extensionModulePath(t *testing.T, name string) string {
	t.Helper()
	return workspacePath(t, "extensions", name, "go")
}

func workspacePath(t *testing.T, parts ...string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate extractor test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	path := filepath.Join(append([]string{root}, parts...)...)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("workspace integration dependency is not checked out at %s", path)
	} else if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeModule(t *testing.T, dir string, replacements map[string]string) {
	t.Helper()
	content := "module github.com/example/testapp\n\ngo 1.25.0\n\nrequire (\n"
	for module := range replacements {
		content += "\t" + module + " v0.0.0\n"
	}
	content += ")\n\n"
	for module, replacement := range replacements {
		content += "replace " + module + " => " + replacement + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolveExtensionsForTest(t *testing.T, roots []string, definitions map[string]string) []string {
	t.Helper()
	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, dependency := range extensionDependencies(t, definitions[id]) {
			visit(dependency)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	resolved := make([]string, 0, len(seen))
	for id := range seen {
		resolved = append(resolved, id)
	}
	slices.Sort(resolved)
	return resolved
}

func extensionDependencies(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Spec struct {
			Dependencies []string `yaml:"dependencies"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document.Spec.Dependencies
}
