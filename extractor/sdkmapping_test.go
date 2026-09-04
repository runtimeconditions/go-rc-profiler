package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExtractDirDiscoversVersionAlignedGoSDKMapping(t *testing.T) {
	root := t.TempDir()
	sdkRoot := filepath.Join(root, "nats")
	extensionRoot := filepath.Join(root, "extensions", "nats")
	appRoot := filepath.Join(root, "app")
	writeFilesForTest(t, map[string]string{
		filepath.Join(sdkRoot, "go.mod"): `module github.com/example/nats

go 1.25.0
`,
		filepath.Join(sdkRoot, "client.go"): `package nats

type Conn struct{}

func Connect(url string) (*Conn, error) { return &Conn{}, nil }
func (c *Conn) Publish(subject string, payload []byte) error { return nil }
`,
		filepath.Join(sdkRoot, "jetstream", "client.go"): `package jetstream

import (
	"context"
	"github.com/example/nats"
)

type KeyValueManager interface { KeyValue(ctx context.Context, bucket string) (KeyValue, error) }
type KeyValue interface { Put(ctx context.Context, key string, value []byte) (uint64, error) }
type KeyValueConfig struct { Bucket string }
type KeyValueCreator interface { CreateKeyValue(ctx context.Context, cfg KeyValueConfig) (KeyValue, error) }
type client struct{}
type keyValue struct{}

type JetStream interface { KeyValueManager; KeyValueCreator }

func New(connection *nats.Conn) JetStream { return client{} }
func (client) KeyValue(context.Context, string) (KeyValue, error) { return keyValue{}, nil }
func (client) CreateKeyValue(context.Context, KeyValueConfig) (KeyValue, error) { return keyValue{}, nil }
func (keyValue) Put(context.Context, string, []byte) (uint64, error) { return 0, nil }
`,
		filepath.Join(extensionRoot, "runtimeconditions.extension.yaml"): testNATSExtension,
		filepath.Join(appRoot, "go.mod"): `module github.com/example/app

go 1.25.0

require github.com/example/nats v1.2.3

replace github.com/example/nats => ../nats
`,
		filepath.Join(appRoot, "main.go"): `package main

import (
	"context"
	"os"
	"github.com/example/nats"
	"github.com/example/nats/jetstream"
)

func main() {
	connection, _ := nats.Connect("nats://localhost:4222")
	_ = connection.Publish("orders.created", nil)
	_ = connection.Publish(os.Getenv("SUBJECT"), nil)
	js := jetstream.New(connection)
	var config = jetstream.KeyValueConfig{Bucket: "profiles"}
	_, _ = js.CreateKeyValue(context.Background(), config)
	mutated := jetstream.KeyValueConfig{Bucket: "must.not.emit"}
	mutated.Bucket = os.Getenv("BUCKET")
	_, _ = js.CreateKeyValue(context.Background(), mutated)
	escaped := jetstream.KeyValueConfig{Bucket: "escaped.must.not.emit"}
	_ = &escaped
	_, _ = js.CreateKeyValue(context.Background(), escaped)
	store, _ := js.KeyValue(context.Background(), "profiles")
	_, _ = store.Put(context.Background(), "current", []byte("production"))
	other, _ := nats.Connect("nats://other:4222")
	_ = other.Publish("audit.created", nil)
}
`,
	})
	writeTestGoSDKMapping(t, sdkRoot, "v1.2.3")

	profile, err := ExtractDir(appRoot, Options{Name: "go-sdk", WorkloadURI: "github.com/example/app", WorkloadVersion: "v0.1.0", ExtensionRoots: []string{filepath.Dir(extensionRoot)}, RequireGoPackages: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Extensions) != 1 || profile.Extensions[0] != testNATSExtensionID {
		t.Fatalf("unexpected extensions: %#v", profile.Extensions)
	}
	if len(profile.Conditions) != 2 {
		t.Fatalf("expected two source-proven dependency conditions, got %#v", profile.Conditions)
	}
	wantOperations := []map[string]any{
		{"resource": "connection", "action": "connect"},
		{"resource": "subject", "action": "publish", "subject": "orders.created"},
		{"resource": "key_value", "action": "create", "bucket": "profiles"},
		{"resource": "key_value", "action": "inspect", "bucket": "profiles"},
		{"resource": "key_value", "action": "write", "bucket": "profiles"},
	}
	if len(profile.Conditions[0].Interface.Operations) != len(wantOperations) {
		t.Fatalf("first dependency operations: got %#v", profile.Conditions)
	}
	for index, want := range wantOperations {
		got := profile.Conditions[0].Interface.Operations[index].Fields
		if !equalJSONValue(got, want) {
			t.Fatalf("operation %d: got %#v want %#v", index, got, want)
		}
	}
	wantOther := []map[string]any{{"resource": "connection", "action": "connect"}, {"resource": "subject", "action": "publish", "subject": "audit.created"}}
	if len(profile.Conditions[1].Interface.Operations) != len(wantOther) {
		t.Fatalf("second dependency operations: got %#v", profile.Conditions)
	}
	for index, want := range wantOther {
		got := profile.Conditions[1].Interface.Operations[index].Fields
		if !equalJSONValue(got, want) {
			t.Fatalf("other connection operation %d: got %#v want %#v", index, got, want)
		}
	}
}

func TestExtractDirRejectsTamperedGoSDKMapping(t *testing.T) {
	root := t.TempDir()
	sdkRoot := filepath.Join(root, "sdk")
	appRoot := filepath.Join(root, "app")
	extensionRoot := filepath.Join(root, "extensions", "nats")
	writeFilesForTest(t, map[string]string{
		filepath.Join(sdkRoot, "go.mod"):                                 "module github.com/example/nats\n\ngo 1.25.0\n",
		filepath.Join(sdkRoot, "client.go"):                              "package nats\nfunc Connect(string) error { return nil }\n",
		filepath.Join(extensionRoot, "runtimeconditions.extension.yaml"): testNATSExtension,
		filepath.Join(appRoot, "go.mod"):                                 "module github.com/example/app\n\ngo 1.25.0\n\nrequire github.com/example/nats v1.2.3\nreplace github.com/example/nats => ../sdk\n",
		filepath.Join(appRoot, "main.go"):                                "package main\nimport nats \"github.com/example/nats\"\nfunc main() { _ = nats.Connect(\"nats://localhost\") }\n",
	})
	writeTestGoSDKMapping(t, sdkRoot, "v1.2.3")
	mappingPath := filepath.Join(sdkRoot, "runtimeconditions", "mappings", "nats.yaml")
	data, err := os.ReadFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "action: connect", "action: inspect", 1))
	if err := os.WriteFile(mappingPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ExtractDir(appRoot, Options{Name: "tampered", ExtensionRoots: []string{filepath.Dir(extensionRoot)}, RequireGoPackages: true})
	if err == nil || !strings.Contains(err.Error(), "mapping SHA-256 does not match index") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTestGoSDKMapping(t *testing.T, sdkRoot string, version string) {
	t.Helper()
	argument := func(parameter string) map[string]any {
		return map[string]any{"argument": map[string]any{"parameter": parameter}}
	}
	template := func(resource string, action string) map[string]any {
		return map[string]any{"kind": "nats", "interfaceType": "service", "operation": map[string]any{"resource": resource, "action": action}}
	}
	goBody := map[string]any{
		"calls": []any{
			map[string]any{"id": "connect", "symbol": map[string]any{"package": "github.com/example/nats", "function": "Connect"}, "conditionTemplate": template("connection", "connect"), "produces": map[string]any{"stateType": "nats.connection", "dependencyIdentity": "new", "bindings": map[string]any{}}},
			map[string]any{"id": "publish", "symbol": map[string]any{"package": "github.com/example/nats", "receiver": "Conn", "method": "Publish"}, "receiverState": "nats.connection", "conditionTemplate": template("subject", "publish"), "operationBindings": map[string]any{"subject": argument("subject")}},
			map[string]any{"id": "jetstream-new", "symbol": map[string]any{"package": "github.com/example/nats/jetstream", "function": "New"}, "argumentState": map[string]any{"stateType": "nats.connection", "argument": map[string]any{"parameter": "connection"}}, "produces": map[string]any{"stateType": "nats.jetstream", "dependencyIdentity": "inherit", "bindings": map[string]any{}}},
			map[string]any{"id": "create-key-value", "symbol": map[string]any{"package": "github.com/example/nats/jetstream", "receiver": "KeyValueCreator", "method": "CreateKeyValue"}, "receiverState": "nats.jetstream", "conditionTemplate": template("key_value", "create"), "operationBindings": map[string]any{"bucket": map[string]any{"argument": map[string]any{"parameter": "cfg", "field": "Bucket"}}}, "produces": map[string]any{"stateType": "nats.key_value", "dependencyIdentity": "inherit", "bindings": map[string]any{"bucket": map[string]any{"argument": map[string]any{"parameter": "cfg", "field": "Bucket"}}}}},
			map[string]any{"id": "key-value", "symbol": map[string]any{"package": "github.com/example/nats/jetstream", "receiver": "KeyValueManager", "method": "KeyValue"}, "receiverState": "nats.jetstream", "conditionTemplate": template("key_value", "inspect"), "operationBindings": map[string]any{"bucket": argument("bucket")}, "produces": map[string]any{"stateType": "nats.key_value", "dependencyIdentity": "inherit", "bindings": map[string]any{"bucket": argument("bucket")}}},
			map[string]any{"id": "key-value-put", "symbol": map[string]any{"package": "github.com/example/nats/jetstream", "receiver": "KeyValue", "method": "Put"}, "receiverState": "nats.key_value", "conditionTemplate": template("key_value", "write"), "operationBindings": map[string]any{"bucket": map[string]any{"state": "bucket"}}},
		},
	}
	semantic, err := json.Marshal(goBody)
	if err != nil {
		t.Fatal(err)
	}
	semanticDigest := sha256.Sum256(semantic)
	mapping := map[string]any{
		"apiVersion": "runtimeconditions.io/sdk-mapping/v1alpha1",
		"kind":       "RuntimeConditionsSDKMapping",
		"metadata":   map[string]any{"name": "nats.test", "module": "github.com/example/nats", "moduleVersion": version, "language": "go", "semanticSha256": hex.EncodeToString(semanticDigest[:])},
		"extension":  map[string]any{"id": testNATSExtensionID, "version": "0.1.0", "semanticSha256": "test-extension-digest"},
		"go":         goBody,
	}
	mappingData, err := yaml.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mappingPath := filepath.Join(sdkRoot, "runtimeconditions", "mappings", "nats.yaml")
	writeFilesForTest(t, map[string]string{mappingPath: string(mappingData)})
	mappingDigest := sha256.Sum256(mappingData)
	index := map[string]any{
		"apiVersion": "runtimeconditions.io/sdk-mapping/v1alpha1",
		"kind":       "RuntimeConditionsSDKMappingIndex",
		"metadata":   map[string]any{"module": "github.com/example/nats", "moduleVersion": version, "language": "go"},
		"mappings":   []any{map[string]any{"name": "nats.test", "path": "runtimeconditions/mappings/nats.yaml", "sha256": hex.EncodeToString(mappingDigest[:])}},
	}
	indexData, err := yaml.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	writeFilesForTest(t, map[string]string{filepath.Join(sdkRoot, "runtimeconditions", "index.yaml"): string(indexData)})
}

func equalJSONValue(left any, right any) bool {
	leftData, _ := json.Marshal(left)
	rightData, _ := json.Marshal(right)
	return string(leftData) == string(rightData)
}

const testNATSExtensionID = "https://example.com/runtimeconditions/nats/0.1.0/runtimeconditions.extension.yaml"

const testNATSExtension = `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: https://example.com/runtimeconditions/nats/0.1.0/runtimeconditions.extension.yaml
  version: 0.1.0
  semanticSha256: test-extension-digest
spec:
  kinds:
  - name: nats
  interfaceTypes:
  - name: service
    targetKind: nats
  interfaceFields:
  - name: operations
    targetKind: nats
    targetType: service
`
