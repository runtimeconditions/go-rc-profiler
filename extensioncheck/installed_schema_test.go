package extensioncheck

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestInstalledCoreSchema(t *testing.T) {
	orig := installedCoreSchemaBase64
	defer func() { installedCoreSchemaBase64 = orig }()

	t.Run("empty fails closed with ErrNoInstalledCoreSchema", func(t *testing.T) {
		installedCoreSchemaBase64 = ""
		schema, err := InstalledCoreSchema()
		if schema != nil {
			t.Errorf("expected nil schema, got %v", schema)
		}
		if !errors.Is(err, ErrNoInstalledCoreSchema) {
			t.Fatalf("expected ErrNoInstalledCoreSchema, got %v", err)
		}
	})

	t.Run("invalid base64 encoding error", func(t *testing.T) {
		installedCoreSchemaBase64 = "not-valid-base64!!!"
		schema, err := InstalledCoreSchema()
		if schema != nil {
			t.Errorf("expected nil schema, got %v", schema)
		}
		if err == nil || !strings.Contains(err.Error(), "installed core profile schema encoding") {
			t.Fatalf("expected encoding error, got %v", err)
		}
		if errors.Is(err, ErrNoInstalledCoreSchema) {
			t.Fatalf("decode error should not match ErrNoInstalledCoreSchema")
		}
	})

	t.Run("valid schema decodes successfully", func(t *testing.T) {
		raw := "title: sample schema\nversion: 1.0.0\n"
		installedCoreSchemaBase64 = base64.StdEncoding.EncodeToString([]byte(raw))
		schema, err := InstalledCoreSchema()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(schema) != raw {
			t.Fatalf("expected %q, got %q", raw, string(schema))
		}
	})
}
