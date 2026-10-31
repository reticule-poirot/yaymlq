package cmd

import (
	"strings"
	"testing"
)

// TestEditKeepsANullInAFlowCollection is #263. yaml.v3's encoder writes an
// empty null as "”" in flow context — a changed value, and the one failure
// this layer exists to prevent. The cases here are the ones #260's restore
// declines on purpose, which is all that is left of it.
func TestEditKeepsANullInAFlowCollection(t *testing.T) {
	for _, in := range []string{
		"f: [&x]\nafter: 1\n",
		"f: {a: &y}\nafter: 1\n",
		"f: [1, &x, 2]\nafter: 1\n",
		"f: {a: 1, b: &y}\nafter: 1\n",
	} {
		t.Run(in, func(t *testing.T) {
			before, err := execute(t, in, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read before: %v", err)
			}
			edited, err := execute(t, in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			after, err := execute(t, edited, "-o", "json", ".f")
			if err != nil {
				t.Fatalf("read after %q: %v", edited, err)
			}
			if before != after {
				t.Errorf("editing .after changed .f\n--- out\n%q\n before %s  after %s", edited, before, after)
			}
			if strings.Contains(edited, "''") {
				t.Errorf("a null came back as the empty string: %q", edited)
			}
		})
	}
}

// TestEditStillWritesNothingForABlockNull is the boundary the fix is scoped to.
// In block context the encoder already writes an empty null correctly, and
// "key:" with no value is one of the most common things in a YAML file — so
// normalising there would put "null" on lines all over every document that
// round-trip exactly today.
func TestEditStillWritesNothingForABlockNull(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"mapping value", "f:\n  a:\nafter: 1\n", "f:\n  a:\nafter: 2\n"},
		{"anchored mapping value", "f:\n  a: &z\nafter: 1\n", "f:\n  a: &z\nafter: 2\n"},
		{"explicit null kept", "f:\n  a: null\nafter: 1\n", "f:\n  a: null\nafter: 2\n"},
		{"tilde kept", "f:\n  a: ~\nafter: 1\n", "f:\n  a: ~\nafter: 2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// TestEditKeepsAFlowNullTheRestoreAlreadyCovers checks the fix does not disturb
// the collections #260 puts back from source: those keep the source's own
// spelling, not this normalisation's.
func TestEditKeepsAFlowNullTheRestoreAlreadyCovers(t *testing.T) {
	for _, in := range []string{"f: {a}\nafter: 1\n", "f: {a: }\nafter: 1\n", "f: [1, ]\nafter: 1\n"} {
		want := strings.Replace(in, "after: 1", "after: 2", 1)
		got, err := execute(t, in, "set", ".after", "2")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if got != want {
			t.Errorf("want %q, got %q", want, got)
		}
	}
}
