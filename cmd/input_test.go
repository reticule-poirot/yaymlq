package cmd

import (
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

func TestReadCappedExactlyAtLimit(t *testing.T) {
	data, err := readCapped(strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatalf("readCapped: %v", err)
	}
	if string(data) != "12345" {
		t.Fatalf("got %q", data)
	}
}

func TestReadCappedOneOverLimit(t *testing.T) {
	_, err := readCapped(strings.NewReader("123456"), 5)
	if err == nil || !errors.Is(err, errInputTooLarge) {
		t.Fatalf("want errInputTooLarge, got %v", err)
	}
}

func TestReadCappedUnderLimit(t *testing.T) {
	data, err := readCapped(strings.NewReader("12"), 5)
	if err != nil {
		t.Fatalf("readCapped: %v", err)
	}
	if string(data) != "12" {
		t.Fatalf("got %q", data)
	}
}

func TestReadCappedZeroIsUnlimited(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	data, err := readCapped(strings.NewReader(big), 0)
	if err != nil {
		t.Fatalf("readCapped: %v", err)
	}
	if len(data) != len(big) {
		t.Fatalf("got %d bytes, want %d", len(data), len(big))
	}
}

func TestReadCappedNegativeIsUsageError(t *testing.T) {
	_, err := readCapped(strings.NewReader("x"), -1)
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("want exit 3 (usage), got %d (%v)", got, err)
	}
}

// TestReadCappedMaxInt64DoesNotOverflow guards #74: limit+1 used to wrap to
// math.MinInt64 at exactly math.MaxInt64, silently returning an empty
// buffer with no error instead of reading the real input.
func TestReadCappedMaxInt64DoesNotOverflow(t *testing.T) {
	data, err := readCapped(strings.NewReader("hello"), math.MaxInt64)
	if err != nil {
		t.Fatalf("readCapped: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q, want the real input — the old bug silently returned empty", data)
	}
}

func TestValidateDocSelectionConflict(t *testing.T) {
	if _, err := execute(t, doc, "--doc", "5", "--all-docs", "meta.name"); err == nil {
		t.Fatal("want an error: --doc and --all-docs together")
	}
}

func TestValidateDocSelectionNegativeDoc(t *testing.T) {
	if _, err := execute(t, doc, "--doc", "-1", "meta.name"); err == nil {
		t.Fatal("want an error: negative --doc")
	}
}

func TestValidateDocSelectionAllDocsWithoutExplicitDocIsFine(t *testing.T) {
	// --all-docs alone (--doc left at its default of 0, never explicitly
	// set) must not trip the conflict check.
	if _, err := execute(t, doc, "--all-docs", "meta.name"); err != nil {
		t.Fatalf("execute: %v", err)
	}
}
