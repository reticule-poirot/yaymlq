package cmd

import (
	"strings"
	"testing"
)

// TestTimestampScalarsReadAsTheirSourceText is #321. yaml.v4 decodes an
// unquoted date-like scalar (`2024-06-01`, and several other spellings) into
// Go's time.Time rather than a string, which leaked straight through every
// read verb: type named a Go type outside the documented vocabulary, len/keys
// errored or misbehaved, and get/-o json printed an expanded, re-formatted
// instant that is not the text the document holds — several distinct
// spellings even collapse to the identical time.Time, so there is no way
// back to the original once decoded. decodeDocs now retags every !!timestamp
// scalar to !!str before the value callers see is built, so it reads back
// exactly as written.
func TestTimestampScalarsReadAsTheirSourceText(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"bare date", "expiry: 2024-06-01\n", "2024-06-01"},
		{"full timestamp", "expiry: 2024-06-01T10:00:00Z\n", "2024-06-01T10:00:00Z"},
		{"space-separated", "expiry: 2024-06-01 10:00:00\n", "2024-06-01 10:00:00"},
		{"unpadded", "expiry: 2024-6-1\n", "2024-6-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.doc, "-o", "raw", ".expiry")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("got %q, want %q", strings.TrimSpace(got), tc.want)
			}
		})
	}
}

func TestTimestampScalarTypeIsString(t *testing.T) {
	got, err := execute(t, "expiry: 2024-06-01\n", "type", "-o", "raw", ".expiry")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "string" {
		t.Errorf("got %q, want %q", strings.TrimSpace(got), "string")
	}
}

func TestTimestampScalarLenIsRuneCount(t *testing.T) {
	got, err := execute(t, "expiry: 2024-06-01\n", "len", "-o", "raw", ".expiry")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "10" {
		t.Errorf("got %q, want %q (len of \"2024-06-01\")", strings.TrimSpace(got), "10")
	}
}

func TestTimestampJSONRendersSourceText(t *testing.T) {
	got, err := execute(t, "expiry: 2024-06-01\n", "-o", "json", ".expiry")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != `"2024-06-01"` {
		t.Errorf("got %q, want %q", strings.TrimSpace(got), `"2024-06-01"`)
	}
}

// TestTimestampMappingKeysStayStrings is the key-position half of #321: a
// date-like key used to decode into a time.Time key, collapsing the mapping
// into map[any]any and making keys/--paths print the key as its Go-formatted
// instant — a spelling `set` on that path couldn't resolve back to the real
// key, landing a second, bogus one beside it instead.
func TestTimestampMappingKeysStayStrings(t *testing.T) {
	doc := "releases:\n  2024-06-01: v1\n  2024-06-02: v2\n"
	got, err := execute(t, doc, "keys", "-o", "raw", ".releases")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "2024-06-01\n2024-06-02\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestTimestampDefaultReadsAsGiven is --default's share of #321: it parses
// as YAML the same way a document value does, so a date-shaped default used
// to come back expanded too.
func TestTimestampDefaultReadsAsGiven(t *testing.T) {
	got, err := execute(t, "a: 1\n", "--default", "2024-06-01", "-o", "raw", ".nope")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "2024-06-01" {
		t.Errorf("got %q, want %q", strings.TrimSpace(got), "2024-06-01")
	}
}

// TestTimestampDuplicateKeysStillRefused guards the ordering retagTimestamps
// relies on: rejectEqualKeys must see the original !!timestamp tag so two
// keys that decode to the same instant (same calendar date, midnight) are
// still refused as duplicates, the way an exact repeat already is. Retagging
// before that check would make them two distinct strings and silently accept
// a document get already refuses.
func TestTimestampDuplicateKeysStillRefused(t *testing.T) {
	_, err := execute(t, "2024-06-01: a\n2024-6-1: b\n", ".")
	if err == nil {
		t.Fatal("want a duplicate-key parse error, got none")
	}
	if code := codeFor(err); code != 2 {
		t.Errorf("codeFor = %d, want 2 (parse); err: %v", code, err)
	}
}
