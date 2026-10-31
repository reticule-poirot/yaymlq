package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditKeepsAQuotedScalarsEscaping is #252's literal-tab row, which is one
// case of a wider fact: yaml.v3 decodes a double-quoted scalar to its text and
// re-escapes that text its own way, so every escape the author chose is
// rewritten. None of these changes a value.
func TestEditKeepsAQuotedScalarsEscaping(t *testing.T) {
	cases := []struct{ name, in string }{
		{"literal tab", "a: \"col1\tcol2\"\nafter: 1\n"},
		{"escaped non-ascii", `a: "caf\u00e9"` + "\nafter: 1\n"},
		{"hex escape", `a: "\x41"` + "\nafter: 1\n"},
		{"several escapes", `a: "\u0041\u0042"` + "\nafter: 1\n"},
		{"escaped nbsp", `a: "\u00a0gap"` + "\nafter: 1\n"},
		{"in a key", `"caf\u00e9": 1` + "\nafter: 1\n"},
		{"in a flow collection", `m: ["caf\u00e9", 2]` + "\nafter: 1\n"},
		{"behind a tag", `a: !!str "caf\u00e9"` + "\nafter: 1\n"},
		{"behind an anchor", `a: &x "caf\u00e9"` + "\nafter: 1\n"},
		{"in a sequence", `s:` + "\n" + `  - "caf\u00e9"` + "\nafter: 1\n"},
		{"with a trailing comment", `a: "caf\u00e9" # note` + "\nafter: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("an edit to .after re-escaped the scalar\n--- want\n%q\n--- got\n%q", want, got)
			}
		})
	}
}

// TestEditKeepsAQuotedScalarsValue is the half that matters more than the
// spelling: whatever the restore does to the text, the value a reader gets back
// is the value that was there.
func TestEditKeepsAQuotedScalarsValue(t *testing.T) {
	for _, in := range []string{
		"a: \"col1\tcol2\"\nafter: 1\n",
		`a: "caf\u00e9"` + "\nafter: 1\n",
		`a: "\x41"` + "\nafter: 1\n",
	} {
		before, err := execute(t, in, "-o", "json", ".a")
		if err != nil {
			t.Fatalf("read before: %v", err)
		}
		edited, err := execute(t, in, "set", ".after", "2")
		if err != nil {
			t.Fatalf("edit: %v", err)
		}
		after, err := execute(t, edited, "-o", "json", ".a")
		if err != nil {
			t.Fatalf("read after %q: %v", edited, err)
		}
		if before != after {
			t.Errorf("editing .after changed .a in %q\n before %s  after %s", in, before, after)
		}
	}
}

// TestEditLeavesAnAlreadyCanonicalScalarAlone is the gate that keeps this fixup
// off the scalars it has nothing to say about: a scalar the source spelled the
// encoder's way is never recorded, so the overwhelming majority of a document's
// scalars never go near the stand-in marker.
func TestEditLeavesAnAlreadyCanonicalScalarAlone(t *testing.T) {
	cases := []struct{ name, in string }{
		{"plain", "a: 1.0\nafter: 1\n"},
		{"single quoted", "a: 'it''s'\nafter: 1\n"},
		{"double quoted, no escapes", `a: "plain"` + "\nafter: 1\n"},
		{"escape yaml.v3 writes too", `a: "line\nbreak"` + "\nafter: 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Replace(tc.in, "after: 1", "after: 2", 1)
			got, err := execute(t, tc.in, "set", ".after", "2")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != want {
				t.Errorf("want %q, got %q", want, got)
			}
		})
	}
}

