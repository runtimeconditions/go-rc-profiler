// Package extractor_test holds the refactoring regression suite. It is an
// external test package on purpose: it may only reach the exported profiler
// surface, so internal reorganization cannot quietly satisfy it.
//
// Most fixtures under testdata/regression are self-contained copies of artifacts
// the profiler is expected to keep compiling today: the first-party Go
// declaration packages and their extension definitions, the reviewed NATS SDK
// mapping and extension release, the rc-demos request logger workload, and the
// six reviewed NATS SDK workloads. Their golden profiles are the reviewed outputs
// those inputs produce now. Two fixtures, full-vocabulary and
// nats-repeated-operations, are written for this suite alone to reach behavior
// the reviewed artifacts never trigger; each says so in its own source.
package extractor_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runtimeconditions/go-rc-profiler/extractor"
	"gopkg.in/yaml.v3"
)

const (
	natsModulePath      = "github.com/nats-io/nats.go"
	natsModuleVersion   = "v1.53.1"
	natsMappingName     = "nats.go.service"
	natsMappingRelative = "runtimeconditions/mappings/nats-service.yaml"
	natsIndexRelative   = "runtimeconditions/index.yaml"
	natsExtensionID     = "https://runtimeconditions.io/extensions/nats-service/0.1.0/runtimeconditions.extension.yaml"
)

var natsWorkloads = []string{
	"complete-service",
	"core-messaging",
	"jetstream-consumer",
	"jetstream-publisher",
	"key-value",
	"object-store",
}

// TestDeclarationWorkloadProfileMatchesGolden locks the profile the rc-demos Go
// request logger produces from the first-party declaration packages.
func TestDeclarationWorkloadProfileMatchesGolden(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "request-logger-http"), extractor.Options{
		Name:              "request-logger-http",
		WorkloadURI:       "github.com/runtimeconditions/rc-demos/apps/request-logger-http",
		WorkloadVersion:   "dev",
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "request-logger-http.golden.yaml")
}

// TestDeclarationWorkloadProfileIsStableWithoutGoPackages proves the syntax-only
// path still reaches the same profile when semantic loading is disabled.
func TestDeclarationWorkloadProfileIsStableWithoutGoPackages(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "request-logger-http"), extractor.Options{
		Name:              "request-logger-http",
		WorkloadURI:       "github.com/runtimeconditions/rc-demos/apps/request-logger-http",
		WorkloadVersion:   "dev",
		DisableGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "request-logger-http.golden.yaml")
}

// TestFullDeclarationVocabularyProfileMatchesGolden exercises the whole
// first-party declarative vocabulary in one profile: every HTTP method, API
// specs with and without a version, both datastore interfaces, caches, sensitive
// and optional environment inputs, configuration alternatives, and the schema
// shapes the profiler derives from Go types.
func TestFullDeclarationVocabularyProfileMatchesGolden(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "full-vocabulary"), extractor.Options{
		Name:              "full-vocabulary",
		WorkloadURI:       "github.com/runtimeconditions/go-rc-profiler/regression/full-vocabulary",
		WorkloadVersion:   "v1.0.0",
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "full-vocabulary.golden.yaml")
}

