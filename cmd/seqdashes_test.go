package cmd

import "testing"

// A blank line above a block sequence item is carried as a "\n" head comment
// (blanklines.go), and the encoder cannot write one on a dash line: it strands
// the dash and pushes the item's body down a line. These check the repair, and
// that it leaves the lone dashes it must not touch alone.
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
			// A run of blank lines collapses to one, which is what this fixup
			// does everywhere; the dash is the part under test.
			name: "a run of blank lines above the item",
			key:  "s",
			src:  "s:\n  - a: 1\n\n\n  - b: 2\n",
			want: "s:\n  - a: 1\n\n  - b: 2\n",
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

func TestRejoinStrandedDashes(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "folds the body up onto the dash",
			out:  "s:\n  - \n    a: 1\n",
			want: "s:\n  - a: 1\n",
		},
		{
			name: "leaves the rest of the body where it is",
			out:  "s:\n  - \n    a: 1\n    b: 2\n",
			want: "s:\n  - a: 1\n    b: 2\n",
		},
		{
			name: "a dash with no trailing space is an empty item",
			out:  "s:\n  -\n    a: 1\n",
			want: "s:\n  -\n    a: 1\n",
		},
		{
			name: "declines when the next line is no deeper",
			out:  "s:\n  - \n  a: 1\n",
			want: "s:\n  - \n  a: 1\n",
		},
		{
			name: "declines when the next line is a comment",
			out:  "s:\n  - \n    # c\n",
			want: "s:\n  - \n    # c\n",
		},
		{
			name: "declines when the next line is blank",
			out:  "s:\n  - \n\n",
			want: "s:\n  - \n\n",
		},
		{
			name: "declines at the end of the stream",
			out:  "s:\n  - ",
			want: "s:\n  - ",
		},
		{
			name: "leaves a document with no stranded dash untouched",
			out:  "a: 1\nb:\n  - c: 2\n",
			want: "a: 1\nb:\n  - c: 2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(rejoinStrandedDashes([]byte(tt.out))); got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}
