package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditKeepsDocumentMarkers is #252's two marker rows stated as the promise
// the other fixups already make. These are document structure rather than
// spacing: a reader that splits a stream on "---" sees a different number of
// documents after an edit that dropped one, and the edit was to an unrelated
// key.
func TestEditKeepsDocumentMarkers(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"leading start marker", "---\nafter: 1\n"},
		{"end marker", "after: 1\n...\n"},
		{"both markers", "---\nafter: 1\n...\n"},
		{"start marker with a tag", "--- !Config\nafter: 1\n"},
		{"start marker with an anchor", "--- &top\nafter: 1\n"},
		{"end marker mid-stream", "after: 1\n...\n---\nb: 2\n"},
		{"marker on every document", "---\nafter: 1\n...\n---\nb: 2\n...\n"},
		{"no markers at all", "after: 1\n"},
		{"separator only", "after: 1\n---\nb: 2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("an edit to .after rewrote the document markers\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditKeepsAMarkerOffALineItCannotShare is the case FuzzEditBlockScalars
// found against the first version of this fixup, which folded the marker back
// onto the encoder's first line whenever the source had written content on it.
// The non-specific tag "!" is the counterexample: the decoder resolves it away
// and the encoder re-emits nothing, so the line that came up to meet the marker
// was the mapping's own first entry — and "--- after: 1" is not a document
// yaml.v3 reads back, so the edit wrote a file this tool could no longer parse.
//
// The marker still comes back; it just keeps its own line. Asserted by reading
// the result, since the failure was that the output stopped being YAML.
func TestEditKeepsAMarkerOffALineItCannotShare(t *testing.T) {
	cases := []struct{ name, in string }{
		{"non-specific tag", "--- !\nafter: 1\n"},
		{"root flow mapping", "--- {after: 1}\n"},
		{"root scalar", "after: 1\n--- hello\n"},
		{"root sequence with a tag", "after: 1\n--- !L\n- 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if want := strings.Count(tc.in, "---"); strings.Count(got, "---") != want {
				t.Errorf("want %d document-start markers, got %q", want, got)
			}
			if _, err := execute(t, got, "-o", "json", "--all-docs", "."); err != nil {
				t.Fatalf("the edited document no longer parses: %q\n%v", got, err)
			}
		})
	}
}

// TestDocTokenLineAcceptsOnlyProperties pins the predicate the fold is gated
// on, since what it rejects is what keeps the output parseable.
func TestDocTokenLineAcceptsOnlyProperties(t *testing.T) {
	for _, line := range []string{"!Config", "&top", "&top !C", "|", ">-", "|2", "|2-"} {
		if !docTokenLine([]byte(line)) {
			t.Errorf("docTokenLine(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"", "after: 1", "- 1", "{a: 1}", "hello", "!Config null", "|x"} {
		if docTokenLine([]byte(line)) {
			t.Errorf("docTokenLine(%q) = true, want false", line)
		}
	}
}

// TestRestoreDocMarkersDeclinesAnOutputItCannotRead drives the bail directly,
// since nothing the encoder writes reaches it: the restore reads the output's
// document boundaries off the separators the encoder put there, and a count
// that disagrees with the records means the text is not shaped the way it reads
// it. Moving a boundary on a guess is the one failure here that costs a value
// rather than a layout, so the delimiters are left alone instead.
func TestRestoreDocMarkersDeclinesAnOutputItCannotRead(t *testing.T) {
	out := []byte("a: 1\n")
	marks := []docMarker{{start: true}, {start: true}}
	if got := restoreDocMarkers(out, marks); string(got) != string(out) {
		t.Errorf("want the output untouched, got %q", got)
	}
}

// TestEditStillDropsABlankLineBesideADocumentMarker records the gap this fixup
// deliberately leaves open, so the next person reads it as a decision rather
// than an oversight. blanklines.go marks a blank line by prefixing a newline to
// the head comment of the node below it; a marker is not a node and has none,
// so a blank line whose only neighbour below is "..." has nothing to hang off.
// The marker itself comes back, which is the structural half; the spacing
// around it would need blanklines.go to grow a second mechanism.
func TestEditStillDropsABlankLineBesideADocumentMarker(t *testing.T) {
	got, err := execute(t, "---\nafter: 1\n\n...\n", "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "---\nafter: 2\n...\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditKeepsDocumentMarkersUnderCRLF checks the restore happens before the
// line endings are reapplied, which is the ordering every other inserted line
// depends on too.
func TestEditKeepsDocumentMarkersUnderCRLF(t *testing.T) {
	in := "---\r\nafter: 1\r\n...\r\n"
	want := "---\r\nafter: 2\r\n...\r\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditInPlaceIsIdempotentWithDocumentMarkers is the property the markers
// cost when they are dropped: the second run of the same edit has a different
// file to work on than the first.
func TestEditInPlaceIsIdempotentWithDocumentMarkers(t *testing.T) {
	in := "---\nafter: 1\n...\n---\nb: 2\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for i := range 2 {
		if _, err := execute(t, "", "set", "-i", ".after", "2", path); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: a path this test just created
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	if string(got) != want {
		t.Errorf("two runs of the same edit\n--- want\n%q\n--- got\n%q", want, string(got))
	}
}

// TestEditKeepsABlockScalarBesideDocumentMarkers holds the two restores that
// both splice lines into the encoder's output against each other: the marker
// scan must not read a block's body lines as documents, and the block restore
// must not consume the marker.
func TestEditKeepsABlockScalarBesideDocumentMarkers(t *testing.T) {
	in := "---\nf: |\n  one\n  two\nafter: 1\n...\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestDeletingTheOnlyKeyKeepsItsDocumentMarkers is the edit that empties a
// document: the markers belong to the document, not to any key in it.
func TestDeletingTheOnlyKeyKeepsItsDocumentMarkers(t *testing.T) {
	got, err := execute(t, "---\na: 1\n...\n", "delete", ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "---\n{}\n...\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}
