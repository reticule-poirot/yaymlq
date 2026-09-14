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
