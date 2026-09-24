package cmd

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/reticule-poirot/yaymlq/internal/editscript"
	"github.com/reticule-poirot/yaymlq/internal/path"
)

// FuzzCLI drives the root command (flag parsing, input decoding, query, and
// render) end to end. internal/path and internal/query are already fuzzed on
// their own, so this exists to catch anything specific to the cmd layer
// itself — decodeDocs, render.go's output formatting, the --default/-e
// plumbing — that per-package fuzzing can't see.
func FuzzCLI(f *testing.F) {
	seeds := []struct{ expr, stdin string }{
		{".a.b", "a:\n  b: 1\n"},
		{"a[0]", "a: [1, 2, 3]\n"},
		{"a.*", "a: {x: 1, y: 2}\n"},
		{"", "a: 1\n"},
		{"a[", "a: 1\n"},
		{".a", "a: &x [*x]\n"}, // alias-bomb shape: must be rejected, not panic
		{".missing", "a: 1\n"},
	}
	for _, s := range seeds {
		f.Add(s.expr, s.stdin)
	}

	f.Fuzz(func(_ *testing.T, expr, stdin string) {
		c := NewRootCommand()
		var out bytes.Buffer
		c.SetIn(strings.NewReader(stdin))
		c.SetOut(&out)
		c.SetErr(&out)
		c.SetArgs([]string{expr})

		// Errors are expected (bad paths, malformed YAML, ...); only a panic
		// fails the fuzz run.
		_ = c.Execute()
	})
}

// FuzzDiff checks myersDiff's strongest correctness property — replaying its
// edit script against a must reproduce b exactly, for any two byte strings —
// and that unifiedDiff never panics rendering whatever script that produces.
func FuzzDiff(f *testing.F) {
	seeds := []struct{ a, b string }{
		{"", ""},
		{"a: 1\n", "a: 1\n"},
		{"a: 1\nb: 2\n", "a: 1\nb: 9\n"},
		{"a: 1\n", "a: 1\nb: 2\n"},
		{"a: 1\nb: 2\n", "a: 1\n"},
		{"a: 1", "a: 1\n"},      // trailing-newline mismatch
		{"a\na\na\n", "a\na\n"}, // repeated lines
	}
	for _, s := range seeds {
		f.Add(s.a, s.b)
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		aLines, aNL := splitLines([]byte(a))
		bLines, bNL := splitLines([]byte(b))

		ops := myersDiff(aLines, bLines)
		if got := applyOps(aLines, ops); !equalSlices(got, bLines) {
			t.Fatalf("applying diff(%q, %q) = %v, want %v", a, b, got, bLines)
		}

		// Must not panic, and must produce empty output exactly when there
		// is no byte-level difference at all — that includes a's and b's
		// lines matching exactly but disagreeing on a trailing newline
		// (splitTrailingNewlineChange's job), not just the raw line-level
		// edit script from myersDiff.
		hasChange := aNL != bNL
		if !hasChange {
			for _, op := range ops {
				if op.kind != opSame {
					hasChange = true
					break
				}
			}
		}
		diff := unifiedDiff("f", []byte(a), []byte(b))
		if (diff == "") == hasChange {
			t.Fatalf("unifiedDiff(%q, %q) empty=%v but hasChange=%v", a, b, diff == "", hasChange)
		}

		dj := unifiedDiffJSON("f", []byte(a), []byte(b))
		if dj.Changed != hasChange {
			t.Fatalf("unifiedDiffJSON(%q, %q).Changed=%v but hasChange=%v", a, b, dj.Changed, hasChange)
		}
		if dj.Hunks == nil {
			t.Fatalf("unifiedDiffJSON(%q, %q).Hunks must never be nil", a, b)
		}
		if _, err := json.Marshal(dj); err != nil {
			t.Fatalf("json.Marshal(unifiedDiffJSON(%q, %q)): %v", a, b, err)
		}
	})
}

