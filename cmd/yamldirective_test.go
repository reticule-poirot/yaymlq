package cmd

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"go.yaml.in/yaml/v4"
)

// TestYAML12DirectiveIsRead: §6.8.1 says a 1.2 processor must accept it, and it
// is no reason to say anything, so the output is compared whole.
func TestYAML12DirectiveIsRead(t *testing.T) {
	for name, src := range map[string]string{
		"plain":              "%YAML 1.2\n---\na: 1\n",
		"spaces":             "%YAML   1.2\n---\na: 1\n",
		"tab":                "%YAML\t1.2\n---\na: 1\n",
		"comment after":      "%YAML 1.2 # why\n---\na: 1\n",
		"comment above":      "# why\n\n%YAML 1.2\n---\na: 1\n",
		"beside a tag":       "%TAG ! tag:x:\n%YAML 1.2\n---\na: 1\n",
		"crlf":               "%YAML 1.2\r\n---\r\na: 1\r\n",
		"byte order mark":    "\xef\xbb\xbf%YAML 1.2\n---\na: 1\n",
		"1.1 still":          "%YAML 1.1\n---\na: 1\n",
		"leading zero of 1":  "%YAML 1.01\n---\na: 1\n",
		"root on the marker": "%YAML 1.2\n--- {a: 1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, src, "-o", "raw", ".a")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != "1\n" {
				t.Errorf("got %q, want %q", got, "1\n")
			}
			if _, err := execute(t, src, "validate"); err != nil {
				t.Errorf("validate: %v", err)
			}
		})
	}
	t.Run("every document may have its own", func(t *testing.T) {
		src := "%YAML 1.2\n---\na: 1\n...\n%YAML 1.1\n---\na: 2\n...\n# c\n%YAML 1.2\n---\na: 3\n"
		got, err := execute(t, src, "-o", "raw", "--all-docs", ".a")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if got != "1\n2\n3\n" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("utf-16", func(t *testing.T) {
		var b []byte
		b = binary.LittleEndian.AppendUint16(b, 0xfeff)
		for _, u := range utf16.Encode([]rune("%YAML 1.2\n---\na: 1\n")) {
			b = binary.LittleEndian.AppendUint16(b, u)
		}
		for _, args := range [][]string{{"-o", "raw", ".a"}, {"set", ".a", "1"}} {
			if _, err := execute(t, string(b), args...); err != nil {
				t.Errorf("%v: %v", args, err)
			}
		}
	})
}

// TestEditKeepsTheDirectiveAsWritten: the parser may be told something else, but
// what goes back to the file is the source's.
func TestEditKeepsTheDirectiveAsWritten(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		args []string
		want string
	}{
		"set": {
			"%YAML 1.2\n---\na: 1\n", []string{"set", ".a", "2"},
			"%YAML 1.2\n---\na: 2\n",
		},
		"spacing and comment": {
			"%YAML   1.2  # why\n---\na: 1\n", []string{"set", ".a", "2"},
			"%YAML   1.2  # why\n---\na: 2\n",
		},
		"delete the last key": {
			"%YAML 1.2\n---\na: 1\n", []string{"delete", ".a"},
			"%YAML 1.2\n---\n{}\n",
		},
		"second document": {
			"a: 1\n...\n%YAML 1.2\n---\na: 1\n", []string{"set", "--doc", "1", ".a", "2"},
			"a: 1\n...\n%YAML 1.2\n---\na: 2\n",
		},
		"crlf": {
			"%YAML 1.2\r\n---\r\na: 1\r\n", []string{"set", ".a", "2"},
			"%YAML 1.2\r\n---\r\na: 2\r\n",
		},
		"flow rendering": {
			"%YAML 1.2\n---\na: [1, 2]\n", []string{"append", ".a", "3"},
			"%YAML 1.2\n---\na: [1, 2, 3]\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, tc.src, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("forced re-render of the document", func(t *testing.T) {
		forceRerender = func(n *yaml.Node) bool { return n.Kind == yaml.DocumentNode }
		defer func() { forceRerender = nil }()
		got, err := execute(t, "%YAML 1.2\n---\na:  1\nb: 2\n", "set", ".b", "3")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if want := "%YAML 1.2\n---\na: 1\nb: 3\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("in place", func(t *testing.T) {
		f := writeTemp(t, "%YAML 1.2\n---\na: 1\n")
		if _, err := execute(t, "", "set", "-i", ".a", "2", f); err != nil {
			t.Fatalf("execute: %v", err)
		}
		got, err := os.ReadFile(f) //nolint:gosec // G304: this test's own temp file
		if err != nil {
			t.Fatal(err)
		}
		if want := "%YAML 1.2\n---\na: 2\n"; string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// TestNewerYAMLMinorVersionIsReadWithANote: §6.8.1, a higher minor version
// "should be processed with an appropriate warning". The note goes to stderr, so
// stdout stays what the read found (execute merges the two, so this asserts on
// the note and on the value, not the whole).
func TestNewerYAMLMinorVersionIsReadWithANote(t *testing.T) {
	const noteText = "note: line "
	for name, tc := range map[string]struct {
		src     string
		args    []string
		version string
		value   string
	}{
		"read":            {"%YAML 1.3\n---\na: 1\n", []string{"-o", "raw", ".a"}, "1.3", "1\n"},
		"two digits":      {"%YAML 1.10\n---\na: 1\n", []string{"-o", "raw", ".a"}, "1.10", "1\n"},
		"keys":            {"%YAML 1.3\n---\na: 1\n", []string{"keys", "."}, "1.3", "a\n"},
		"edit":            {"%YAML 1.3\n---\na: 1\n", []string{"set", ".a", "2"}, "1.3", "%YAML 1.3\n---\na: 2\n"},
		"second document": {"a: 1\n...\n%YAML 1.3\n---\na: 1\n", []string{"-o", "raw", "--doc", "1", ".a"}, "1.3", "1\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, tc.src, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !strings.Contains(got, noteText) || !strings.Contains(got, "%YAML "+tc.version) {
				t.Errorf("want a note naming %%YAML %s, got %q", tc.version, got)
			}
			if !strings.Contains(got, tc.value) {
				t.Errorf("want %q in %q", tc.value, got)
			}
		})
	}
	t.Run("validate", func(t *testing.T) {
		got, err := execute(t, "%YAML 1.3\n---\na: 1\n", "validate")
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !strings.Contains(got, noteText) {
			t.Errorf("want a note, got %q", got)
		}
	})
	t.Run("-q stays silent", func(t *testing.T) {
		got, err := execute(t, "%YAML 1.3\n---\na: 1\n", "-q", ".a")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want nothing", got)
		}
	})
	t.Run("one note per run", func(t *testing.T) {
		got, err := execute(t, "%YAML 1.3\n---\na: 1\n...\n%YAML 1.4\n---\na: 2\n", "-o", "raw", "--all-docs", ".a")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if n := strings.Count(got, noteText); n != 1 {
			t.Errorf("got %d notes in %q, want 1", n, got)
		}
	})
}