// TestNATSSDKWorkloadProfilesMatchGolden runs every NATS SDK workload through the
// reviewed static mapping and compares against the reviewed profiles.
func TestNATSSDKWorkloadProfilesMatchGolden(t *testing.T) {
	isolateGoEnvironment(t)
	sdkDir := stageNATSSDK(t, nil)
	extensionsRoot := regressionPath(t, "extensions")

	for _, workload := range natsWorkloads {
		t.Run(workload, func(t *testing.T) {
			profile, err := extractor.ExtractDir(stageNATSWorkload(t, workload, sdkDir), extractor.Options{
				Name:              "nats-" + workload,
				WorkloadURI:       "https://github.com/runtimeconditions/sdk-authorship-discovery/tree/main/nats/go/" + workload,
				WorkloadVersion:   "0.1.0",
				ExtensionRoots:    []string{extensionsRoot},
				RequireGoPackages: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			requireGoldenProfile(t, profile, "nats-"+workload+".golden.yaml")
		})
	}
}

// TestSkipValidationEmitsTheSameProfile proves the escape hatch only skips the
// validation layers. It must still emit the profile, and the extension list it
// assembles itself must already be ordered, because extension closure resolution
// is not there to reorder it.
func TestSkipValidationEmitsTheSameProfile(t *testing.T) {
	isolateGoEnvironment(t)

	profile, err := extractor.ExtractDir(regressionPath(t, "workloads", "full-vocabulary"), extractor.Options{
		Name:              "full-vocabulary",
		WorkloadURI:       "github.com/runtimeconditions/go-rc-profiler/regression/full-vocabulary",
		WorkloadVersion:   "v1.0.0",
		SkipValidation:    true,
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "full-vocabulary.golden.yaml")
}

// TestNATSSDKRepeatedOperationsAreDeduplicated locks operation de-duplication
// within one source-proven dependency identity. The reviewed NATS fixtures never
// repeat an operation, so this workload exists only for the regression suite.
func TestNATSSDKRepeatedOperationsAreDeduplicated(t *testing.T) {
	isolateGoEnvironment(t)
	sdkDir := stageNATSSDK(t, nil)

	profile, err := extractor.ExtractDir(stageNATSWorkload(t, "repeated-operations", sdkDir), extractor.Options{
		Name:              "nats-repeated-operations",
		WorkloadURI:       "github.com/runtimeconditions/go-rc-profiler/regression/nats-repeated-operations",
		WorkloadVersion:   "v1.0.0",
		ExtensionRoots:    []string{regressionPath(t, "extensions")},
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireGoldenProfile(t, profile, "nats-repeated-operations.golden.yaml")
}

// TestNATSSDKMappingFailsClosed locks the trust boundary described in
// docs/sdk-mappings.md. Corrupt installed metadata for an imported SDK must stop
// profiling with a specific diagnosis rather than emit a silently narrowed
// profile.
func TestNATSSDKMappingFailsClosed(t *testing.T) {
	isolateGoEnvironment(t)

	tests := []struct {
		name          string
		corruptSDK    func(t *testing.T, sdkDir string)
		extensionsAbs func(t *testing.T) []string
		wantError     string
	}{
		{
			name: "mapping body no longer matches the index digest",
			corruptSDK: func(t *testing.T, sdkDir string) {
				rewriteFile(t, filepath.Join(sdkDir, natsMappingRelative), func(mapping string) string {
					return strings.Replace(mapping, "action: connect", "action: inspect", 1)
				})
			},
			wantError: "mapping SHA-256 does not match index",
		},
		{
			name: "mapping body no longer matches its own semantic digest",
			corruptSDK: func(t *testing.T, sdkDir string) {
				rewriteFile(t, filepath.Join(sdkDir, natsMappingRelative), func(mapping string) string {
					return strings.Replace(mapping, "action: connect", "action: inspect", 1)
				})
				writeNATSIndex(t, sdkDir, natsModuleVersion, natsMappingRelative)
			},
			wantError: "semantic SHA-256 does not match go mapping body",
		},
		{
			name: "index claims a module version the application did not select",
			corruptSDK: func(t *testing.T, sdkDir string) {
				writeNATSIndex(t, sdkDir, "v1.52.0", natsMappingRelative)
			},
			wantError: "index identity does not match resolved module " + natsModulePath + " " + natsModuleVersion,
		},
		{
			name: "index points at a mapping outside the module",
			corruptSDK: func(t *testing.T, sdkDir string) {
				writeNATSIndex(t, sdkDir, natsModuleVersion, "../nats-service.yaml")
			},
			wantError: `mapping path "../nats-service.yaml" escapes module root`,
		},
		{
			name: "index is not a Runtime Conditions SDK mapping index",
			corruptSDK: func(t *testing.T, sdkDir string) {
				rewriteFile(t, filepath.Join(sdkDir, natsIndexRelative), func(index string) string {
					return strings.Replace(index, "runtimeconditions.io/sdk-mapping/v1alpha1", "runtimeconditions.io/sdk-mapping/v1beta1", 1)
				})
			},
			wantError: "unsupported SDK mapping index",
		},
		{
			name: "mapping targets an extension that is not installed",
			extensionsAbs: func(t *testing.T) []string {
				return nil
			},
			wantError: "extension " + natsExtensionID + " is not available in the configured extension roots",
		},
		{
			name: "installed extension release does not match the mapping",
			extensionsAbs: func(t *testing.T) []string {
				extensionsRoot := stageExtensions(t)
				rewriteFile(t, filepath.Join(extensionsRoot, "nats-service", "runtimeconditions.extension.yaml"), func(definition string) string {
					return strings.Replace(definition, "version: 0.1.0", "version: 0.2.0", 1)
				})
				return []string{extensionsRoot}
			},
			wantError: "extension " + natsExtensionID + " version or semantic SHA-256 does not match installed definition",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sdkDir := stageNATSSDK(t, test.corruptSDK)
			extensionRoots := []string{regressionPath(t, "extensions")}
			if test.extensionsAbs != nil {
				extensionRoots = test.extensionsAbs(t)
			}

			_, err := extractor.ExtractDir(stageNATSWorkload(t, "core-messaging", sdkDir), extractor.Options{
				Name:              "nats-core-messaging",
				WorkloadURI:       "https://github.com/runtimeconditions/sdk-authorship-discovery/tree/main/nats/go/core-messaging",
				WorkloadVersion:   "0.1.0",
				ExtensionRoots:    extensionRoots,
				RequireGoPackages: true,
			})
			requireErrorContains(t, err, test.wantError)
		})
	}
}

// TestDeclarationWorkloadFailsClosed locks the declarative-path diagnostics that
// stop a profile from being emitted.
func TestDeclarationWorkloadFailsClosed(t *testing.T) {
	isolateGoEnvironment(t)

	tests := []struct {
		name      string
		source    string
		wantError string
	}{
		{
			name: "condition name is not a compile-time string",
			source: `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

func declaration(name string) {
	common.API(name)
}
`,
			wantError: "API name must be a string literal or string const",
		},
		{
			name: "operation schema type cannot be resolved",
			source: `package main

import common "github.com/runtimeconditions/extensions/common-integrations/go"

func declaration() {
	common.API("todos-api", common.GET("/todos", common.Response[chan int]()))
}
`,
			wantError: "unsupported schema type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workloadDir := stageDeclarationWorkload(t, test.source)

			_, err := extractor.ExtractDir(workloadDir, extractor.Options{
				Name:            "declaration-failure",
				WorkloadURI:     "github.com/example/declaration-failure",
				WorkloadVersion: "v0.1.0",
			})
			requireErrorContains(t, err, test.wantError)
		})
	}
}

// TestSDKMappingIsIgnoredForUnimportedModules proves that installed SDK metadata
// only participates when the application actually imports the module.
func TestSDKMappingIsIgnoredForUnimportedModules(t *testing.T) {
	isolateGoEnvironment(t)
	sdkDir := stageNATSSDK(t, func(t *testing.T, sdkDir string) {
		rewriteFile(t, filepath.Join(sdkDir, natsIndexRelative), func(index string) string {
			return strings.Replace(index, "runtimeconditions.io/sdk-mapping/v1alpha1", "runtimeconditions.io/sdk-mapping/v1beta1", 1)
		})
	})

	workloadDir := t.TempDir()
	writeFile(t, filepath.Join(workloadDir, "go.mod"), "module github.com/example/unimported\n\ngo 1.25.0\n\nrequire "+natsModulePath+" "+natsModuleVersion+"\n\nreplace "+natsModulePath+" => "+sdkDir+"\n")
	writeFile(t, filepath.Join(workloadDir, "main.go"), "package main\n\nfunc main() {}\n")

	profile, err := extractor.ExtractDir(workloadDir, extractor.Options{
		Name:            "unimported-sdk",
		WorkloadURI:     "github.com/example/unimported",
		WorkloadVersion: "v0.1.0",
		ExtensionRoots:  []string{regressionPath(t, "extensions")},
	})
	if err != nil {
		t.Fatalf("malformed metadata in an unimported module must not fail profiling: %v", err)
	}
	if len(profile.Conditions) != 0 || len(profile.Extensions) != 0 {
		t.Fatalf("expected an empty profile, got %#v", profile)
	}
}

// TestWorkloadWithoutRuntimeConditionsMetadataProfilesEmpty proves that ordinary
// imports without a declaration package or SDK mapping produce a structurally
// valid profile with no conditions instead of a failure.
func TestWorkloadWithoutRuntimeConditionsMetadataProfilesEmpty(t *testing.T) {
	isolateGoEnvironment(t)
	sdkDir := stageNATSSDK(t, func(t *testing.T, sdkDir string) {
		if err := os.RemoveAll(filepath.Join(sdkDir, "runtimeconditions")); err != nil {
			t.Fatal(err)
		}
	})

	profile, err := extractor.ExtractDir(stageNATSWorkload(t, "core-messaging", sdkDir), extractor.Options{
		Name:              "unmapped-sdk",
		WorkloadURI:       "github.com/example/unmapped",
		WorkloadVersion:   "v0.1.0",
		ExtensionRoots:    []string{regressionPath(t, "extensions")},
		RequireGoPackages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Conditions) != 0 || len(profile.Extensions) != 0 {
		t.Fatalf("expected an empty profile, got %#v", profile)
	}
}

func regressionPath(t *testing.T, parts ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(append([]string{"testdata", "regression"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func requireGoldenProfile(t *testing.T, profile *extractor.RuntimeConditionsProfile, golden string) {
	t.Helper()
	got, err := yaml.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(regressionPath(t, "golden", golden))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("profile differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected an error containing %q, got: %v", want, err)
	}
}

// isolateGoEnvironment keeps extraction independent of the developer's Go
// workspace, which otherwise reaches the go/packages load and the `go list -m`
// call the SDK mapping discovery performs.
func isolateGoEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
}

// stageNATSSDK resolves the reviewed NATS module release, copies it somewhere
// writable, and installs the reviewed mapping the way SDK release automation
// would. corrupt, when set, damages the installed metadata afterwards.
func stageNATSSDK(t *testing.T, corrupt func(t *testing.T, sdkDir string)) string {
	t.Helper()
	sdkDir := filepath.Join(t.TempDir(), "nats.go")
	copyTree(t, downloadedModuleDir(t), sdkDir)
	writeFile(t, filepath.Join(sdkDir, natsMappingRelative), readFile(t, regressionPath(t, "sdk-mappings", "nats-service.yaml")))
	writeNATSIndex(t, sdkDir, natsModuleVersion, natsMappingRelative)
	if corrupt != nil {
		corrupt(t, sdkDir)
	}
	return sdkDir
}

// downloadedModuleDir asks the Go toolchain for the extracted source of the
// reviewed NATS release, resolving it from the module cache when it is present.
func downloadedModuleDir(t *testing.T) string {
	t.Helper()
	command := exec.Command("go", "mod", "download", "-json", natsModulePath)
	command.Dir = regressionPath(t, "workloads", "nats-core-messaging")
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Skipf("%s %s is unavailable: %s", natsModulePath, natsModuleVersion, strings.TrimSpace(string(exit.Stderr)))
		}
		t.Skipf("%s %s is unavailable: %v", natsModulePath, natsModuleVersion, err)
	}
	var download struct {
		Version string
		Dir     string
	}
	if err := json.Unmarshal(output, &download); err != nil {
		t.Fatal(err)
	}
	if download.Version != natsModuleVersion || download.Dir == "" {
		t.Skipf("%s %s was not extracted: %s", natsModulePath, natsModuleVersion, output)
	}
	return download.Dir
}

// writeNATSIndex installs the module index that points at the mapping, matching
// the layout SDK release automation produces.
func writeNATSIndex(t *testing.T, sdkDir string, moduleVersion string, mappingRelative string) {
	t.Helper()
	digest := sha256.Sum256([]byte(readFile(t, filepath.Join(sdkDir, natsMappingRelative))))
	index := map[string]any{
		"apiVersion": "runtimeconditions.io/sdk-mapping/v1alpha1",
		"kind":       "RuntimeConditionsSDKMappingIndex",
		"metadata": map[string]any{
			"module":        natsModulePath,
			"moduleVersion": moduleVersion,
			"language":      "go",
		},
		"mappings": []any{map[string]any{
			"name":   natsMappingName,
			"path":   mappingRelative,
			"sha256": hex.EncodeToString(digest[:]),
		}},
	}
	data, err := yaml.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sdkDir, natsIndexRelative), string(data))
}

// stageNATSWorkload copies a reviewed NATS workload and points it at the staged
// SDK so extraction reads the installed metadata.
func stageNATSWorkload(t *testing.T, workload string, sdkDir string) string {
	t.Helper()
	workloadDir := filepath.Join(t.TempDir(), workload)
	copyTree(t, regressionPath(t, "workloads", "nats-"+workload), workloadDir)
	goModPath := filepath.Join(workloadDir, "go.mod")
	writeFile(t, goModPath, readFile(t, goModPath)+"\nreplace "+natsModulePath+" => "+sdkDir+"\n")
	return workloadDir
}

// stageDeclarationWorkload builds a module that consumes the vendored first-party
// declaration packages.
func stageDeclarationWorkload(t *testing.T, source string) string {
	t.Helper()
	workloadDir := t.TempDir()
	extensionsRoot := regressionPath(t, "extensions")
	writeFile(t, filepath.Join(workloadDir, "go.mod"), "module github.com/example/declaration\n\ngo 1.25.0\n\n"+
		"require github.com/runtimeconditions/extensions/common-integrations/go v0.0.0\n\n"+
		"replace github.com/runtimeconditions/extensions/common-integrations/go => "+filepath.Join(extensionsRoot, "common-integrations", "go")+"\n")
	writeFile(t, filepath.Join(workloadDir, "main.go"), source)
	return workloadDir
}

// stageExtensions copies the vendored extension catalog so a test can damage an
// installed definition without touching the checked-in fixture.
func stageExtensions(t *testing.T) string {
	t.Helper()
	extensionsRoot := filepath.Join(t.TempDir(), "extensions")
	copyTree(t, regressionPath(t, "extensions"), extensionsRoot)
	return extensionsRoot
}

func copyTree(t *testing.T, source string, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func rewriteFile(t *testing.T, path string, rewrite func(string) string) {
	t.Helper()
	writeFile(t, path, rewrite(readFile(t, path)))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
