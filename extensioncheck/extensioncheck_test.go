package extensioncheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type authoringFixture struct {
	Mode                   string   `yaml:"mode"`
	Root                   string   `yaml:"root"`
	Languages              []string `yaml:"languages"`
	RequireLanguagePackage bool     `yaml:"requireLanguagePackage"`
	WantErrorContains      string   `yaml:"wantErrorContains"`
}

func TestValidateFirstPartyExtensions(t *testing.T) {
	extensionsRoot := repoPath(t, "extensions")

	tests := []struct {
		root     string
		language string
		require  bool
	}{
		{root: filepath.Join(extensionsRoot, "common-integrations"), language: "go", require: true},
		{root: filepath.Join(extensionsRoot, "env-configuration"), language: "go", require: true},
		{root: filepath.Join(extensionsRoot, "common-integrations"), language: "java", require: true},
		{root: filepath.Join(extensionsRoot, "env-configuration"), language: "java", require: true},
	}
	for _, test := range tests {
		t.Run(filepath.Base(test.root)+"/"+test.language, func(t *testing.T) {
			err := ValidateExtension(test.root, Options{
				Language:               test.language,
				RequireLanguagePackage: test.require,
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExtensionAuthoringFixtures(t *testing.T) {
	fixtureRoot := filepath.Join("testdata", "authoring")
	entries, err := os.ReadDir(fixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		caseDir := filepath.Join(fixtureRoot, entry.Name())
		fixture := readAuthoringFixture(t, filepath.Join(caseDir, "fixture.yaml"))
		for _, language := range fixture.Languages {
			t.Run(entry.Name()+"/"+language, func(t *testing.T) {
				err := runAuthoringFixture(caseDir, fixture, language)
				if fixture.WantErrorContains == "" {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if err == nil {
					t.Fatalf("expected error containing %q", fixture.WantErrorContains)
				}
				if !strings.Contains(err.Error(), fixture.WantErrorContains) {
					t.Fatalf("unexpected error:\n%v", err)
				}
			})
		}
	}
}

func readAuthoringFixture(t *testing.T, path string) authoringFixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture authoringFixture
	if err := yaml.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Mode == "" {
		t.Fatalf("%s: mode is required", path)
	}
	if fixture.Root == "" {
		t.Fatalf("%s: root is required", path)
	}
	if len(fixture.Languages) == 0 {
		t.Fatalf("%s: languages is required", path)
	}
	return fixture
}

func runAuthoringFixture(caseDir string, fixture authoringFixture, language string) error {
	root := filepath.Join(caseDir, filepath.FromSlash(fixture.Root))
	opts := Options{
		Language:               language,
		RequireLanguagePackage: fixture.RequireLanguagePackage,
	}
	switch fixture.Mode {
	case "extension":
		return ValidateExtension(root, opts)
	case "extensions":
		return ValidateExtensions(root, opts)
	default:
		return errUnknownFixtureMode(fixture.Mode)
	}
}

func errUnknownFixtureMode(mode string) error {
	return validationErrors{"unknown fixture mode " + mode}
}

func TestValidateExtensionResolvesDependenciesBeforeBindings(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	child := filepath.Join(root, "child")
	writeFiles(t, map[string]string{
		filepath.Join(base, "base-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/base/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: base.cache
  interfaceTypes:
    - name: base.key_value
      targetKind: base.cache
`,
		filepath.Join(base, "go", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/base/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../base-v1alpha1.yaml
  language: go

go:
  importPath: github.com/example/runtimeconditions/base/go
  package: base

  declarations:
    - function: Cache
      nameArg: 0
      kind: base.cache
      interfaceType: base.key_value
`,
		filepath.Join(base, "go", "declarations.go"): `package base

type Declaration struct{}

func Cache(name string) Declaration {
	return Declaration{}
}
`,
		filepath.Join(child, "child-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/child/v1alpha1/runtimeconditions.extension.yaml

spec:
  dependencies:
    - https://example.com/runtimeconditions/base/v1alpha1/runtimeconditions.extension.yaml
  conditionFields:
    - name: configuration
      appliesToKinds:
        - base.cache
      appliesToInterfaceTypes:
        - base.key_value
  fieldValues:
    - field: configuration.env[].property
      targetKind: base.cache
      targetType: base.key_value
      values:
        - url
`,
		filepath.Join(child, "go", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/child/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../child-v1alpha1.yaml
  language: go

go:
  importPath: github.com/example/runtimeconditions/child/go
  package: child

  options:
    - function: Env
      target: configuration.env[]
      appliesToKinds:
        - base.cache
      appliesToInterfaceTypes:
        - base.key_value
      stringArgs:
        property: 0
        name: 1
`,
		filepath.Join(child, "go", "declarations.go"): `package child

type Option struct{}

func Env(property, name string) Option {
	return Option{}
}
`,
	})

	err := ValidateExtension(child, Options{
		Language:               "go",
		RequireLanguagePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateExtensionRejectsBindingWithoutGoDeclaration(t *testing.T) {
	root := t.TempDir()
	extension := filepath.Join(root, "broken")
	writeFiles(t, map[string]string{
		filepath.Join(extension, "broken-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/broken/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: broken.cache
`,
		filepath.Join(extension, "go", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/broken/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../broken-v1alpha1.yaml
  language: go

go:
  importPath: github.com/example/runtimeconditions/broken/go
  package: broken

  declarations:
    - function: Missing
      nameArg: 0
      kind: broken.cache
`,
		filepath.Join(extension, "go", "declarations.go"): `package broken

type Declaration struct{}

func Present(name string) Declaration {
	return Declaration{}
}
`,
	})

	err := ValidateExtension(extension, Options{
		Language:               "go",
		RequireLanguagePackage: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "binding declaration function Missing is not declared") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtensionRejectsBindingVocabularyOutsideResolvedGraph(t *testing.T) {
	root := t.TempDir()
	extension := filepath.Join(root, "broken")
	writeFiles(t, map[string]string{
		filepath.Join(extension, "broken-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/broken/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: broken.cache
`,
		filepath.Join(extension, "go", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/broken/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../broken-v1alpha1.yaml
  language: go

go:
  importPath: github.com/example/runtimeconditions/broken/go
  package: broken

  declarations:
    - function: Broken
      nameArg: 0
      kind: missing.cache
`,
		filepath.Join(extension, "go", "declarations.go"): `package broken

type Declaration struct{}

func Broken(name string) Declaration {
	return Declaration{}
}
`,
	})

	err := ValidateExtension(extension, Options{
		Language:               "go",
		RequireLanguagePackage: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "declaration kind missing.cache") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtensionRejectsOverlappingConditionFieldDefinitions(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	child := filepath.Join(root, "child")
	writeFiles(t, map[string]string{
		filepath.Join(base, "base-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/base/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: shared.object
  interfaceTypes:
    - name: shared.api
      targetKind: shared.object
  conditionFields:
    - name: configuration
      appliesToKinds:
        - shared.object
      appliesToInterfaceTypes:
        - shared.api
`,
		filepath.Join(child, "child-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/child/v1alpha1/runtimeconditions.extension.yaml

spec:
  dependencies:
    - https://example.com/runtimeconditions/base/v1alpha1/runtimeconditions.extension.yaml
  conditionFields:
    - name: configuration
      appliesToKinds:
        - shared.object
      appliesToInterfaceTypes:
        - shared.api
`,
	})

	err := ValidateExtension(child, Options{Language: "go"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "conditionField:configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtensionRejectsJavaBindingWithoutEntryClass(t *testing.T) {
	root := t.TempDir()
	extension := filepath.Join(root, "broken")
	writeFiles(t, map[string]string{
		filepath.Join(extension, "broken-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/java-missing-class/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: java.broken
`,
		filepath.Join(extension, "java", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/java-missing-class/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../broken-v1alpha1.yaml
  language: java

java:
  package: io.example.broken

  declarations:
    - function: declare
      nameArg: 0
      kind: java.broken
`,
		filepath.Join(extension, "java", "src", "main", "java", "io", "example", "broken", "Broken.java"): `package io.example.broken;

public final class Broken {
    public static final class Declaration {
    }

    public static Declaration declare(String name) {
        return new Declaration();
    }
}
`,
	})

	err := ValidateExtension(extension, Options{
		Language:               "java",
		RequireLanguagePackage: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "Java declaration function declare is missing class") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtensionRejectsJavaBindingWithoutStaticMethod(t *testing.T) {
	root := t.TempDir()
	extension := filepath.Join(root, "broken")
	writeFiles(t, map[string]string{
		filepath.Join(extension, "broken-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/java-missing-method/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: java.broken
`,
		filepath.Join(extension, "java", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/java-missing-method/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../broken-v1alpha1.yaml
  language: java

java:
  package: io.example.broken

  declarations:
    - class: Broken
      function: missing
      nameArg: 0
      kind: java.broken
`,
		filepath.Join(extension, "java", "src", "main", "java", "io", "example", "broken", "Broken.java"): `package io.example.broken;

public final class Broken {
    public static final class Declaration {
    }

    public static Declaration present(String name) {
        return new Declaration();
    }
}
`,
	})

	err := ValidateExtension(extension, Options{
		Language:               "java",
		RequireLanguagePackage: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "binding declaration function Broken.missing is not declared in Java package") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtensionRejectsJavaBindingConstantMismatch(t *testing.T) {
	root := t.TempDir()
	extension := filepath.Join(root, "broken")
	writeFiles(t, map[string]string{
		filepath.Join(extension, "broken-v1alpha1.yaml"): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition

metadata:
  id: https://example.com/runtimeconditions/java-constant-mismatch/v1alpha1/runtimeconditions.extension.yaml

spec:
  kinds:
    - name: java.cache
  interfaceTypes:
    - name: key_value
      targetKind: java.cache
  interfaceFields:
    - name: engine
      targetKind: java.cache
      targetType: key_value
  fieldValues:
    - field: interface.engine
      targetKind: java.cache
      targetType: key_value
      values:
        - redis
`,
		filepath.Join(extension, "java", goBindingsManifest): `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsBinding

metadata:
  extension: https://example.com/runtimeconditions/java-constant-mismatch/v1alpha1/runtimeconditions.extension.yaml
  extensionDefinition: ../broken-v1alpha1.yaml
  language: java

java:
  package: io.example.broken

  constants:
    Broken.Engine.REDIS: redis

  declarations:
    - class: Broken
      function: declare
      nameArg: 0
      kind: java.cache
      interfaceType: key_value
`,
		filepath.Join(extension, "java", "src", "main", "java", "io", "example", "broken", "Broken.java"): `package io.example.broken;

public final class Broken {
    public enum Engine {
        REDIS("valkey");

        private final String value;

        Engine(String value) {
            this.value = value;
        }
    }

    public static final class Declaration {
    }

    public static Declaration declare(String name) {
        return new Declaration();
    }
}
`,
	})

	err := ValidateExtension(extension, Options{
		Language:               "java",
		RequireLanguagePackage: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `binding constant Broken.Engine.REDIS value "redis" does not match Java value "valkey"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func repoPath(t *testing.T, parts ...string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

func writeFiles(t *testing.T, files map[string]string) {
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
