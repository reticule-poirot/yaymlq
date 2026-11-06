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

// TestYAMLKeyOrderIsDeterministic: the encoder's key comparison is cyclic on a
// digit run past int64 ("(1" < "1B" < "1000...A" < "(1"), so sort.Sort's answer
// followed Go's map iteration. Where a want puts those three in byte order, no
// iteration can produce it without the fix: insertion sort, which sort.Sort uses
// for so few keys, leaves no adjacent pair the comparison calls reversed, and
// every adjacent pair in that order is.
func TestYAMLKeyOrderIsDeterministic(t *testing.T) {
	const cycle = "{(1: 1, 1B: 2, 10000070000010000110A: 3}"
	const sorted = "(1: 1\n10000070000010000110A: 3\n1B: 2\n"
	tests := []struct {
		name string
		in   string
		args []string
		want string
	}{
		{"default output", "f: " + cycle + "\n", []string{".f"}, sorted},
		{"-o yaml", "f: " + cycle + "\n", []string{"-o", "yaml", ".f"}, sorted},
		{"-o raw", "f: " + cycle + "\n", []string{"-o", "raw", ".f"}, sorted},
		{"--print0", "f: " + cycle + "\n", []string{"--print0", ".f"}, strings.TrimSuffix(sorted, "\n")},
		{"fan-out", "f: " + cycle + "\n", []string{".*"}, sorted},
		{"--all-docs", "f: " + cycle + "\n---\nf: 1\n", []string{".f", "--all-docs"}, sorted + "1\n"},
		{"nested", "a: {f: " + cycle + "}\n", []string{"."}, "a:\n  f:\n    (1: 1\n    10000070000010000110A: 3\n    1B: 2\n"},
		{"in a list", "- " + cycle + "\n", []string{"."}, "- (1: 1\n  10000070000010000110A: 3\n  1B: 2\n"},
		{"beside a non-string key", "f: {7: x, (1: 1, 1B: 2, 10000070000010000110A: 3}\n", []string{".f"}, "7: x\n" + sorted},
		{"merged in", "b: &b " + cycle + "\nm: {<<: *b}\n", []string{".m"}, sorted},
		{"NaN key", ".nan: x\n3: c\n1: a\n2: b\n", []string{"."}, "1: a\n2: b\n3: c\n.nan: x\n"},
		{"two NaN keys", ".nan: b\n.NaN: a\n", []string{"."}, ".nan: a\n.nan: b\n"},
		{"a sibling mapping keeps natural order", "o: {a10: 1, a2: 2}\nf: " + cycle + "\n", []string{"."}, "f:\n  (1: 1\n  10000070000010000110A: 3\n  1B: 2\no:\n  a2: 2\n  a10: 1\n"},
		{"scalars keep their spelling", "\"123\": \"456\"\nn: null\nm: \"\\tq\\nz\"\n10000070000010000110A: 1\n", []string{"."}, "10000070000010000110A: 1\n\"123\": \"456\"\nm: |-\n  \tq\n  z\n\"n\": null\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for range 32 {
				got, err := execute(t, tc.in, tc.args...)
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
				}
			}
		})
	}
}
