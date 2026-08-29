package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderFormats(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		format string
		want   string
	}{
		{"yaml scalar", "hi", "yaml", "hi\n"},
		{"yaml map", map[string]any{"a": 1}, "yaml", "a: 1\n"},
		{"json scalar", "hi", "json", "\"hi\"\n"},
		{"json list", []any{1, 2}, "json", "[\n  1,\n  2\n]\n"},
		{"raw string", "hi", "raw", "hi\n"},
		{"raw int", 42, "raw", "42\n"},
		{"raw bool", true, "raw", "true\n"},
		{"raw nil", nil, "raw", "\n"},
		{"raw map falls back to yaml", map[string]any{"a": 1}, "raw", "a: 1\n"},
		{"empty format defaults to yaml", "hi", "", "hi\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := render(&buf, tc.value, tc.format); err != nil {
				t.Fatalf("render: %v", err)
			}
			if buf.String() != tc.want {
				t.Fatalf("render(%v, %q) = %q, want %q", tc.value, tc.format, buf.String(), tc.want)
			}
		})
	}
}

func TestRenderUnknownFormat(t *testing.T) {
	err := render(&bytes.Buffer{}, "x", "toml")
	if err == nil || !strings.Contains(err.Error(), "toml") {
		t.Fatalf("want error naming the bad format, got %v", err)
	}
}
