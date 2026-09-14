package cmd

import (
	"strings"
	"testing"
)

func TestDetectIndent(t *testing.T) {
	cases := []struct {
		name, source string
		want         int
	}{
		{"two space", "a:\n  b: 1\n", 2},
		{"four space", "a:\n    b: 1\n    c:\n        d: 2\n", 4},
		{"flat, nothing indented", "a: 1\nb: 2\n", 0},
		{"blank lines ignored", "a:\n\n    b: 1\n", 4},
		{"empty", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectIndent([]byte(tc.source)); got != tc.want {
				t.Fatalf("detectIndent(%q) = %d, want %d", tc.source, got, tc.want)
			}
		})
	}
}

func TestEditAutoDetectsFourSpaceIndent(t *testing.T) {
	in := "a:\n    b: 1\n    c: 2\n"
	got, err := execute(t, in, "set", ".a.b", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "a:\n    b: 9\n    c: 2\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEditIndentFlagOverridesDetection(t *testing.T) {
	in := "a:\n    b: 1\n"
	got, err := execute(t, in, "set", "--indent", "3", ".a.b", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "\n   b: 9\n") {
		t.Fatalf("want 3-space indent, got %q", got)
	}
}

func TestEditFlatDocDefaultsToTwoSpaceIndent(t *testing.T) {
	got, err := execute(t, "a: 1\n", "set", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "a: 9" {
		t.Fatalf("got %q", got)
	}
}

func TestEditIndentZeroRejected(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "set", "--indent", "0", ".a", "9"); err == nil {
		t.Fatal("want error for --indent 0")
	}
}

func TestEditIndentNegativeRejected(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "set", "--indent", "-1", ".a", "9"); err == nil {
		t.Fatal("want error for --indent -1")
	}
}
