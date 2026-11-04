package cmd

import (
	"testing"
)

// A blank line above a block sequence item once stranded the dash and pushed
// the item's body down a line. These check the item stays compact, and that a
// lone dash the source wrote stays lone.
func TestEditKeepsABlankSeparatedSequenceItemCompact(t *testing.T) {
	tests := []struct {
		name string
		key  string
		src  string
		want string // empty means "byte-identical to src"
	}{
		{
			name: "mapping item",
			key:  "updates",
			src:  "updates:\n  - package-ecosystem: gomod\n    directory: /\n\n  - package-ecosystem: github-actions\n    directory: /\n",
		},
		{
			name: "several keys below the dash",
			key:  "s",
			src:  "s:\n  - a: 1\n\n  - a: 3\n    b: 4\n    c:\n      d: 5\n",
		},
		{
			name: "sequence item",
			key:  "s",
			src:  "s:\n  - - x\n\n  - - y\n",
		},
		{
			name: "nested three deep",
			key:  "x",
			src:  "x:\n  y:\n    z:\n      - p: 1\n\n      - q: 2\n",
		},
		{
			name: "every item blank-separated",
			key:  "s",
			src:  "s:\n\n  - a: 1\n\n  - b: 2\n\n  - c: 3\n",
		},
		{
			name: "a comment below the blank line",
			key:  "s",
			src:  "s:\n  - a: 1\n\n  # why\n  - b: 2\n",
		},
		{
			name: "a run of blank lines above the item",
			key:  "s",
			src:  "s:\n  - a: 1\n\n\n  - b: 2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.want
			if want == "" {
				want = tt.src
			}
			got, err := execute(t, tt.src, "rename", "."+tt.key, tt.key)
			if err != nil {
				t.Fatalf("rename: %v", err)
			}
			if got != want {
				t.Errorf("a no-op edit did not reproduce the source\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// A dash alone on its line is not always the artifact: an empty sequence item
// is one too, and the encoder writes it the same way but for the trailing
// space. Folding one of those up would swallow the item that follows.
func TestEditLeavesAnEmptySequenceItemAlone(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "empty item above a sibling",
			src:  "s:\n  -\n  - a: 1\n",
			want: "s:\n  -\n  - a: 1\n",
		},
		{
			name: "empty item at the end of the stream",
			src:  "s:\n  - a: 1\n  -\n",
			want: "s:\n  - a: 1\n  -\n",
		},
		{
			name: "empty item above a dedented key",
			src:  "s:\n  -\nafter: 1\n",
			want: "s:\n  -\nafter: 1\n",
		},
		{
			name: "empty item blank-separated from a sibling",
			src:  "s:\n  - a: 1\n\n  -\n",
			want: "s:\n  - a: 1\n\n  -\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := execute(t, tt.src, "rename", ".s", "s")
			if err != nil {
				t.Fatalf("rename: %v", err)
			}
			if got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}

// A dash the source wrote alone on its line, with the item's body below it,
// stays alone: the encoder always compacts it, and the edit did not ask for that.
func TestEditKeepsALoneDashAlone(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"mapping item", "s:\n  -\n    env:\n      - CGO_ENABLED=0\n"},
		{"several items", "s:\n  -\n    a: 1\n    b: 2\n  -\n    c: 3\n"},
		{"sequence item", "s:\n  -\n    - x\n"},
		{"nested lone dashes", "s:\n  -\n    -\n      a: 1\n"},
		{"beside a compact sibling", "s:\n  - a: 1\n  -\n    b: 2\n"},
		{"a blank line above the dash", "s:\n  - a: 1\n\n  -\n    b: 2\n"},
		{"a blank line below the dash", "s:\n  -\n\n    b: 2\n"},
		{"a comment above the dash", "s:\n  # hc\n  -\n    a: 1\n"},
		{"a comment between the dash and the key", "s:\n  -\n    # inner\n    a: 1\n"},
		{"a block scalar first value", "s:\n  -\n    a: |\n      x\n    b: 1\n"},
		{"a flow first value", "s:\n  -\n    a: [1,  2]\n"},
		{"an anchored first key", "s:\n  -\n    &k a: 1\n"},
		{"trailing space after the dash", "s:\n  -   \n    a: 1\n"},
		{"an empty item", "s:\n  -\n  - x\n"},
		{"a root sequence under a header", "# header\n\n-\n  a: 1\n- b: 2\n"},
		{"the marker text already in the source", "s:\n  -\n    a: yaymlqLoneDash\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, want := []string{"set", ".zz", "1"}, tt.src+"zz: 1\n"
			if tt.src[0] == '#' {
				// A root sequence has no mapping to add a key to; a no-op edit
				// on its second item drives it instead.
				args, want = []string{"set", ".[1].b", "2"}, tt.src
			}
			got, err := execute(t, tt.src, args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if got != want {
				t.Errorf("want %q, got %q", want, got)
			}
		})
	}
}

// An edit inside a lone-dash item leaves the dash where it was; one that
// replaces the item is the caller's, and comes out in the encoder's form.
func TestEditOfALoneDashItem(t *testing.T) {
	const src = "s:\n  -\n    a: 1\n    b: 2\n  -\n    c: 3\n"
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"set", ".s[0].b", "9"}, "s:\n  -\n    a: 1\n    b: 9\n  -\n    c: 3\n"},
		{[]string{"delete", ".s[0].a"}, "s:\n  -\n    b: 2\n  -\n    c: 3\n"},
		{[]string{"rename", ".s[0].a", "z"}, "s:\n  -\n    z: 1\n    b: 2\n  -\n    c: 3\n"},
		{[]string{"set", ".s[0]", "5"}, "s:\n  - 5\n  -\n    c: 3\n"},
		{[]string{"append", ".s", "d: 4"}, "s:\n  -\n    a: 1\n    b: 2\n  -\n    c: 3\n  - d: 4\n"},
	}
	for _, tt := range tests {
		t.Run(tt.args[0]+" "+tt.args[1], func(t *testing.T) {
			got, err := execute(t, src, tt.args...)
			if err != nil {
				t.Fatalf("%v: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}
