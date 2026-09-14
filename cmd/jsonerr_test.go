package cmd

import (
	"encoding/json"
	"errors"
	"testing"
)

// decodeJSONErr runs args, asserts stderr held exactly one JSON object, and
// returns it decoded.
func decodeJSONErr(t *testing.T, stdin string, args ...string) jsonError {
	t.Helper()
	out, err := execute(t, stdin, args...)
	if err == nil {
		t.Fatalf("%v: want an error", args)
	}
	var je jsonError
	if jsonErr := json.Unmarshal([]byte(out), &je); jsonErr != nil {
		t.Fatalf("%v: stderr %q is not a single JSON object: %v", args, out, jsonErr)
	}
	return je
}

func TestJSONErrorNoMatch(t *testing.T) {
	je := decodeJSONErr(t, doc, "-o", "json", "meta.missing")
	if je.Kind != "no-match" {
		t.Fatalf("kind = %q, want no-match", je.Kind)
	}
	if je.Path != "meta.missing" {
		t.Fatalf("path = %q, want meta.missing", je.Path)
	}
	if je.Error == "" {
		t.Fatal("error message empty")
	}
}

func TestJSONErrorParse(t *testing.T) {
	je := decodeJSONErr(t, "a: [1, 2", "-o", "json", "a")
	if je.Kind != "parse" {
		t.Fatalf("kind = %q, want parse", je.Kind)
	}
	if je.Line != 1 {
		t.Fatalf("line = %d, want 1", je.Line)
	}
}

func TestJSONErrorIO(t *testing.T) {
	je := decodeJSONErr(t, "", "-o", "json", "a", "/no/such/file.yaml")
	if je.Kind != "io" {
		t.Fatalf("kind = %q, want io", je.Kind)
	}
}

func TestJSONErrorUsage(t *testing.T) {
	je := decodeJSONErr(t, doc, "-o", "json", "a[")
	if je.Kind != "usage" {
		t.Fatalf("kind = %q, want usage", je.Kind)
	}
}

func TestJSONErrorInspect(t *testing.T) {
	je := decodeJSONErr(t, doc, "keys", "-o", "json", "a[")
	if je.Kind != "usage" {
		t.Fatalf("kind = %q, want usage", je.Kind)
	}
}

func TestJSONErrorSilentExitStaysSilent(t *testing.T) {
	// -e's/-q's deliberate "no match" silence holds regardless of -o json.
	out, err := execute(t, doc, "-o", "json", "-e", "meta.missing")
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
}

func TestJSONErrorNotSentInTextMode(t *testing.T) {
	out, err := execute(t, doc, "meta.missing")
	if err == nil {
		t.Fatal("want an error")
	}
	var je jsonError
	if json.Unmarshal([]byte(out), &je) == nil {
		t.Fatal("text-mode output should not be a bare JSON error object")
	}
}
