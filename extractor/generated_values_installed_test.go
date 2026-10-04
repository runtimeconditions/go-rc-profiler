package extractor

import (
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

func TestInstalledGeneratedShapeDecoding(t *testing.T) {
	proxy := os.Getenv("RC_GO_BINDING_CONFORMANCE_PROXY")
	if proxy == "" {
		t.Skip("set RC_GO_BINDING_CONFORMANCE_PROXY to an absolute local Go proxy")
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
	const prefix = "example.com/runtimeconditions/conformance/"
	modules := []string{"recursive-reference", "object-alternatives", "heterogeneous-union", "collections-and-maps"}
	mod := "module example.com/generated-shapes\n\ngo 1.22\n\nrequire (\n"
	for _, module := range modules {
		mod += prefix + module + " v1.0.0\n"
	}
	mod += ")\n"
	source := `package main
import recur "example.com/runtimeconditions/conformance/recursive-reference"
import alt "example.com/runtimeconditions/conformance/object-alternatives"
import union "example.com/runtimeconditions/conformance/heterogeneous-union"
import coll "example.com/runtimeconditions/conformance/collections-and-maps"
var recursiveValue = recur.Root{Name: "root", Children: recur.RootChildren{recur.NodeNode{Name: "child"}}}
var alternativeValue = alt.Configuration{Image: &[]string{"image"}[0], Command: nil}
var zeroPointerValue = alt.Configuration{Command: new(string)}
var unionValue = union.TargetString("target")
var entriesValue = coll.Entries{coll.EntriesItem{Key: "key", Value: 1}}
var labelsValue = coll.Labels{"team": "example"}
var tagsValue = coll.Tags{"tag"}
func main() {}
`
	for name, data := range map[string]string{"go.mod": mod, "main.go": source} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOPROXY", "file://"+proxy)
	download := exec.Command("go", "mod", "download", "all")
	download.Dir = root
	if output, err := download.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, output)
	}
	t.Setenv("GOPROXY", "off")
	fset, files, semantic, err := gosource.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	packages, err := extensioncheck.ResolveImportedGoPackages(t.Context(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := newGeneratedReader(fset, semantic, packages)
	values := map[string]ast.Expr{}
	for _, file := range files {
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			if spec, ok := node.(*ast.ValueSpec); ok && len(spec.Names) == 1 && len(spec.Values) == 1 {
				values[spec.Names[0].Name] = spec.Values[0]
			}
			return true
		})
	}
	tests := []struct {
		name, module, rootType string
		want                   any
	}{
		{"recursiveValue", "recursive-reference", "Root", map[string]any{"name": "root", "children": []any{map[string]any{"name": "child"}}}},
		{"alternativeValue", "object-alternatives", "Configuration", map[string]any{"image": "image"}},
		{"zeroPointerValue", "object-alternatives", "Configuration", map[string]any{"command": ""}},
		{"unionValue", "heterogeneous-union", "TargetTarget", "target"},
		{"entriesValue", "collections-and-maps", "Entries", []any{map[string]any{"key": "key", "value": int64(1)}}},
		{"labelsValue", "collections-and-maps", "Labels", map[string]any{"team": "example"}},
		{"tagsValue", "collections-and-maps", "Tags", []any{"tag"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := prefix + test.module
			ref := map[string]any{"type": test.rootType}
			got, err := reader.decode(values[test.name], ref, path, 0)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("decoded value: got %#v, want %#v, error %v", got, test.want, err)
			}
		})
	}
}
