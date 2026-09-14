package cmd

import (
	"strings"
	"testing"
)

func TestHasCRLF(t *testing.T) {
	cases := []struct {
		name, source string
		want         bool
	}{
		{"all CRLF", "a: 1\r\nb: 2\r\n", true},
		{"all LF", "a: 1\nb: 2\n", false},
		{"mixed", "a: 1\r\nb: 2\n", false},
		{"empty", "", false},
		{"no newline at all", "a: 1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCRLF([]byte(tc.source)); got != tc.want {
				t.Fatalf("hasCRLF(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestRestoreCRLF(t *testing.T) {
	got := string(restoreCRLF([]byte("a: 1\n\nb: 2\n")))
	want := "a: 1\r\n\r\nb: 2\r\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEditPreservesCRLF(t *testing.T) {
	in := "a: 1\r\nb: 2\r\n"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"set", []string{"set", ".a", "9"}},
		{"delete", []string{"delete", ".b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(got, "\r\n") {
				t.Fatalf("want CRLF preserved, got %q", got)
			}
			if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
				t.Fatalf("want no bare LF left, got %q", got)
			}
		})
	}
}

func TestEditLFStaysLF(t *testing.T) {
	got, err := execute(t, "a: 1\nb: 2\n", "set", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "\r\n") {
		t.Fatalf("want plain LF, got %q", got)
	}
}

func TestEditMixedLineEndingsLeftAsLF(t *testing.T) {
	got, err := execute(t, "a: 1\r\nb: 2\n", "set", ".a", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "\r\n") {
		t.Fatalf("mixed input should not trigger CRLF restoration, got %q", got)
	}
}
