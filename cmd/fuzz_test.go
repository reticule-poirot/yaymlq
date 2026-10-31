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
	"gopkg.in/yaml.v3"
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
		"a: 1  # a\rb: 2    # b\r",     // bare CR, a line break the fixups
		"a: 1  # a\u0085b: 2    # b\n", // used to miss entirely (#254)
		"a: 1  #\n",                    // empty comment text
		"a: &x 1  # anchor\nb: *x  # alias\n",
		// A comment on a key whose value is an anchored or tagged collection:
		// the decoder moves the comment and the encoder strands the token,
		// which at column 0 is not a document at all (#256).
		"k: &p  # note\n  v: 1\n",
		"k: !tag  # note\n  v: 1\n",
		"k:  # note\n  &p !tag\n  v: 1\n",
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

// FuzzEditBlockScalars checks the property #250 was a counterexample to: an
// edit to one key leaves every other key's value alone. The block scalar is
// built from a fuzzed header and body rather than a whole fuzzed document, so
// the generator spends its budget on the part whose extent has to be read out
// of the raw source — where a misjudged last line splices a truncated block
// over a correct one.
//
// Asserted on the value rather than on the bytes: the layout of an untouched
// block is restored where it can be, but reproducing it is not a promise this
// makes for every shape a header can take (a header on a line of its own is
// deliberately normalised onto its key's line). Changing the value is a
// promise, since a block scalar's trailing line breaks are content.
func FuzzEditBlockScalars(f *testing.F) {
	seeds := []struct{ header, body string }{
		{"|", "  one\n  two\n"},
		{"|+", "  one\n\n\n"},
		{">+", "  one\n  two\n\n"},
		{"|-", "  one\n"},
		{"|2", "    one\n  two\n"},
		{"|2-", "    one\n"},
		{">", "  one\n  two\n  three\n"},
		{"| # why", "  one\n"},
		{"|", "  one\n   \n  two\n"}, // whitespace-only content line
		{"|", ""},                    // empty block
	}
	for _, s := range seeds {
		f.Add(s.header, s.body)
	}

	f.Fuzz(func(t *testing.T, header, body string) {
		// The non-LF line breaks used to be skipped here: every raw-source
		// fixup split on "\n" alone, so a document using one of the other four
		// was read against the wrong lines from that point on (#254). They are
		// counted properly now, and leaving them in the generator's reach is
		// most of what keeps that true.
		//
		// Keep the generator on this target's subject. Without this it drifts
		// into documents that hold no block scalar at all and rediscovers the
		// unrelated constructs #252 catalogues, which says nothing about
		// whether a block scalar survived an edit.
		if header == "" || (header[0] != '|' && header[0] != '>') {
			return
		}
		in := "f: " + header + "\n" + body + "after: 1\n"
		if blockScalarYamlV3CannotWriteBack(t, in) {
			return
		}
		before, err := execute(t, in, "-o", "json", ".f")
		if err != nil {
			return // not a document with a readable .f; nothing to promise
		}
		edited, err := execute(t, in, "set", ".after", "2")
		if err != nil {
			return
		}
		// The restore works by handing the encoder a stand-in marker, so a
		// marker reaching the output would write this package's internals into
		// the user's file — the worst failure available here, and worth an
		// invariant rather than an argument that it cannot happen.
		if strings.Contains(edited, blockMarkerCore) && !strings.Contains(in, blockMarkerCore) {
			t.Fatalf("a block scalar's stand-in marker reached the output\n--- in\n%q\n--- out\n%q", in, edited)
		}
		after, err := execute(t, edited, "-o", "json", ".f")
		if err != nil {
			t.Fatalf("editing .after made .f unreadable\n--- in\n%q\n--- out\n%q\n%v", in, edited, err)
		}
		if before != after {
			t.Fatalf("editing .after changed .f\n--- in\n%q\n--- out\n%q\n before %s  after %s", in, edited, before, after)
		}
	})
}

// blockScalarYamlV3CannotWriteBack reports whether src holds a block scalar
// yaml.v3 cannot write back as it read it, which this target therefore has
// nothing to promise about. Both cases are pre-existing and byte-identical on
// v0.22.1, and both are decided from the decoded tree rather than by pattern,
// so the exclusion says the same thing the code it excludes does.
//
//   - A block scalar used as a mapping key. The encoder writes explicit-key
//     form, which gives the restore no line to splice into (see
//     walkBlockScalars), and when such a key's value opens with a line break
//     two distinct keys can collapse into a duplicate and the output stops
//     parsing. Noted on #252.
//   - A block scalar whose value holds LS or PS. yaml.v3 counts those as
//     lines, so Node.Line advances over them, but it does not normalise them
//     to "\n" inside block content the way it does CR and NEL — so a recorded
//     body can never re-parse to that value, the restore declines, and the
//     encoder cannot render a value opening with a break either (#254).
func blockScalarYamlV3CannotWriteBack(t *testing.T, src string) bool {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(src))
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			return false
		}
		if unwritableBlockScalarIn(&n, false) {
			return true
		}
	}
}

func unwritableBlockScalarIn(n *yaml.Node, isKey bool) bool {
	if n.Kind == yaml.ScalarNode && n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		if isKey || strings.ContainsAny(n.Value, "  ") {
			return true
		}
	}
	for i, c := range n.Content {
		if unwritableBlockScalarIn(c, n.Kind == yaml.MappingNode && i%2 == 0) {
			return true
		}
	}
	return false
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

