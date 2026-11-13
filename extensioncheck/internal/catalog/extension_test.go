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

func TestProfileStringUsesTheShortURLAndVersion(t *testing.T) {
	full := "https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml"
	for _, test := range []struct {
		name string
		ref  ExtensionReference
		want string
	}{
		{"https id ending in the version tail", ExtensionReference{ID: full, Version: "v1alpha1"}, "https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha1"},
		{"tail names another version", ExtensionReference{ID: full, Version: "v2"}, full + ":v2"},
		{"not an https id", ExtensionReference{ID: "urn:example:jobs", Version: "1.0.0"}, "urn:example:jobs:1.0.0"},
		{"id without the version tail", ExtensionReference{ID: "https://example.com/jobs", Version: "1.0.0"}, "https://example.com/jobs:1.0.0"},
		{"no version", ExtensionReference{ID: full}, full},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.ref.ProfileString(); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			if got, _ := test.ref.MarshalYAML(); got != test.want {
				t.Fatalf("MarshalYAML got %q, want %q", got, test.want)
			}
		})
	}
}

func TestExpandProfileReferenceRebuildsTheFullID(t *testing.T) {
	full := ExtensionReference{ID: "https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml", Version: "v1alpha1"}
	short := ExtensionReference{ID: "https://runtimeconditions.io/extensions/rc/common-integrations", Version: "v1alpha1"}
	if got := ExpandProfileReference(short); got != full {
		t.Fatalf("got %#v, want %#v", got, full)
	}
	for _, same := range []ExtensionReference{full, {ID: "urn:example:jobs", Version: "1"}, {ID: "https://example.com/jobs"}} {
		if got := ExpandProfileReference(same); got != same {
			t.Fatalf("%#v changed to %#v", same, got)
		}
	}
	// A short reference survives a write and a read.
	var back ExtensionReference
	text, _ := full.MarshalYAML()
	if err := yaml.Unmarshal([]byte(text.(string)), &back); err != nil || ExpandProfileReference(back) != full {
		t.Fatalf("round trip gave %#v, %v", back, err)
	}
}
