package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestExitCode(t *testing.T) {
	if got := exitCode(nil, &bytes.Buffer{}); got != 0 {
		t.Fatalf("nil err -> %d, want 0", got)
	}

	if got := exitCode(silentExit{code: 3}, &bytes.Buffer{}); got != 3 {
		t.Fatalf("silentExit{3} -> %d, want 3", got)
	}

	var buf bytes.Buffer
	if got := exitCode(errors.New("boom"), &buf); got != 1 {
		t.Fatalf("plain err -> %d, want 1", got)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("error message not printed: %q", buf.String())
	}
}

func TestExitCodeWrappedSilentExit(t *testing.T) {
	err := errors.Join(errors.New("context"), silentExit{code: 1})
	if got := exitCode(err, &bytes.Buffer{}); got != 1 {
		t.Fatalf("wrapped silentExit -> %d, want 1", got)
	}
}
