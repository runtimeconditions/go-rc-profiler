package catalog

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExtensionReferenceYAML(t *testing.T) {
	for _, test := range []struct {
		name, input string
		want        ExtensionReference
	}{
		{"object", "{id: https://example.com/extension, version: v1alpha1}", ExtensionReference{ID: "https://example.com/extension", Version: "v1alpha1"}},
		{"opaque object id", "{id: 'urn:example:extension:1', version: '2'}", ExtensionReference{ID: "urn:example:extension:1", Version: "2"}},
		{"versioned scalar", "https://example.com/extension:v1alpha1", ExtensionReference{ID: "https://example.com/extension", Version: "v1alpha1"}},
		{"unversioned scalar", "https://example.com/extension", ExtensionReference{ID: "https://example.com/extension"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got ExtensionReference
			if err := yaml.Unmarshal([]byte(test.input), &got); err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestExtensionReferenceYAMLRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{
		"{}",
		"{version: '1'}",
		"{id: example}",
		"{id: '', version: '1'}",
		"{id: example, version: ''}",
		"{id: 123, version: '1'}",
		"{id: example, version: 1}",
		"{id: example, version: null}",
		"{id: example, version: '1', uri: example}",
		"{id: example, id: other, version: '1'}",
		"[]",
		"''",
		"123",
		"'https://example.com/extension:'",
	} {
		t.Run(input, func(t *testing.T) {
			var reference ExtensionReference
			if err := yaml.Unmarshal([]byte(input), &reference); err == nil {
				t.Fatalf("accepted invalid reference %s as %#v", input, reference)
			}
		})
	}
}

func TestExtensionReferenceYAMLPreservesProfileSerialization(t *testing.T) {
	reference := ExtensionReference{ID: "https://example.com/extension", Version: "v1alpha1"}
	data, err := yaml.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://example.com/extension:v1alpha1\n"; string(data) != want {
		t.Fatalf("got %q, want %q", data, want)
	}
	var decoded ExtensionReference
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != reference {
		t.Fatalf("round trip changed %#v to %#v", reference, decoded)
	}
}