// TestUnsupportedYAMLVersionNamesTheRightLine: every directive of a stream is
// checked, so a refusal has to name the one that caused it.
func TestUnsupportedYAMLVersionNamesTheRightLine(t *testing.T) {
	for name, tc := range map[string]struct{ src, line, version string }{
		"first":                {"%YAML 2.0\n---\na: 1\n", "line 1", "2.0"},
		"after a 1.2":          {"%YAML 1.2\n---\na: 1\n...\n%YAML 2.0\n---\na: 2\n", "line 5", "2.0"},
		"after a 1.1":          {"%YAML 1.1\n---\na: 1\n...\n%YAML 3.1\n---\na: 2\n", "line 5", "3.1"},
		"after a bare comment": {"# c\n%YAML 0.9\n---\na: 1\n", "line 2", "0.9"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := execute(t, tc.src, "--all-docs", ".a")
			if err == nil {
				t.Fatal("want a refusal, got nil")
			}
			if codeFor(err) != 2 {
				t.Errorf("codeFor = %d, want 2 (parse); err = %v", codeFor(err), err)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.line) || !strings.Contains(msg, "%YAML "+tc.version) {
				t.Errorf("want %s and %%YAML %s named, got %q", tc.line, tc.version, msg)
			}
		})
	}
}

// TestDirectiveMisuseIsStillRefused: telling the parser 1.1 must not lose what it
// checks about a directive's placement.
func TestDirectiveMisuseIsStillRefused(t *testing.T) {
	for name, src := range map[string]string{
		"duplicate":             "%YAML 1.2\n%YAML 1.2\n---\na: 1\n",
		"duplicate, mixed":      "%YAML 1.2\n%YAML 1.1\n---\na: 1\n",
		"no marker after it":    "%YAML 1.2\na: 1\n",
		"no document end":       "a: 1\n%YAML 1.2\n---\nb: 1\n",
		"indented":              " %YAML 1.2\n---\na: 1\n",
		"junk after version":    "%YAML 1.2x\n---\na: 1\n",
		"two versions":          "%YAML 1.2 1.2\n---\na: 1\n",
		"only the directive":    "%YAML 1.2\n",
		"marker in a quoted":    "a: \"x\n...\n%YAML 1.2\n  y\"\n",
		"swallowed by a scalar": "--- |\n...\n%YAML 1.2\n---\nb: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, src, "--all-docs", ".")
			if err == nil {
				t.Fatalf("want a refusal, got %q", got)
			}
			if codeFor(err) != 2 {
				t.Errorf("codeFor = %d, want 2 (parse); err = %v", codeFor(err), err)
			}
		})
	}
}

// TestDirectiveTextInContentIsLeftAlone: a line spelled like a directive is one
// only at the start of the stream or after "...". Past "---" it is content, and
// the rewrite the parser needs would change a value if it reached one.
func TestDirectiveTextInContentIsLeftAlone(t *testing.T) {
	for name, tc := range map[string]struct{ src, path, want string }{
		"literal root":      {"--- |\n%YAML 1.2\n", ".", "%YAML 1.2\n"},
		"literal, indented": {"--- |\n  %YAML 1.2\n", ".", "%YAML 1.2\n"},
		"plain continued":   {"--- a\n%YAML 1.2\n", ".", "a %YAML 1.2\n"},
		"plain under a key": {"a: x\n  %YAML 1.2\n", ".a", "x %YAML 1.2\n"},
		"double-quoted":     {"a: \"x\n%YAML 1.2\n  y\"\n", ".a", "x %YAML 1.2 y\n"},
		"literal under key": {"a: |\n  %YAML 1.2\n", ".a", "%YAML 1.2\n"},
		"second document":   {"%YAML 1.2\n--- |\n%YAML 1.2\n", ".", "%YAML 1.2\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, tc.src, "-o", "raw", tc.path)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if strings.TrimRight(got, "\n") != strings.TrimRight(tc.want, "\n") {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("an edit keeps it", func(t *testing.T) {
		in := "a: |\n  %YAML 1.2\nb: 1\n"
		got, err := execute(t, in, "set", ".b", "2")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if want := "a: |\n  %YAML 1.2\nb: 2\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
