package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
