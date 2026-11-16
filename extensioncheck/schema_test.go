package extensioncheck

import (
	"strings"
	"testing"
)

func TestParseDocumentRejectsCyclicAlias(t *testing.T) {
	if _, err := parseDocument([]byte("root: &root\n  nested: *root\n")); err == nil || !strings.Contains(err.Error(), "cyclic YAML alias") {
		t.Fatalf("expected cyclic YAML alias error, got %v", err)
	}
}
