package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This test uses the separately assembled, installable fixture modules. The
// profiler receives a workload and Go's module cache, never fixture source.
func TestInstalledGeneratedBindings(t *testing.T) {
	proxy := os.Getenv("RC_GO_BINDING_FIXTURE_PROXY")
	conformanceProxy := os.Getenv("RC_GO_BINDING_CONFORMANCE_PROXY")
	if conformanceProxy != "" {
		proxy = conformanceProxy
	}
	if proxy == "" || os.Getenv("RC_GO_PROFILER_BIN") == "" {
		t.Skip("set RC_GO_BINDING_FIXTURE_PROXY and RC_GO_PROFILER_BIN to run installed-artifact tests")
	}
	root := t.TempDir()
	moduleCache := filepath.Join(root, "module-cache")
	buildCache := filepath.Join(root, "build-cache")
	t.Cleanup(func() {
		// Go makes extracted module files read-only; restore permissions so
		// testing.TempDir can remove this isolated cache.
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
	const prefix = "example.com/runtimeconditions/conformance/"
	tests := []struct {
		name, requires, imports, call, condition, extensions string
	}{
		{
			name: "owned", requires: prefix + "owned-kind-interface v1.0.0",
			imports:    `binding "` + prefix + `owned-kind-interface"`,
			call:       `binding.Service(binding.Http{Endpoint: "https://example.com"}, binding.Region("eu"))`,
			condition:  "    - interface:\n        endpoint: https://example.com\n        type: http\n      kind: service\n      region: eu\n",
			extensions: "    - urn:runtimeconditions:conformance:owned-kind-interface\n",
		},
		{
			name: "additive", requires: prefix + "additive-field-base v1.0.0\n" + prefix + "additive-field v1.0.0\n" + prefix + "owned-kind-interface v1.0.0",
			imports:    `base "` + prefix + `additive-field-base"` + "\n" + `extra "` + prefix + `additive-field"` + "\n" + `_ "` + prefix + `owned-kind-interface"`,
			call:       `base.Service(base.Http{}, extra.Credential{Token: "fixture-token"})`,
			condition:  "    - credential:\n        token: fixture-token\n      interface:\n        type: http\n      kind: service\n",
			extensions: "    - urn:runtimeconditions:conformance:additive-field\n    - urn:runtimeconditions:conformance:additive-field:base\n",
		},
		{
			name: "transitive", requires: prefix + "transitive-leaf v1.0.0\n" + prefix + "transitive-middle v1.0.0\n" + prefix + "transitive-root v1.0.0",
			imports:    `leaf "` + prefix + `transitive-leaf"` + "\n" + `middle "` + prefix + `transitive-middle"` + "\n" + `root "` + prefix + `transitive-root"`,
			call:       `leaf.Worker(middle.Process{}, root.Command("run"))`,
			condition:  "    - command: run\n      interface:\n        type: process\n      kind: worker\n",
			extensions: "    - urn:runtimeconditions:conformance:transitive:leaf\n    - urn:runtimeconditions:conformance:transitive:middle\n    - urn:runtimeconditions:conformance:transitive:root\n",
		},
	}
	if conformanceProxy != "" {
		tests = append(tests,
			struct{ name, requires, imports, call, condition, extensions string }{
				name: "recursive", requires: prefix + "recursive-reference v1.0.0",
				imports:    `binding "` + prefix + `recursive-reference"`,
				call:       `binding.Tree(binding.TreeNode{}, binding.Root{Name: "root", Children: binding.RootChildren{binding.NodeNode{Name: "child"}}})`,
				condition:  "    - interface:\n        type: node\n      kind: tree\n      root:\n        children:\n            - name: child\n        name: root\n",
				extensions: "    - urn:runtimeconditions:conformance:recursive-reference\n",
			},
			struct{ name, requires, imports, call, condition, extensions string }{
				name: "alternatives", requires: prefix + "object-alternatives v1.0.0",
				imports:    `binding "` + prefix + `object-alternatives"`,
				call:       `binding.Deployment(binding.Process{}, binding.Configuration{Image: &[]string{"image"}[0]})` + "\nvar _ = " + `binding.Deployment(binding.Process{}, binding.Configuration{Command: &[]string{"run"}[0]})`,
				condition:  "    - configuration:\n        image: image\n      interface:\n        type: process\n      kind: deployment\n    - configuration:\n        command: run\n      interface:\n        type: process\n      kind: deployment\n",
				extensions: "    - urn:runtimeconditions:conformance:object-alternatives\n",
			},
			struct{ name, requires, imports, call, condition, extensions string }{
				name: "union", requires: prefix + "heterogeneous-union v1.0.0",
				imports:    `binding "` + prefix + `heterogeneous-union"`,
				call:       `binding.Selector(binding.SelectorTarget{}, binding.TargetString("target"))` + "\nvar _ = " + `binding.Selector(binding.SelectorTarget{}, binding.TargetObject{Id: "object"})`,
				condition:  "    - interface:\n        type: target\n      kind: selector\n      target: target\n    - interface:\n        type: target\n      kind: selector\n      target:\n        id: object\n",
				extensions: "    - urn:runtimeconditions:conformance:heterogeneous-union\n",
			},
			struct{ name, requires, imports, call, condition, extensions string }{
				name: "collections", requires: prefix + "collections-and-maps v1.0.0",
				imports:    `binding "` + prefix + `collections-and-maps"`,
				call:       `binding.Collection(binding.Items{}, binding.Tags{"tag"}, binding.Entries{binding.EntriesItem{Key: "key", Value: 1}}, binding.Labels{"team": "example"})`,
				condition:  "    - entries:\n        - key: key\n          value: 1\n      interface:\n        type: items\n      kind: collection\n      labels:\n        team: example\n      tags:\n        - tag\n",
				extensions: "    - urn:runtimeconditions:conformance:collections-and-maps\n",
			},
			struct{ name, requires, imports, call, condition, extensions string }{
				name: "scoped", requires: prefix + "scoped-domains-collisions v1.0.0",
				imports:    `binding "` + prefix + `scoped-domains-collisions"`,
				call:       `binding.Service(binding.Http{}, binding.HttpModeDirect, binding.BaseUrl("https://example.com"))` + "\nvar _ = " + `binding.Service(binding.Grpc{}, binding.GrpcModeStreaming)`,
				condition:  "    - base_url: https://example.com\n      interface:\n        type: http\n      kind: service\n      mode: direct\n    - interface:\n        type: grpc\n      kind: service\n      mode: streaming\n",
				extensions: "    - urn:runtimeconditions:conformance:scoped-domains-collisions\n",
			},
			struct{ name, requires, imports, call, condition, extensions string }{
				name: "source-names", requires: prefix + "source-name-preservation v1.0.0",
				imports:    `binding "` + prefix + `source-name-preservation"`,
				call:       `binding.Class(binding.HttpServer2Url{}, binding.ApiUrl("https://example.com/api"), binding.Café("cafe"), binding.Patch9("v9"))`,
				condition:  "    - api_url: https://example.com/api\n      café: cafe\n      interface:\n        type: http_server2_url\n      kind: class\n      patch9: v9\n",
				extensions: "    - urn:runtimeconditions:conformance:source-name-preservation\n",
			},
		)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workload := filepath.Join(root, test.name)
			if err := os.Mkdir(workload, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFixtureFile(t, filepath.Join(workload, "go.mod"), "module example.com/"+test.name+"\n\ngo 1.22\n\nrequire (\n"+test.requires+"\n)\n")
			writeFixtureFile(t, filepath.Join(workload, "main.go"), "package main\nimport (\n"+test.imports+"\n)\nvar _ = "+test.call+"\nfunc init() { panic(\"application code executed\") }\nfunc main() {}\n")
			env := append(os.Environ(), "GOWORK=off", "GOSUMDB=off", "GOCACHE="+buildCache, "GOMODCACHE="+moduleCache)
			download := exec.Command("go", "mod", "download", "all")
			download.Dir, download.Env = workload, append(env, "GOPROXY=file://"+proxy)
			if output, err := download.CombinedOutput(); err != nil {
				t.Fatalf("go mod download: %v\n%s", err, output)
			}
			binary := exec.Command(profilerBinary, "generate", "-dir", workload, "-name", "fixture", "-workload-uri", "urn:example:"+test.name, "-workload-version", "1.0.0")
			binary.Env = append(env, "GOPROXY=off")
			actual, err := binary.CombinedOutput()
			if err != nil {
				t.Fatalf("installed profiler: %v\n%s", err, actual)
			}
			want := "apiVersion: runtimeconditions.io/v1alpha1\nconditions:\n" + test.condition + "extensions:\n" + test.extensions + "kind: RuntimeConditionsProfile\nmetadata:\n    name: fixture\nworkload:\n    uri: urn:example:" + test.name + "\n    version: 1.0.0\n"
			if string(actual) != want {
				t.Fatalf("profile mismatch\nwant:\n%s\ngot:\n%s", want, actual)
			}
			repeat := exec.Command(profilerBinary, "generate", "-dir", workload, "-name", "fixture", "-workload-uri", "urn:example:"+test.name, "-workload-version", "1.0.0")
			repeat.Env = append(env, "GOPROXY=off")
			again, err := repeat.CombinedOutput()
			if err != nil || string(again) != string(actual) {
				t.Fatalf("repeated generation changed output: %v\n%s", err, again)
			}
			if test.name == "owned" {
				writeFixtureFile(t, filepath.Join(workload, "main.go"), "package main\nimport (\n"+test.imports+"\n)\nfunc declare(region string) { _ = binding.Service(binding.Http{Endpoint: \"https://example.com\"}, binding.Region(region)) }\nfunc main() {}\n")
				out := filepath.Join(workload, "rejected.yaml")
				negative := exec.Command(profilerBinary, "generate", "-dir", workload, "-out", out)
				negative.Env = append(env, "GOPROXY=off")
				output, err := negative.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "not a compile-time constant") {
					t.Fatalf("dynamic declaration should fail: %v\n%s", err, output)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("rejected declaration wrote a profile: %v", err)
				}
				writeFixtureFile(t, filepath.Join(workload, "main.go"), "package main\nimport (\n"+test.imports+"\n)\nvar declare = binding.Service\nvar _ = declare(binding.Http{Endpoint: \"https://example.com\"}, binding.Region(\"eu\"))\nfunc main() {}\n")
				alias := exec.Command(profilerBinary, "generate", "-dir", workload, "-out", out)
				alias.Env = append(env, "GOPROXY=off")
				output, err = alias.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "must be called directly") {
					t.Fatalf("aliased declaration should fail: %v\n%s", err, output)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("rejected alias wrote a profile: %v", err)
				}
			}
			if test.name == "scoped" {
				writeFixtureFile(t, filepath.Join(workload, "main.go"), "package main\nimport (\n"+test.imports+"\n)\nvar _ = binding.Service(binding.Http{}, binding.HttpMode(\"streaming\"))\nfunc main() {}\n")
				out := filepath.Join(workload, "rejected.yaml")
				negative := exec.Command(profilerBinary, "generate", "-dir", workload, "-out", out)
				negative.Env = append(env, "GOPROXY=off")
				output, err := negative.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "outside domain") {
					t.Fatalf("wrong scoped value should fail: %v\n%s", err, output)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("rejected scoped value wrote a profile: %v", err)
				}
			}
			if test.name == "alternatives" || test.name == "collections" {
				invalidCall := `binding.Deployment(binding.Process{}, binding.Configuration{Image: &[]string{"image"}[0], Command: &[]string{"run"}[0]})`
				if test.name == "collections" {
					invalidCall = `binding.Collection(binding.Items{}, binding.Entries{binding.EntriesItem{Key: "key", Value: 3}})`
				}
				writeFixtureFile(t, filepath.Join(workload, "main.go"), "package main\nimport (\n"+test.imports+"\n)\nvar _ = "+invalidCall+"\nfunc main() {}\n")
				out := filepath.Join(workload, "rejected.yaml")
				negative := exec.Command(profilerBinary, "generate", "-dir", workload, "-out", out)
				negative.Env = append(env, "GOPROXY=off")
				output, err := negative.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "fails extension schema") {
					t.Fatalf("invalid %s value should fail schema validation: %v\n%s", test.name, err, output)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("rejected %s value wrote a profile: %v", test.name, err)
				}
			}
		})
	}
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
