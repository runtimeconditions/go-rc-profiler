package extensioncheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This validation test uses binding modules fetched by Go into a separate
// workload. Run it with the core schema linked into the test binary.
func TestInstalledConformanceSchemas(t *testing.T) {
	proxy := os.Getenv("RC_GO_BINDING_CONFORMANCE_PROXY")
	if proxy == "" {
		t.Skip("set RC_GO_BINDING_CONFORMANCE_PROXY to an absolute local Go proxy")
	}
	if installedCoreSchemaBase64 == "" {
		t.Fatal("installed conformance tests require the release core schema linker flag")
	}
	root := t.TempDir()
	moduleCache := filepath.Join(root, "module-cache")
	t.Setenv("GOMODCACHE", moduleCache)
	t.Setenv("GOCACHE", filepath.Join(root, "build-cache"))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOSUMDB", "off")
	t.Cleanup(func() {
		_ = filepath.Walk(moduleCache, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				if info.IsDir() {
					_ = os.Chmod(path, 0o755)
				} else {
					_ = os.Chmod(path, 0o644)
				}
			}
			return nil
		})
	})
	tests := []struct {
		name, module, owner string
		valid, invalid      map[string]any
	}{
		{"recursive", "recursive-reference", "https://runtimeconditions.io/conformance/recursive-reference:1.0.0",
			map[string]any{"kind": "tree", "interface": map[string]any{"type": "node"}, "root": map[string]any{"name": "root", "children": []any{map[string]any{"name": "child"}}}},
			map[string]any{"kind": "tree", "interface": map[string]any{"type": "node"}, "root": map[string]any{"name": "root", "children": []any{map[string]any{"children": []any{}}}}}},
		{"alternatives", "object-alternatives", "https://runtimeconditions.io/conformance/object-alternatives:1.0.0",
			map[string]any{"kind": "deployment", "interface": map[string]any{"type": "process"}, "configuration": map[string]any{"image": "example"}},
			map[string]any{"kind": "deployment", "interface": map[string]any{"type": "process"}, "configuration": map[string]any{"image": "example", "command": "run"}}},
		{"union", "heterogeneous-union", "https://runtimeconditions.io/conformance/heterogeneous-union:1.0.0",
			map[string]any{"kind": "selector", "interface": map[string]any{"type": "target"}, "target": map[string]any{"id": "object"}},
			map[string]any{"kind": "selector", "interface": map[string]any{"type": "target"}, "target": map[string]any{"other": "value"}}},
		{"collections", "collections-and-maps", "https://runtimeconditions.io/conformance/collections-and-maps:1.0.0",
			map[string]any{"kind": "collection", "interface": map[string]any{"type": "items"}, "tags": []any{"tag"}, "entries": []any{map[string]any{"key": "key", "value": int64(1)}}, "labels": map[string]any{"team": "example"}},
			map[string]any{"kind": "collection", "interface": map[string]any{"type": "items"}, "entries": []any{map[string]any{"key": "key", "value": int64(3)}}}},
		{"scoped", "scoped-domains-collisions", "https://runtimeconditions.io/conformance/scoped-domains-collisions:1.0.0",
			map[string]any{"kind": "service", "interface": map[string]any{"type": "http"}, "mode": "direct"},
			map[string]any{"kind": "service", "interface": map[string]any{"type": "http"}, "mode": "streaming"}},
		{"source-names", "source-name-preservation", "https://runtimeconditions.io/conformance/source-name-preservation:1.0.0",
			map[string]any{"kind": "class", "interface": map[string]any{"type": "http_server2_url"}, "café": "cafe"},
			map[string]any{"kind": "class", "interface": map[string]any{"type": "http_server2_url"}, "café": int64(1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workload := filepath.Join(root, test.name)
			if err := os.Mkdir(workload, 0o755); err != nil {
				t.Fatal(err)
			}
			path := "example.com/runtimeconditions/conformance/" + test.module
			if err := os.WriteFile(filepath.Join(workload, "go.mod"), []byte("module example.com/conformance-"+test.name+"\n\ngo 1.22\n\nrequire "+path+" v1.0.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workload, "main.go"), []byte("package main\nimport _ \""+path+"\"\nfunc main() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOPROXY", "file://"+proxy)
			download := exec.Command("go", "mod", "download", "all")
			download.Dir = workload
			if output, err := download.CombinedOutput(); err != nil {
				t.Fatalf("go mod download: %v\n%s", err, output)
			}
			t.Setenv("GOPROXY", "off")
			profile := func(condition map[string]any) map[string]any {
				return map[string]any{"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsProfile",
					"metadata": map[string]any{"name": "conformance"}, "workload": map[string]any{"uri": "urn:example:conformance"},
					"conditions": []any{condition}}
			}
			valid, err := FinalizeGeneratedProfile(t.Context(), workload, profile(test.valid), [][]string{{path}})
			if err != nil {
				t.Fatalf("valid profile: %v", err)
			}
			if got := valid["extensions"]; !reflect.DeepEqual(got, []any{test.owner}) {
				t.Fatalf("direct contributors: %v", got)
			}
			if rejected, err := FinalizeGeneratedProfile(t.Context(), workload, profile(test.invalid), [][]string{{path}}); rejected != nil || err == nil || !strings.Contains(err.Error(), "schema") && !strings.Contains(err.Error(), "domain") {
				t.Fatalf("invalid profile must fail before output: result %v, error %v", rejected, err)
			}
		})
	}
}

// A binding dependency can contribute a schema without contributing any
// vocabulary or being imported as a Go package by the workload. It must still
// resolve from the installed module graph and participate in validation.
func TestInstalledValidationOnlyDependency(t *testing.T) {
	proxy := os.Getenv("RC_GO_BINDING_CONFORMANCE_PROXY")
	if proxy == "" {
		t.Skip("set RC_GO_BINDING_CONFORMANCE_PROXY to an absolute local Go proxy")
	}
	if installedCoreSchemaBase64 == "" {
		t.Fatal("installed conformance tests require the release core schema linker flag")
	}
	root := t.TempDir()
	moduleCache := filepath.Join(root, "module-cache")
	t.Setenv("GOMODCACHE", moduleCache)
	t.Setenv("GOCACHE", filepath.Join(root, "build-cache"))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOSUMDB", "off")
	t.Cleanup(func() {
		_ = filepath.Walk(moduleCache, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				if info.IsDir() {
					_ = os.Chmod(path, 0o755)
				} else {
					_ = os.Chmod(path, 0o644)
				}
			}
			return nil
		})
	})
	const rootPath = "example.com/runtimeconditions/conformance/dependency-schema-only-root"
	const rootID = "https://runtimeconditions.io/conformance/dependency-schema-only-root:1.0.0"
	const dependencyID = "https://runtimeconditions.io/conformance/dependency-schema-only-dependency:1.0.0"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(
		"module example.com/validation-only-consumer\n\ngo 1.22\n\nrequire "+rootPath+" v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(
		"package main\nimport b \""+rootPath+"\"\nvar _ = b.Job(b.Process{}, b.Command(\"ok\"))\nfunc init() { panic(\"workload source must not execute\") }\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", "file://"+proxy)
	download := exec.Command("go", "mod", "download", "all")
	download.Dir = root
	if output, err := download.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, output)
	}
	t.Setenv("GOPROXY", "off")
	packages, err := ResolveImportedGoPackages(t.Context(), root, []string{rootPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || packages[0].ExtensionID != dependencyID || packages[1].ExtensionID != rootID {
		t.Fatalf("installed dependency closure: %+v", packages)
	}
	profile := func(command string) map[string]any {
		return map[string]any{
			"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsProfile",
			"metadata":   map[string]any{"name": "validation-only"},
			"workload":   map[string]any{"uri": "urn:example:validation-only"},
			"conditions": []any{map[string]any{"kind": "job", "interface": map[string]any{"type": "process"}, "command": command}},
		}
	}
	valid, err := FinalizeGeneratedProfile(t.Context(), root, profile("ok"), [][]string{{rootPath}})
	if err != nil {
		t.Fatal(err)
	}
	if got := valid["extensions"]; !reflect.DeepEqual(got, []any{rootID}) {
		t.Fatalf("noncontributing dependency emitted: %v", got)
	}
	if rejected, err := FinalizeGeneratedProfile(t.Context(), root, profile("too-long"), [][]string{{rootPath}}); rejected != nil || err == nil || !strings.Contains(err.Error(), dependencyID+"#schema:command-limit") {
		t.Fatalf("dependency schema must reject before output: result %v, error %v", rejected, err)
	}
}
