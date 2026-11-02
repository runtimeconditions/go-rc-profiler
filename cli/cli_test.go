package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func run(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestVersionIsPlainOrJSON(t *testing.T) {
	out, _, err := run("--version")
	if err != nil || out != "go-rc-profiler dev (unknown)\n" {
		t.Fatalf("%q %v", out, err)
	}
	out, _, err = run("--version", "--json")
	if err != nil || out != `{"name":"go-rc-profiler","version":"dev","coreSchemas":["0.4.0"]}`+"\n" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestBadFlagExitsTwoAfterReporting(t *testing.T) {
	_, stderr, err := run("generate", "-nope")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 2 || !exit.Reported || !strings.Contains(stderr, "-nope") {
		t.Fatalf("%v %q", err, stderr)
	}
	var out bytes.Buffer
	if Exit(&out, err) != 2 || out.Len() != 0 {
		t.Fatalf("reported error printed again: %q", out.String())
	}
}

func TestHelpSucceeds(t *testing.T) {
	if _, stderr, err := run("generate", "-h"); err != nil || !strings.Contains(stderr, "-workload-version") {
		t.Fatalf("%v %q", err, stderr)
	}
}

func TestUnknownCommandExitsOne(t *testing.T) {
	_, _, err := run("bogus")
	var out bytes.Buffer
	if err == nil || Exit(&out, err) != 1 || !strings.Contains(out.String(), `unknown command "bogus"`) {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestValidateExtensionRequiresPackage(t *testing.T) {
	if _, _, err := run("validate-extension"); err == nil || !strings.Contains(err.Error(), "-package is required") {
		t.Fatal(err)
	}
}
