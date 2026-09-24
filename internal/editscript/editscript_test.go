package editscript_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/editscript"
)

func TestParse(t *testing.T) {
	script := `
# a comment, and a blank line above/below

set .services.web.image = nginx:1.28
append .services.web.ports = 9090
delete .services.web.environment.DEBUG
rename .services.db = database
`
	got, err := editscript.Parse(strings.NewReader(script))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []editscript.Op{
		{Verb: editscript.Set, Path: ".services.web.image", Value: "nginx:1.28", Line: 4},
		{Verb: editscript.Append, Path: ".services.web.ports", Value: "9090", Line: 5},
		{Verb: editscript.Delete, Path: ".services.web.environment.DEBUG", Line: 6},
		{Verb: editscript.Rename, Path: ".services.db", Value: "database", Line: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseEmptyScript(t *testing.T) {
	got, err := editscript.Parse(strings.NewReader("\n# just a comment\n\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no ops, got %#v", got)
	}
}

func TestParseFlexibleSpacing(t *testing.T) {
	got, err := editscript.Parse(strings.NewReader("set .a=1\nset .b   =   2\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []editscript.Op{
		{Verb: editscript.Set, Path: ".a", Value: "1", Line: 1},
		{Verb: editscript.Set, Path: ".b", Value: "2", Line: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseValueMayContainEquals(t *testing.T) {
	// Only the first "=" is the delimiter; the rest of the line, "=" and
	// all, is the value verbatim.
	got, err := editscript.Parse(strings.NewReader(`set .a = "x=y=z"`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].Value != `"x=y=z"` {
		t.Fatalf("got %#v", got)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, script string
	}{
		{"unknown verb", "frobnicate .a = 1"},
		{"set missing equals", "set .a 1"},
		{"append missing equals", "append .a 1"},
		{"rename missing equals", "rename .a b"},
		{"delete with no path", "delete"},
		{"single word line", "set"},
		{"empty path before equals", "set  = 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := editscript.Parse(strings.NewReader(tc.script)); err == nil {
				t.Fatalf("Parse(%q): want error, got nil", tc.script)
			}
		})
	}
}

func TestParseErrorNamesTheLine(t *testing.T) {
	_, err := editscript.Parse(strings.NewReader("set .a = 1\nbogus line\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want an error naming line 2, got %v", err)
	}
}

func TestParseSyntaxErrorsAreNotErrRead(t *testing.T) {
	// A malformed line is the caller's mistake, not a failure reading the
	// script — the two need to stay distinguishable so a caller (cmd/apply.go)
	// can classify them into different exit codes.
	for _, script := range []string{"bogus line", "set .a 1", "delete"} {
		_, err := editscript.Parse(strings.NewReader(script))
		if err == nil {
			t.Fatalf("Parse(%q): want error, got nil", script)
		}
		if errors.Is(err, editscript.ErrRead) {
			t.Fatalf("Parse(%q): a syntax error should not be ErrRead, got %v", script, err)
		}
	}
}

func TestParseTokenTooLongIsErrRead(t *testing.T) {
	// A single line over the scanner's buffer cap is a read-level failure
	// (bufio.Scanner: token too long), not a syntax mistake in an otherwise
	// readable line.
	huge := "set .a = " + strings.Repeat("x", 2<<20)
	_, err := editscript.Parse(strings.NewReader(huge))
	if !errors.Is(err, editscript.ErrRead) {
		t.Fatalf("want ErrRead for an oversized line, got %v", err)
	}
}

// TestParseSeparatorOutsideQuotes: the "=" that splits path from value is the
// first one the path isn't quoting. Splitting on the first "=" anywhere cut a
// key containing one in half, and the truncated path still parsed — so `set
// "a = b" = new` wrote a new key `a` instead of failing or editing `a = b`.
func TestParseSeparatorOutsideQuotes(t *testing.T) {
	tests := []struct {
		name, line string
		want       editscript.Op
	}{
		{
			"double-quoted key holding the separator",
			`set "a = b" = new`,
			editscript.Op{Verb: editscript.Set, Path: `"a = b"`, Value: "new", Line: 1},
		},
		{
			"single-quoted key holding the separator",
			`append 'x = y'.list = 3`,
			editscript.Op{Verb: editscript.Append, Path: `'x = y'.list`, Value: "3", Line: 1},
		},
		{
			"quoted key holding a bare equals",
			`set "a=b".c = 1`,
			editscript.Op{Verb: editscript.Set, Path: `"a=b".c`, Value: "1", Line: 1},
		},
		{
			"quote inside the value is not a path quote",
			`set .a = "x=y=z"`,
			editscript.Op{Verb: editscript.Set, Path: ".a", Value: `"x=y=z"`, Line: 1},
		},
		{
			"rename to a key holding the separator",
			`rename "a = b" = c`,
			editscript.Op{Verb: editscript.Rename, Path: `"a = b"`, Value: "c", Line: 1},
		},
		{
			"delete takes the whole rest, quotes and all",
			`delete "a = b"`,
			editscript.Op{Verb: editscript.Delete, Path: `"a = b"`, Line: 1},
		},
	}
	for _, tc := range tests {
		got, err := editscript.Parse(strings.NewReader(tc.line + "\n"))
		if err != nil {
			t.Errorf("%s: Parse(%q): %v", tc.name, tc.line, err)
			continue
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
			t.Errorf("%s: Parse(%q) = %#v, want %#v", tc.name, tc.line, got, tc.want)
		}
	}
}

// TestParseUnterminatedQuoteStillReportsTheLine: a quote that never closes
// swallows the separator, and the line has to stay an error rather than
// become one op with a surprising path.
func TestParseUnterminatedQuoteStillReportsTheLine(t *testing.T) {
	_, err := editscript.Parse(strings.NewReader(`set "a = b = new` + "\n"))
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error %q does not name the line", err)
	}
	if errors.Is(err, editscript.ErrRead) {
		t.Errorf("a syntax mistake was classified as a read failure: %v", err)
	}
}

// TestParseSeparatorSkipsEscapedQuotes: the path grammar escapes a quote
// character inside a quoted run, so the scan for the separator has to skip
// what the escape covers. Without that, an escaped quote closes the run
// early here while path.Parse keeps it open, and the two disagree about
// where the path ends — the same class of seam bug as #158, one grammar
// level deeper.
func TestParseSeparatorSkipsEscapedQuotes(t *testing.T) {
	tests := []struct {
		name, line string
		want       editscript.Op
	}{
		{
			"key holding both quote characters",
			`set "it's \"x\"" = v`,
			editscript.Op{Verb: editscript.Set, Path: `"it's \"x\""`, Value: "v", Line: 1},
		},
		{
			"escaped quote before the separator",
			`set "a\" = b" = v`,
			editscript.Op{Verb: editscript.Set, Path: `"a\" = b"`, Value: "v", Line: 1},
		},
		{
			"escaped backslash does not escape the quote after it",
			`set "a\\" = v`,
			editscript.Op{Verb: editscript.Set, Path: `"a\\"`, Value: "v", Line: 1},
		},
		{
			"escaped newline in the key",
			`set "a\nb" = v`,
			editscript.Op{Verb: editscript.Set, Path: `"a\nb"`, Value: "v", Line: 1},
		},
	}
	for _, tc := range tests {
		got, err := editscript.Parse(strings.NewReader(tc.line + "\n"))
		if err != nil {
			t.Errorf("%s: Parse(%q): %v", tc.name, tc.line, err)
			continue
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
			t.Errorf("%s: Parse(%q) = %#v, want %#v", tc.name, tc.line, got, tc.want)
		}
	}
}

// TestParseDocFlag: an op may name the document it applies to, so one script
// can span a multi-document stream — which is what makes a listing from
// `get --paths --all-docs -o json` usable as a script at all (#159).
// Spelled exactly like the CLI flag it mirrors.
func TestParseDocFlag(t *testing.T) {
	tests := []struct {
		name, line string
		want       editscript.Op
	}{
		{
			"space-separated",
			`set --doc 2 .a = 1`,
			editscript.Op{Verb: editscript.Set, Path: ".a", Value: "1", Line: 1, Doc: 2, HasDoc: true},
		},
		{
			"equals-separated",
			`set --doc=2 .a = 1`,
			editscript.Op{Verb: editscript.Set, Path: ".a", Value: "1", Line: 1, Doc: 2, HasDoc: true},
		},
		{
			"on delete, which has no value half",
			`delete --doc 1 .a`,
			editscript.Op{Verb: editscript.Delete, Path: ".a", Line: 1, Doc: 1, HasDoc: true},
		},
		{
			"on rename",
			`rename --doc 1 .a = b`,
			editscript.Op{Verb: editscript.Rename, Path: ".a", Value: "b", Line: 1, Doc: 1, HasDoc: true},
		},
		{
			"zero is not the same as absent",
			`set --doc 0 .a = 1`,
			editscript.Op{Verb: editscript.Set, Path: ".a", Value: "1", Line: 1, Doc: 0, HasDoc: true},
		},
		{
			"absent leaves it unset",
			`set .a = 1`,
			editscript.Op{Verb: editscript.Set, Path: ".a", Value: "1", Line: 1},
		},
		{
			"a path that only starts like the flag is a path",
			`set --docs = 1`,
			editscript.Op{Verb: editscript.Set, Path: "--docs", Value: "1", Line: 1},
		},
		{
			"a quoted path named like the flag is a path",
			`set "--doc" = 1`,
			editscript.Op{Verb: editscript.Set, Path: `"--doc"`, Value: "1", Line: 1},
		},
		{
			// Nothing follows it, so it can't be a selector — and reading it
			// as a path is what the boundary rule already says. The failure
			// a caller sees is "no such key: --doc", which is legible.
			"a bare --doc with no argument is a path",
			`delete --doc`,
			editscript.Op{Verb: editscript.Delete, Path: "--doc", Line: 1},
		},
		{
			"negative index parses; the range check belongs to the caller",
			`delete --doc -1 .a`,
			editscript.Op{Verb: editscript.Delete, Path: ".a", Line: 1, Doc: -1, HasDoc: true},
		},
	}
	for _, tc := range tests {
		got, err := editscript.Parse(strings.NewReader(tc.line + "\n"))
		if err != nil {
			t.Errorf("%s: Parse(%q): %v", tc.name, tc.line, err)
			continue
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
			t.Errorf("%s: Parse(%q) = %#v, want %#v", tc.name, tc.line, got, tc.want)
		}
	}
}

// TestParseDocFlagErrors: a malformed selector must not fall through to being
// read as part of the path.
func TestParseDocFlagErrors(t *testing.T) {
	for _, line := range []string{
		`set --doc x .a = 1`,
		`set --doc= .a = 1`,
		`delete --doc 1`,
		`set --doc 1 = 2`,
	} {
		_, err := editscript.Parse(strings.NewReader(line + "\n"))
		if err == nil {
			t.Errorf("Parse(%q): want an error, got nil", line)
			continue
		}
		if !strings.Contains(err.Error(), "line 1") {
			t.Errorf("Parse(%q): error does not name the line: %v", line, err)
		}
	}
}
