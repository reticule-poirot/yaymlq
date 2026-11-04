package cmd

import "testing"

// TestNoOpEditKeepsABlankLineWithNoNodeBelow guards #270's trailing-blank row
// and the blank beside a document marker from #259: blank lines with no node
// below them — at the end of the stream, beside a "---" or a "...". The
// splice copies them, runs included.
func TestNoOpEditKeepsABlankLineWithNoNodeBelow(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"at end of file", "nop: 1\nlast: 1\n\n", ""},
		{"a run at end of file", "nop: 1\nlast: 1\n\n\n", "nop: 1\nlast: 1\n\n\n"},
		{"after a foot comment at end of file", "nop: 1\n# foot\n\n", ""},
		{"around a foot comment at end of file", "nop: 1\n\n# foot\n\n", ""},
		{"after a nested foot comment", "nop: 1\nm:\n  a: 1\n  # fb\n\n", ""},
		{"before ---", "nop: 1\n\n---\nb: 2\n", ""},
		{"before the first ---", "\n---\nnop: 1\n", ""},
		{"between a head comment and ---", "# h\n\n---\nnop: 1\n", ""},
		{"between --- and a node below a head comment", "# h\n---\n\nnop: 1\n", ""},
		{"after a foot comment before ---", "nop: 1\n# foot\n\n---\nb: 2\n", ""},
		{"before ...", "---\nnop: 1\n\n...\n", ""},
		{"after ...", "nop: 1\n...\n\n---\nb: 2\n", ""},
		{"after ... at end of file", "nop: 1\n...\n\n", ""},
		{"all of them", "nop: 1\n\n...\n\n---\n\nb: 2\n\n...\n\n", ""},
		{"CRLF", "nop: 1\r\n\r\n---\r\nb: 2\r\n\r\n", ""},
		{"bare CR", "nop: 1\r\rlast: 1\r\r", ""},
		{"an empty document is not doubled", "nop: 1\n---\n\n---\nc: 3\n", ""},
		{"keep chomping is not doubled", "nop: 1\na: |+\n  x\n\n", ""},
		{"keep chomping before ... is not doubled", "---\nnop: 1\na: |+\n  x\n\n...\n", ""},
		{"own-line keep header is not doubled", "nop: 1\na:\n  |+\n  x\n\n", "nop: 1\na:\n  |+\n  x\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == "" {
				want = tc.in
			}
			got, err := execute(t, tc.in, "rename", ".nop", "nop")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Fatalf("want %q\ngot  %q", want, got)
			}
			again, err := execute(t, got, "rename", ".nop", "nop")
			if err != nil {
				t.Fatalf("second run: %v", err)
			}
			if again != got {
				t.Fatalf("not idempotent:\nfirst  %q\nsecond %q", got, again)
			}
		})
	}
}

// TestEditKeepsTheEndOfFileBlankAtTheEnd checks the blank belongs to the end
// of the stream, not to the node above it: an edit that adds or removes the
// last node leaves it where it was, and one that replaces a keep-chomping
// block takes the block's own trailing blanks with it, since those were value.
func TestEditKeepsTheEndOfFileBlankAtTheEnd(t *testing.T) {
	cases := []struct {
		name, in string
		args     []string
		want     string
	}{
		{"set a new last key", "nop: 1\nlast: 1\n\n", []string{"set", ".new", "1"}, "nop: 1\nlast: 1\nnew: 1\n\n"},
		{"delete the last key", "nop: 1\nlast: 1\n\n", []string{"delete", ".last"}, "nop: 1\n\n"},
		{"replace a keep block", "nop: 1\na: |+\n  x\n\n", []string{"set", ".a", "1"}, "nop: 1\na: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q\ngot  %q", tc.want, got)
			}
		})
	}
}

// TestEditDoesNotTurnAnEndOfFileBlankIntoValue checks the restore never adds a
// line a block brought in by the edit would read as its own: under keep
// chomping a trailing blank line is content.
func TestEditDoesNotTurnAnEndOfFileBlankIntoValue(t *testing.T) {
	for _, value := range []string{`"x\n"`, `"x\n\n"`, `"x"`} {
		t.Run(value, func(t *testing.T) {
			want, err := execute(t, "v: "+value+"\n", "-o", "json", ".v")
			if err != nil {
				t.Fatal(err)
			}
			edited, err := execute(t, "nop: 1\nlast: 1\n\n", "set", ".last", value)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			got, err := execute(t, edited, "-o", "json", ".last")
			if err != nil {
				t.Fatalf("read back: %v\n%q", err, edited)
			}
			if got != want {
				t.Fatalf("want %s got %s\n%q", want, got, edited)
			}
		})
	}
}