// TestEditingAQuotedScalarItselfRewritesIt is the rule the rest of the layer
// follows: the scalar the edit replaced is the caller's, spelled the way the
// encoder spells the caller's value.
func TestEditingAQuotedScalarItselfRewritesIt(t *testing.T) {
	got, err := execute(t, `a: "caf\u00e9"`+"\nb: 1\n", "set", "--string", ".a", "x")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "a: x\nb: 1\n"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditLeavesAQuotedScalarItCannotReadToTheEncoder pins the fail-safe side.
// A quoted scalar the source broke across lines has no single line to read the
// spelling off, so it is left to the encoder — which is what shipped before this
// fixup existed.
func TestEditLeavesAQuotedScalarItCannotReadToTheEncoder(t *testing.T) {
	got, err := execute(t, "a: \"one\n  caf\\u00e9\"\nafter: 1\n", "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, quotedMarkerCore) {
		t.Fatalf("a stand-in marker reached the output: %q", got)
	}
	if _, err := execute(t, got, "-o", "json", ".a"); err != nil {
		t.Fatalf("the edited document no longer reads: %q\n%v", got, err)
	}
}

// TestEditKeepsAQuotedScalarBesideItsOwnMarkerText puts the stand-in marker's
// own text in the document, so the restore has to tell a marker it issued from
// one the author wrote.
func TestEditKeepsAQuotedScalarBesideItsOwnMarkerText(t *testing.T) {
	in := "note: " + quotedMarkerCore + "0\n" + `a: "caf\u00e9"` + "\nafter: 1\n"
	want := strings.Replace(in, "after: 1", "after: 2", 1)
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditKeepsAQuotedScalarUnderCRLF checks the restore lands before the line
// endings are reapplied.
func TestEditKeepsAQuotedScalarUnderCRLF(t *testing.T) {
	in := "a: \"col1\tcol2\"\r\nafter: 1\r\n"
	want := "a: \"col1\tcol2\"\r\nafter: 2\r\n"
	got, err := execute(t, in, "set", ".after", "2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEditInPlaceIsIdempotentWithAQuotedScalar is the property the re-escaping
// costs: the second run of the same edit has a different file to work on.
func TestEditInPlaceIsIdempotentWithAQuotedScalar(t *testing.T) {
	in := `a: "caf\u00e9"` + "\nb: \"col1\tcol2\"\nafter: 1\n"
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

// TestQuoteStartRejectsWhatIsNotAQuotedScalar covers the scan from a node's
// reported column to its opening quote: only the properties yaml.v3 counts as
// part of the node may stand between, since anything further along is a quote
// belonging to something else.
func TestQuoteStartRejectsWhatIsNotAQuotedScalar(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		col        int
	}{
		{"no quote at all", "a: plain", 4},
		{"column past the line", `a: "x"`, 99},
		{"column before the line", `a: "x"`, 0},
		{"text before the quote", `a: x "y"`, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := quoteStart(tc.line, tc.col); ok {
				t.Errorf("quoteStart(%q, %d) accepted a line that opens no quoted scalar", tc.line, tc.col)
			}
		})
	}
	for _, tc := range []struct {
		name, line string
		col, want  int
	}{
		{"quote at the column", `a: "x"`, 4, 3},
		{"past an anchor", `a: &n "x"`, 4, 6},
		{"past a tag", `a: !!str "x"`, 4, 9},
		{"single quote", `a: 'x'`, 4, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := quoteStart(tc.line, tc.col)
			if !ok || got != tc.want {
				t.Errorf("quoteStart(%q, %d) = %d, %v; want %d, true", tc.line, tc.col, got, ok, tc.want)
			}
		})
	}
}

// TestQuotedReproducesRefusesARecordThatDoesNotParseBack is the check that
// stands between a misread spelling and a different string in the user's file.
func TestQuotedReproducesRefusesARecordThatDoesNotParseBack(t *testing.T) {
	for _, tc := range []struct {
		text, value string
		want        bool
	}{
		{`"a"`, "a", true},
		{`'a'`, "a", true},
		{`"\x41"`, "A", true},
		{`"a"`, "b", false},
		{`"a`, "a", false},    // a truncated spelling
		{`"\q"`, "", false},   // not a YAML escape at all
		{`"a" ]`, "a", false}, // text past the scalar's end
	} {
		if got := quotedReproduces(tc.text, tc.value); got != tc.want {
			t.Errorf("quotedReproduces(%q, %q) = %v, want %v", tc.text, tc.value, got, tc.want)
		}
	}
}
