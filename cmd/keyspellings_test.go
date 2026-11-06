package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
)

// TestAPrintedPathEditsTheKeyItCameFrom is #381: a key whose written spelling
// isn't its decoded text printed a path that `set` took as a new key.
func TestAPrintedPathEditsTheKeyItCameFrom(t *testing.T) {
	for _, key := range []string{
		"1.0", "0x10", "1e3", "+1", "1_000", "0o17", "017", "0b11", "1.50", "1e21",
		"~", "Null", "NULL", "TRUE", "True", ".inf", "-.inf", ".nan", "!!binary aGk=",
		"1", "true", "null", "plain", `"1"`, "yes", "2001-01-01", "!!float 1", "!!str 1",
	} {
		t.Run(key, func(t *testing.T) {
			printed, err := execute(t, key+": b\n", "*", "--paths")
			if err != nil {
				t.Fatalf("--paths: %v", err)
			}
			got, err := execute(t, key+": b\n", "set", "--", strings.TrimSpace(printed), "z")
			if err != nil {
				t.Fatalf("set %s: %v", printed, err)
			}
			if want := key + ": z\n"; got != want {
				t.Errorf("set %s: got %q, want %q", printed, got, want)
			}
		})
	}
}

func TestEditsAddressAKeyByEitherSpelling(t *testing.T) {
	stream := "1.0: a\n---\n\"1\": b\n---\nz: c\n"
	for _, tc := range []struct {
		name, in string
		args     []string
		want     string
	}{
		{"delete", "1.0: b\nc: d\n", []string{"delete", `."1"`}, "c: d\n"},
		{"delete null", "~: b\nc: d\n", []string{"delete", ".null"}, "c: d\n"},
		{"rename", "1.0: b\n", []string{"rename", `."1"`, "q"}, "q: b\n"},
		{"rename onto its own read spelling", "1.0: b\n", []string{"rename", `."1.0"`, "1"}, "\"1\": b\n"},
		{"append", "1.0: [a]\n", []string{"append", `."1"`, "c"}, "1.0: [a, c]\n"},
		{"must exist", "1.0: b\n", []string{"set", "--must-exist", `."1"`, "z"}, "1.0: z\n"},
		{"nested", "a:\n  1.0:\n    b: c\n", []string{"set", `.a."1".b`, "z"}, "a:\n  1.0:\n    b: z\n"},
		{"flow set", "m: {1.0: a, ~: b}\n", []string{"set", `.m."1"`, "z"}, "m: {1.0: z, ~: b}\n"},
		{"flow delete", "m: {1.0: a, ~: b}\n", []string{"delete", ".m.null"}, "m: {1.0: a}\n"},

		{"stream must exist", stream, []string{"set", "--all-docs", "--must-exist", `."1"`, "z"}, "1.0: z\n---\n\"1\": z\n---\nz: c\n"},
		{"stream delete", stream, []string{"delete", "--all-docs", `."1"`}, "{}\n---\n{}\n---\nz: c\n"},
		{"stream set", stream, []string{"set", "--all-docs", `."1"`, "z"}, "1.0: z\n---\n\"1\": z\n---\nz: c\n\"1\": z\n"},

		{"written spelling", "1.0: b\n", []string{"set", `."1.0"`, "z"}, "1.0: z\n"},
		{"written null", "~: b\n", []string{"set", `."~"`, "z"}, "~: z\n"},
		{"written beside its twin", "1e3: a\n1000: b\n", []string{"set", `."1e3"`, "z"}, "1e3: z\n1000: b\n"},
		{"written nan beside its twin", ".nan: a\n.NaN: b\n", []string{"set", `.".nan"`, "z"}, ".nan: z\n.NaN: b\n"},
		{"written null beside a string", "~: a\n\"null\": b\n", []string{"set", `."~"`, "z"}, "~: z\n\"null\": b\n"},
		{"rename frees the shared spelling", "1: a\n1.0: b\n", []string{"rename", `."1.0"`, "x"}, "1: a\nx: b\n"},
		{"merge override of the same spelling", "base: &b\n  k: 1\nd:\n  <<: *b\n", []string{"set", ".d.k", "z"}, "base: &b\n  k: 1\nd:\n  <<: *b\n  k: z\n"},

		{"alias key by its target", "x: &k foo\n*k : v\n", []string{"set", ".foo", "z"}, "x: &k foo\n*k : z\n"},
		{"alias key delete", "x: &k foo\n*k : v\n", []string{"delete", ".foo"}, "x: &k foo\n"},
		{"alias key beside its anchor's name", "x: &k foo\nk: 1\n*k : v\n", []string{"set", ".k", "z"}, "x: &k foo\nk: z\n*k : v\n"},
		{"delete beside an anchor's name", "x: &k foo\nk: 1\n*k : v\n", []string{"delete", ".k"}, "x: &k foo\n*k : v\n"},
		{"alias key by its target's read spelling", "x: &k 1.0\n*k : v\n", []string{"set", `."1"`, "z"}, "x: &k 1.0\n*k : z\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v\n%s", err, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyAddressesAKeyByEitherSpelling(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"set", "set \"1\" = z\n", "1.0: z\nc: 1\n"},
		{"the cache follows a rename", "rename \"1\" = q\nset q = 2\nset \"1\" = n\n", "q: 2\nc: 1\n\"1\": n\n"},
		{"and frees the old spelling", "rename \"1.0\" = q\nset \"1\" = n\n", "q: b\nc: 1\n\"1\": n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "ops")
			if err := os.WriteFile(script, []byte(tc.script), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := execute(t, "1.0: b\nc: 1\n", "apply", "-f", script)
			if err != nil {
				t.Fatalf("apply: %v\n%s", err, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAReadOfASharedSpellingIsRefused: this used to take whichever key a Go
// map's iteration reached first, so the answer changed from run to run.
func TestAReadOfASharedSpellingIsRefused(t *testing.T) {
	twins := "1: a\n1.0: b\n"
	for _, tc := range []struct {
		name, in string
		args     []string
	}{
		{"get", twins, []string{"-o", "raw", `."1"`}},
		{"type", twins, []string{"type", `."1"`}},
		{"default", twins, []string{"--default", "D", `."1"`}},
		{"all docs", twins, []string{"--all-docs", "-o", "raw", `."1"`}},
		{"quiet", twins, []string{"-q", `."1"`}},
		{"fan-out", "1: {x: a}\n1.0: {x: b}\n", []string{"-o", "raw", `**."1".x`}},
		{"own key and a merged one", "base: &b\n  1.0: x\nd:\n  1: own\n  <<: *b\n", []string{"-o", "raw", `.d."1"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 30 {
				got, err := execute(t, tc.in, tc.args...)
				if !errors.Is(err, query.ErrAmbiguousKey) {
					t.Fatalf("got %q, %v; want ErrAmbiguousKey", got, err)
				}
				if codeFor(err) != 1 {
					t.Fatalf("exit %d, want 1", codeFor(err))
				}
				if strings.Contains(got, "D") {
					t.Fatalf("--default answered a refusal: %q", got)
				}
			}
		})
	}
}

func TestAReadOfASharedSpellingIsRefusedAsJSON(t *testing.T) {
	got, err := execute(t, "1: a\n1.0: b\n", "-o", "json", `."1"`)
	if err == nil {
		t.Fatalf("no error: %q", got)
	}
	want := `{"error":"path names more than one key: \"1\"","kind":"no-match"}` + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnEditOfASharedSpellingIsRefused(t *testing.T) {
	twins := "1: a\n1.0: b\n"
	for _, tc := range []struct {
		name, in string
		args     []string
		names    []string
	}{
		{"set", twins, []string{"set", `."1"`, "z"}, []string{"1 (line 1)", "1.0 (line 2)"}},
		{"delete", twins, []string{"delete", `."1"`}, []string{"1 (line 1)", "1.0 (line 2)"}},
		{"delete all docs", twins, []string{"delete", "--all-docs", `."1"`}, []string{"1 (line 1)", "1.0 (line 2)"}},
		{"a string twin", "1.0: a\n\"1\": b\n", []string{"set", `."1"`, "z"}, []string{"1.0", "1 (line 2)"}},
		{"null", "~: a\n\"null\": b\n", []string{"set", ".null", "z"}, []string{"~", "null"}},
		{"exponent", "1e3: a\n1000: b\n", []string{"set", `."1000"`, "z"}, []string{"1e3", "1000"}},
		{"nan", ".nan: a\n.NaN: b\n", []string{"set", ".NaN", "z"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := execute(t, tc.in, tc.args...)
			if !errors.Is(err, ymledit.ErrAmbiguousKey) {
				t.Fatalf("got %q, %v; want ErrAmbiguousKey", got, err)
			}
			if errors.Is(err, ymledit.ErrNotResolved) {
				t.Error("carries ErrNotResolved, so --all-docs would skip it")
			}
			if codeFor(err) != 1 {
				t.Errorf("exit %d, want 1", codeFor(err))
			}
			for _, n := range tc.names {
				if !strings.Contains(err.Error(), n) {
					t.Errorf("error %q doesn't name %q", err, n)
				}
			}
		})
	}
}

func TestRenameRefusesASpellingAKeyAlreadyAnswersTo(t *testing.T) {
	for _, tc := range []struct{ in, key string }{
		{"1.0: b\nc: d\n", "1"},
		{"~: b\nc: d\n", "null"},
	} {
		_, err := execute(t, tc.in, "rename", ".c", tc.key)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("rename .c %s on %q: %v, want already exists", tc.key, tc.in, err)
		}
	}
}

// TestSetRefusesToShadowAMergedKeyItCannotOverride: `"1"` beside a merged `1.0`
// overrides nothing, since the two are different keys.
func TestSetRefusesToShadowAMergedKeyItCannotOverride(t *testing.T) {
	for _, in := range []string{
		"base: &b\n  1.0: x\nd:\n  5: own\n  <<: *b\n",
		"base: &b\n  1.0: x\nd:\n  <<: *b\n",
	} {
		for _, args := range [][]string{{"set", `.d."1"`, "z"}, {"delete", `.d."1"`}} {
			if got, err := execute(t, in, args...); !errors.Is(err, ymledit.ErrMergedKey) {
				t.Errorf("%v on %q: got %q, %v; want ErrMergedKey", args, in, got, err)
			}
		}
	}
}

func TestAMissListsASharedSpellingOnce(t *testing.T) {
	twins := "1: a\n1.0: b\n"
	_, err := execute(t, twins, "-o", "raw", ".x")
	if err == nil || strings.Contains(err.Error(), `"1", "1"`) {
		t.Errorf("text: %v", err)
	}
	got, _ := execute(t, twins, "-o", "json", ".x")
	if !strings.Contains(got, `"available":["1"]`) {
		t.Errorf("json: %s", got)
	}
}

// TestSharedSpellingsStayReadable: only the path that can't tell the keys apart
// is refused, not the document.
func TestSharedSpellingsStayReadable(t *testing.T) {
	twins := "1: a\n1.0: b\n"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-o", "raw", "*"}, "b\na\n"},
		{[]string{"*", "--paths"}, "\"1\"\n\"1\"\n"},
	} {
		got, err := execute(t, twins, tc.args...)
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
		} else if got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.args, got, tc.want)
		}
	}
	if got, err := execute(t, twins, "validate", "--require", `."1"`); err != nil {
		t.Errorf("validate --require: %v\n%s", err, got)
	}
}

// TestNoPrintedPathAddsAKey holds the --paths contract across every shape: an
// edit of a printed path lands on a key or refuses, and never adds one.
func TestNoPrintedPathAddsAKey(t *testing.T) {
	refusals := []error{ymledit.ErrAmbiguousKey, ymledit.ErrMergedKey, ymledit.ErrAliased, ymledit.ErrAnchored}
	for _, in := range []string{
		"1.0: a\n~: b\nTRUE: c\n0x10: d\n.inf: e\nplain: f\n",
		"a:\n  1.0:\n    ~: b\n  m: {1e3: c, +1: d}\n",
		"1: a\n1.0: b\n",
		"~: a\n\"null\": b\n",
		"x: &k foo\n*k : v\n",
		"x: &k 1.0\n*k : v\n",
		"base: &b\n  1.0: x\nd:\n  5: own\n  <<: *b\n",
	} {
		before, err := execute(t, in, "**", "--paths")
		if err != nil {
			t.Fatalf("%q: --paths: %v", in, err)
		}
		for _, p := range strings.Fields(before) {
			if p == "." {
				continue
			}
			got, err := execute(t, in, "set", "--must-exist", p, "1")
			if err != nil {
				if !slices.ContainsFunc(refusals, func(r error) bool { return errors.Is(err, r) }) {
					t.Errorf("%q: set %s: %v", in, p, err)
				}
				continue
			}
			after, err := execute(t, got, "**", "--paths")
			if err != nil {
				t.Errorf("%q: set %s wrote %q, which reads back as %v", in, p, got, err)
			} else if strings.Count(after, "\n") > strings.Count(before, "\n") {
				t.Errorf("%q: set %s added a key: %q", in, p, got)
			}
		}
	}
}
