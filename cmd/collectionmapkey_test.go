package cmd

import (
	"strings"
	"testing"
)

// TestAliasedCollectionKeyIsRefusedLikeACollectionKey: an alias to a mapping or
// sequence is the same key as the collection it names. Reads either surfaced
// Go syntax or panicked, the latter when a merge hashed the key.
func TestAliasedCollectionKeyIsRefusedLikeACollectionKey(t *testing.T) {
	const want = "a mapping or sequence used as a mapping key is not supported"
	inputs := []struct{ name, src string }{
		{"sequence", "x: &k [1]\ny:\n  *k : 1\n"},
		{"mapping", "x: &k {a: 1}\ny:\n  *k : 1\n"},
		{"inside a merge", "x: &k [1]\ny:\n  1: 2\n  <<: {*k : 1}\n"},
	}
	verbs := [][]string{
		{"."},
		{"keys", "."},
		{"validate", "-"},
		{"set", ".x", "1"},
		{"delete", ".x"},
		{"rename", ".x", "z"},
		{"append", ".x", "1"},
	}
	for _, in := range inputs {
		for _, args := range verbs {
			t.Run(in.name+"/"+args[0], func(t *testing.T) {
				out, err := execute(t, in.src, args...)
				if err == nil {
					t.Fatalf("want an error, got nil\n%s", out)
				}
				msg := out + err.Error()
				if !strings.Contains(msg, want) {
					t.Errorf("got %q, want it to contain %q", msg, want)
				}
				if strings.Contains(msg, "interface") {
					t.Errorf("error leaks Go syntax: %q", msg)
				}
			})
		}
	}
}

func TestAliasedScalarKeyStillWorks(t *testing.T) {
	got, err := execute(t, "x: &k a\ny:\n  *k : 1\n", ".y.a")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, got)
	}
	if got != "1\n" {
		t.Errorf("got %q, want %q", got, "1\n")
	}
}

// TestCollectionMappingKeyErrorIsConsistent is #355: a mapping or sequence
// used as a mapping key (`? [a, b]\n: 1\n`) is refused by every editing
// verb, through rejectMappingKeys/describeMappingKey, with a message that
// doesn't mention Go at all — "a mapping or sequence used as a mapping key
// is not supported". get/keys/len/type decoded the same input their own
// way, straight into `any` without that check, and surfaced yaml.v3's raw
// panic-recovery text instead ("invalid map key: []interface {}{...}").
// Both halves of the CLI now refuse the same input with the same message.
func TestCollectionMappingKeyErrorIsConsistent(t *testing.T) {
	const src = "? [a, b]\n: 1\n"
	const want = "a mapping or sequence used as a mapping key is not supported"

	cases := []struct {
		name string
		args []string
	}{
		{"get", []string{"."}},
		{"keys", []string{"keys", "."}},
		{"len", []string{"len", "."}},
		{"type", []string{"type", "."}},
		{"set", []string{"set", ".x", "1"}},
		{"delete", []string{"delete", ".x"}},
		{"rename", []string{"rename", ".x", "y"}},
		{"append", []string{"append", ".x", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, src, tc.args...)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			msg := err.Error()
			if !strings.Contains(msg, want) {
				t.Errorf("%v: got %q, want it to contain %q", tc.args, msg, want)
			}
			if strings.Contains(msg, "interface") || strings.Contains(msg, "[]interface") {
				t.Errorf("%v: error leaks Go syntax: %q", tc.args, msg)
			}
			if codeFor(err) != 2 {
				t.Errorf("%v: codeFor = %d, want 2 (parse)", tc.args, codeFor(err))
			}
		})
	}
}
