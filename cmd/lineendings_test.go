package cmd

import (
	"strings"
	"testing"
)

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