// FuzzEditCommentGutters drives `set` end to end (flag parsing, decode,
// recordCommentGutters, mutate, encode, widenCommentGutters, CRLF handling)
// against arbitrary stdin — recordCommentGutters/widenCommentGutters run
// unconditionally on every edit, but nothing previously exercised applyEdit
// itself with arbitrary input; FuzzSet (internal/ymledit) only fuzzes the
// tree mutation against one fixed seed document.
func FuzzEditCommentGutters(f *testing.F) {
	seeds := []string{
		"a: 1  # two spaces\n",
		"a: 1    # four spaces\n# standalone\nb: 2\n",
		"a: 1  # a\n---\nb: 2    # b\n",
		"a: 1  # a\r\nb: 2    # b\r\n", // CRLF
		"a: 1  #\n",                    // empty comment text
		"a: &x 1  # anchor\nb: *x  # alias\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(_ *testing.T, stdin string) {
		c := NewRootCommand()
		var out bytes.Buffer
		c.SetIn(strings.NewReader(stdin))
		c.SetOut(&out)
		c.SetErr(&out)
		c.SetArgs([]string{"set", ".a", "9"})

		// Errors are expected (bad YAML, no such path, ...); only a panic
		// fails the fuzz run.
		_ = c.Execute()
	})
}

// FuzzGutterWidth checks gutterWidth/trailingCommentIndex never panic on
// arbitrary input, and that gutterWidth recovers exactly the gutter width a
// line was built with.
func FuzzGutterWidth(f *testing.F) {
	seeds := []struct {
		content string
		n       int
		comment string
	}{
		{"image: v1", 2, "# two spaces"},
		{"", 0, "#"},
		{"a", 10, "# many spaces"},
	}
	for _, s := range seeds {
		f.Add(s.content, s.n, s.comment)
	}

	f.Fuzz(func(t *testing.T, content string, n int, comment string) {
		if n < 0 || n > 200 || comment == "" || comment[0] != '#' ||
			strings.ContainsAny(content, "\n") || strings.ContainsAny(comment, "\n") ||
			strings.HasSuffix(content, " ") {
			return // out of the domain this property covers, not a case to test
		}
		line := content + strings.Repeat(" ", n) + comment

		got, ok := gutterWidth(line, comment)
		if !ok {
			t.Fatalf("gutterWidth(%q, %q): not found", line, comment)
		}
		if got != n {
			t.Fatalf("gutterWidth(%q, %q) = %d, want %d", line, comment, got, n)
		}

		// trailingCommentIndex must not panic on whatever gutterWidth just
		// examined, or on the comment text alone.
		trailingCommentIndex([]byte(line))
		trailingCommentIndex([]byte(comment))
	})
}

// FuzzPathThroughEditScript fuzzes the seam #158 lived in: a path rendered by
// internal/path is read back by internal/editscript, two grammars that know
// nothing about each other. The property is that a one-key path survives the
// trip — `set <path> = v` parses back to the same key — which is what makes
// `yaymlq --paths ... | sed 's/^/set /' | yaymlq apply` safe.
//
// cmd is the right home for it: it is the layer that actually puts the two
// grammars in contact.
func FuzzPathThroughEditScript(f *testing.F) {
	for _, k := range []string{"a", "a = b", "two words", "odd.key", "7", "*", "", "say \"hi\"", "it's", `it's "both"`, "a\nb", `back\slash`, "a[0]", "-1", " padded "} {
		f.Add(k)
	}
	f.Fuzz(func(t *testing.T, key string) {
		// The only precondition left, and not a limitation of either
		// grammar: path.Parse refuses invalid UTF-8 outright, and yaml.v3
		// never produces such a key — a document containing a stray octet
		// fails to parse ("invalid leading UTF-8 octet"), and a "\xf8"
		// escape decodes to a valid rune. Both quote characters in one key,
		// and a key holding a line break, used to be skipped here too;
		// #157's escapes are what let them be asserted instead.
		if !utf8.ValidString(key) {
			return
		}
		want := []path.Segment{{Key: key}}
		line := "set " + path.Format(want) + " = v"

		ops, err := editscript.Parse(strings.NewReader(line + "\n"))
		if err != nil {
			t.Fatalf("key %q rendered as %q, which apply rejects: %v", key, line, err)
		}
		if len(ops) != 1 || ops[0].Value != "v" {
			t.Fatalf("key %q rendered as %q, parsed as %#v", key, line, ops)
		}
		got, err := path.Parse(ops[0].Path)
		if err != nil {
			t.Fatalf("key %q: apply took the path as %q, which doesn't parse: %v", key, ops[0].Path, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("key %q survived as %#v (path text %q in line %q)", key, got, ops[0].Path, line)
		}
	})
}