// FuzzEditDocumentMarkers fuzzes the seam the document-marker restore sits in:
// it rewrites the encoder's own document delimiters, so a misread boundary does
// not lose layout — it moves a key from one document into another, or writes a
// marker line the document below it cannot share. Both are failures that leave
// a file this tool can no longer read, which is what this asserts against.
//
// The generator is given the marker text and the documents it separates rather
// than one free-form stream, so its budget goes on the delimiters instead of on
// rediscovering yaml.v3's own parse errors.
func FuzzEditDocumentMarkers(f *testing.F) {
	seeds := []struct{ head, body, tail string }{
		{"---", "after: 1", "..."},
		{"", "after: 1", "..."},
		{"--- !Config", "after: 1", ""},
		{"--- !", "after: 1", ""},
		{"--- &top", "after: 1", "... # done"},
		{"--- {after: 1}", "", ""},
		{"---", "f: |\n  one\n  two\nafter: 1", "..."},
	}
	for _, s := range seeds {
		f.Add(s.head, s.body, s.tail)
	}

	f.Fuzz(func(t *testing.T, head, body, tail string) {
		// Keep the generator on this target's subject: a stream whose second
		// document is the one being delimited, so both the first document's
		// end marker and the second's start marker are in play.
		in := "first: 0\n"
		if head != "" {
			in += head + "\n"
		}
		in += body + "\n"
		if tail != "" {
			in += tail + "\n"
		}
		want, err := execute(t, in, "-o", "json", "--all-docs", ".first")
		if err != nil {
			return // not a stream with a readable .first; nothing to promise
		}
		edited, err := execute(t, in, "set", ".first", "9")
		if err != nil {
			return
		}
		// The document count is the promise: a marker read as the wrong kind
		// of boundary merges two documents or splits one, and either way the
		// keys of the stream are no longer where they were.
		got, err := execute(t, edited, "-o", "json", "--all-docs", ".first")
		if err != nil {
			t.Fatalf("editing .first made the stream unreadable\n--- in\n%q\n--- out\n%q\n%v", in, edited, err)
		}
		if strings.Count(got, "\n") != strings.Count(want, "\n") {
			t.Fatalf("editing .first changed which documents hold it\n--- in\n%q\n--- out\n%q\n before %s  after %s", in, edited, want, got)
		}
	})
}

// FuzzEditFlowCollections fuzzes the seam the flow-collection restore sits in:
// the extent of a flow collection is read out of the raw source, where a quoted
// bracket, a comment or a scalar carrying "#" can put the closing bracket
// somewhere scanFlowLine does not predict. A misread extent splices truncated
// text over correct output, so the property is the one that fails when it does
// — the collection still reads back as the value it had.
//
// The generator is given the collection's inside rather than a whole document,
// so its budget goes on the bracket content instead of on rediscovering
// yaml.v3's own parse errors.
func FuzzEditFlowCollections(f *testing.F) {
	seeds := []struct{ open, inside string }{
		{"{", " a: 1, b: 2 "},
		{"[", " 1, 2 "},
		{"[", "1,2,"},
		{"[", "\n  1,\n  2,\n"},
		{"{", "\n  a: 1, # one\n  b: 2\n"},
		{"[", ` "]", '[' `},
		{"[", " a#b "},
		{"{", " a: '', b: \"\\\"\" "},
		{"{", ""},
		{"[", " [1], {a: 1} "},
	}
	for _, s := range seeds {
		f.Add(s.open, s.inside)
	}

	f.Fuzz(func(t *testing.T, open, inside string) {
		// Keep the generator on this target's subject: a flow collection as the
		// value of .f, with an unrelated key below it for the edit to land on.
		shut := "}"
		if open == "[" {
			shut = "]"
		} else if open != "{" {
			return
		}
		in := "f: " + open + inside + shut + "\nafter: 1\n"
		if flowYamlV3CannotWriteBack(in) {
			return
		}
		before, err := execute(t, in, "-o", "json", ".f")
		if err != nil {
			return // not a document with a readable .f; nothing to promise
		}
		edited, err := execute(t, in, "set", ".after", "2")
		if err != nil {
			return
		}
		// The restore works by handing the encoder a stand-in marker, so a
		// marker reaching the output would write this package's internals into
		// the user's file — the worst failure available here.
		if strings.Contains(edited, flowMarkerCore) && !strings.Contains(in, flowMarkerCore) {
			t.Fatalf("a flow collection's stand-in marker reached the output\n--- in\n%q\n--- out\n%q", in, edited)
		}
		after, err := execute(t, edited, "-o", "json", ".f")
		if err != nil {
			t.Fatalf("editing .after made .f unreadable\n--- in\n%q\n--- out\n%q\n%v", in, edited, err)
		}
		if before != after {
			t.Fatalf("editing .after changed .f\n--- in\n%q\n--- out\n%q\n before %s  after %s", in, edited, before, after)
		}
	})
}

// flowYamlV3CannotWriteBack reports whether src holds a flow collection yaml.v3
// cannot write back as it read it, which FuzzEditFlowCollections therefore has
// nothing to promise about. The one case is a flow mapping entry whose value the
// source left out — "{a}", decoded as null and re-emitted as "”" — which is a
// changed value on v0.22.1 and on main, before any of this. Decided from the
// decoded tree rather than by pattern, so the exclusion says the same thing the
// code it excludes does.
func flowYamlV3CannotWriteBack(src string) bool {
	dec := yaml.NewDecoder(strings.NewReader(src))
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			return false
		}
		if flowImplicitNull(&n, false) {
			return true
		}
	}
}
