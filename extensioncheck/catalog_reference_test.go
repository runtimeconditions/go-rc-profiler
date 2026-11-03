package extensioncheck

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const catalogBaseID = "https://example.com/extensions/base/v1/runtimeconditions.extension.yaml"
const catalogChildID = "https://example.com/extensions/child/v1/runtimeconditions.extension.yaml"

func catalogReferenceFixture(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, map[string]string{
		filepath.Join(root, "base.yaml"): fmt.Sprintf(`apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: %s
  version: v1
spec:
  kinds: [{name: cache}]
  interfaceTypes: [{name: key_value, targetKind: cache}]
  interfaceFields: [{name: engine, targetKind: cache, targetType: key_value}]
  fieldValues:
  - field: interface.engine
    targetKind: cache
    targetType: key_value
    values: [redis, memcached]
  schemas:
  - id: cache-engine
    description: Restricts the cache engine.
    appliesToKind: cache
    appliesToInterfaceType: key_value
    schema:
      type: object
      properties:
        interface:
          type: object
          properties:
            engine: {enum: [redis, memcached]}
`, catalogBaseID),
		filepath.Join(root, "child.yaml"): fmt.Sprintf(`apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: %s
  version: v1
spec:
  dependencies: [%s]
`, catalogChildID, catalogBaseID),
	})
	profile := []byte(fmt.Sprintf(`apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsProfile
extensions: [%s, %s]
conditions:
- kind: cache
  interface: {type: key_value, engine: redis}
`, catalogBaseID, catalogChildID))
	return root, profile
}

func TestCatalogIDOnlyReferencesResolveVersionAnnotations(t *testing.T) {
	root, profile := catalogReferenceFixture(t)
	beforeProfile := append([]byte(nil), profile...)
	before := map[string][]byte{}
	for _, name := range []string{"base.yaml", "child.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = data
	}
	opts := ProfileOptions{CatalogRoots: []string{root}}
	got, err := ResolveExtensionClosure([]ExtensionReference{{ID: catalogChildID}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []ExtensionReference{{ID: catalogBaseID, Version: "v1"}, {ID: catalogChildID, Version: "v1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("closure: got %#v, want %#v", got, want)
	}
	for _, engine := range []string{"redis", "memcached"} {
		if err := ValidateProfileYAML(bytes.ReplaceAll(profile, []byte("redis"), []byte(engine)), opts); err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
	}
	if !bytes.Equal(profile, beforeProfile) {
		t.Fatal("Profile bytes changed")
	}
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("definition bytes changed: %s: %v", name, err)
		}
	}
}

func TestCatalogReferenceResolutionRemainsFailClosed(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{"missing_dependency", "missing extension definition"},
		{"missing_declared_closure", "extensions missing dependency"},
		{"invalid_engine", "does not satisfy extension schema"},
		{"wrong_explicit_version", "missing extension definition"},
		{"ambiguous_id", "ambiguous extension ID"},
		{"duplicate_resolved_reference", "duplicate extension id"},
		{"cycle", "extension dependency cycle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, profile := catalogReferenceFixture(t)
			switch test.name {
			case "missing_dependency":
				if err := os.Remove(filepath.Join(root, "base.yaml")); err != nil {
					t.Fatal(err)
				}
			case "missing_declared_closure":
				profile = bytes.ReplaceAll(profile, []byte(catalogBaseID+", "), nil)
			case "invalid_engine":
				profile = bytes.ReplaceAll(profile, []byte("engine: redis"), []byte("engine: invalid"))
			case "wrong_explicit_version":
				profile = bytes.ReplaceAll(profile, []byte(catalogBaseID), []byte(catalogBaseID+":v2"))
			case "ambiguous_id":
				data, err := os.ReadFile(filepath.Join(root, "base.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				writeFiles(t, map[string]string{filepath.Join(root, "other.yaml"): strings.Replace(string(data), "version: v1", "version: v2", 1)})
			case "duplicate_resolved_reference":
				profile = bytes.ReplaceAll(profile, []byte("extensions: ["), []byte("extensions: ["+catalogBaseID+":v1, "))
			case "cycle":
				data, err := os.ReadFile(filepath.Join(root, "base.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				writeFiles(t, map[string]string{filepath.Join(root, "base.yaml"): strings.Replace(string(data), "spec:", "spec:\n  dependencies: ["+catalogChildID+"]", 1)})
			}
			err := ValidateProfileYAML(profile, ProfileOptions{CatalogRoots: []string{root}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("wanted %q, got %v", test.want, err)
			}
		})
	}
}

func TestCatalogExplicitVersionRemainsExact(t *testing.T) {
	root, profile := catalogReferenceFixture(t)
	opts := ProfileOptions{CatalogRoots: []string{root}}
	for _, version := range []string{"v1", "v2"} {
		_, err := ResolveExtensionClosure([]ExtensionReference{{ID: catalogBaseID, Version: version}}, opts)
		if (err == nil) != (version == "v1") {
			t.Fatalf("version %s: %v", version, err)
		}
	}
	profile = bytes.ReplaceAll(profile, []byte(catalogBaseID), []byte(catalogBaseID+":v1"))
	if err := ValidateProfileYAML(profile, opts); err != nil {
		t.Fatal(err)
	}
}
