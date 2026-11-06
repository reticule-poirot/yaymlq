package cmd

import (
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// jsonKeyCases are mappings JSON has no direct form for: an object member name
// must be a string, and YAML keys need not be. Before #239 the bool, null and
// float rows failed at exit 1 with the encoder's own wording
// ("jsontext: object member name must be a string after offset 2") and
// kind "no-match" — a path that had resolved perfectly.
//
// The int row is the one that always worked, which is what makes the rest a
// bug rather than a limitation: the stringification exists and simply wasn't
// reached for the other scalar kinds.
var jsonKeyCases = []struct {
	name string
	doc  string
	want string
}{
	{"null key", "null: n\n", `{"null": "n"}`},
	{"bool key", "true: t\n", `{"true": "t"}`},
	{"float key", "1.5: f\n", `{"1.5": "f"}`},
	{"int key", "1: one\n", `{"1": "one"}`},
	{"mixed with strings", "a: 1\nnull: n\n", `{"a": 1, "null": "n"}`},
	{"nested", "outer:\n  null: n\n", `{"outer": {"null": "n"}}`},
	{"inside a list", "- null: n\n- 2\n", `[{"null": "n"}, 2]`},
	{"key is the value's shape too", "null:\n  true: deep\n", `{"null": {"true": "deep"}}`},
	{"all string keys are untouched", "a: 1\n\"08\": z\n", `{"a": 1, "08": "z"}`},
}

func TestJSONRendersNonStringKeysAsTheirText(t *testing.T) {
	for _, tc := range jsonKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execute(t, tc.doc, "-o", "json", ".")
			if err != nil {
				t.Fatalf("want a successful render, got %v", err)
			}
			var got, want any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("output is not JSON (%v): %s", err, out)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %s, want %s", out, tc.want)
			}
		})
	}
}

// TestJSONKeySpellingIsThePathSpelling is the half that makes the choice of
// text non-arbitrary. A key rendered into JSON has to be the same spelling a
// path expression addresses it by — `null`, not `<nil>` and not `""` — or a
// caller reading an object member name back as a path reaches nothing. The
// path-syntax table in --help promises exactly this: "a non-string key (1:,
// null:) by its text".
func TestJSONKeySpellingIsThePathSpelling(t *testing.T) {
	const doc = "null: n\ntrue: t\n1.5: f\n1: i\n"
	out, err := execute(t, doc, "-o", "json", ".")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("output is not a JSON object (%v): %s", err, out)
	}
	for name := range obj {
		expr := `."` + name + `"`
		if _, err := path.Parse(expr); err != nil {
			t.Errorf("member name %q does not round-trip into a path: %v", name, err)
			continue
		}
		if _, err := execute(t, doc, "-o", "raw", expr); err != nil {
			t.Errorf("member name %q is not addressable as %s: %v", name, expr, err)
		}
	}
}

// TestPathsRoundTripUnderJSONOutput is the probe that found #239, kept as a
// test. The property is the one CLAUDE.md names: a path --paths emits must
// resolve. It held for -o raw and broke for -o json on any document with a
// non-string key, since the failure was in rendering rather than resolving.
func TestPathsRoundTripUnderJSONOutput(t *testing.T) {
	const doc = "plain: 1\nnull: n\ntrue: t\n1.5: f\nnested:\n  - null: deep\n"
	out, err := execute(t, doc, "--paths", "-o", "raw", "**")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, p := range strings.Split(out, "\n") {
		if p == "" || strings.HasPrefix(p, "note:") {
			continue
		}
		seen++
		if _, err := execute(t, doc, "-o", "json", p); err != nil {
			t.Errorf("--paths emitted %q, which does not render under -o json: %v", p, err)
		}
	}
	if seen == 0 {
		t.Fatal("no paths to check; the fixture or --paths changed")
	}
}

// TestNonStringKeysStillRenderAsYAML guards the formats that always worked, so
// the fix can't quietly reshape them: -o yaml must keep a null key as `null:`
// rather than borrowing JSON's quoted spelling.
func TestNonStringKeysStillRenderAsYAML(t *testing.T) {
	out, err := execute(t, "null: n\ntrue: t\n", "-o", "yaml", ".")
	if err != nil {
		t.Fatal(err)
	}
	// The keys, not the values: yaml.v3 quotes the value "n" of its own
	// accord, since YAML 1.1 reads a bare n as false.
	for _, want := range []string{"null:", "true:"} {
		if !strings.Contains(out, want) {
			t.Errorf("want the key %q unquoted in the YAML rendering, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"null":`) {
		t.Errorf("YAML output borrowed JSON's quoted key spelling:\n%s", out)
	}
}

// TestYAMLRejectsKeysWithTheSameText: a key and its quoted twin — `1` and
// `"1"`, `null` and `"null"` — never reach rendering. yaml.v3 refuses the
// document as a duplicate key. TestJSONRefusesKeysThatStringifyAlike covers the
// pairs it does accept.
func TestYAMLRejectsKeysWithTheSameText(t *testing.T) {
	for _, doc := range []string{"1: a\n\"1\": b\n", "null: a\n\"null\": b\n"} {
		// Under -o json the message is written to the output and the error
		// carries only the code, so the assertion is on both.
		out, err := execute(t, doc, "-o", "json", ".")
		if code := exitCode(err, io.Discard); code != 2 {
			t.Errorf("%q: want exit 2 (parse), got %d (%v)", doc, code, err)
		}
		if !strings.Contains(out, "already defined") {
			t.Errorf("%q: want yaml.v3's duplicate-key error, got %s", doc, out)
		}
	}
}

// TestJSONRefusesKeysThatStringifyAlike guards #300. Each document holds an int
// key and a float key that yaml.v3 keeps apart but query.KeyText spells the
// same; one JSON object cannot hold both. -o json used to keep one of them,
// picked by map iteration, so it is repeated here to catch an answer that
// changes between runs. -o yaml keeps both, and still must.
func TestJSONRefusesKeysThatStringifyAlike(t *testing.T) {
	cases := []struct {
		name, doc, query, spelled string
		values                    []string
	}{
		{"issue repro", "f: {0: 'a', 1, 000., X}\n", ".f", "0", []string{"a", "null"}},
		{"one and one point zero", "1: x\n1.0: y\n", ".", "1", []string{"x", "y"}},
		{"negative", "-1: x\n-1.0: y\n", ".", "-1", []string{"x", "y"}},
		{"exponent", "1e2: x\n100: y\n", ".", "100", []string{"x", "y"}},
		{"inside a sequence item", "a:\n  b:\n    - {2: x, 2.0: y}\n", ".", "2", []string{"x", "y"}},
		{"below the queried node", "top:\n  inner:\n    3: x\n    3.0: y\n", ".top", "3", []string{"x", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 10; i++ {
				out, err := execute(t, tc.doc, "-o", "json", tc.query)
				if code := exitCode(err, io.Discard); code != 3 {
					t.Fatalf("run %d: exit %d, want 3 (usage); output %s", i, code, out)
				}
				var got jsonError
				if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
					t.Fatalf("run %d: output is not a JSON error: %v\n%s", i, jerr, out)
				}
				if got.Kind != "usage" {
					t.Errorf("kind = %q, want usage", got.Kind)
				}
				for _, part := range []string{strconv.Quote(tc.spelled), "-o yaml"} {
					if !strings.Contains(got.Error, part) {
						t.Errorf("error %q does not mention %s", got.Error, part)
					}
				}
			}
			out, err := execute(t, tc.doc, "-o", "yaml", tc.query)
			if err != nil {
				t.Fatalf("-o yaml: %v", err)
			}
			for _, v := range tc.values {
				if !strings.Contains(out, v) {
					t.Errorf("-o yaml lost %q:\n%s", v, out)
				}
			}
		})
	}
}

// TestJSONStillRendersDistinctNonStringKeys: the refusal is about a shared
// name, not about non-string keys, which #239 made renderable.
func TestJSONStillRendersDistinctNonStringKeys(t *testing.T) {
	out, err := execute(t, "1: x\n1.5: y\ntrue: z\n", "-o", "json", ".")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, out)
	}
	if want := map[string]string{"1": "x", "1.5": "y", "true": "z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestReadRefusesKeysThatResolveAlike: yaml.v3 refuses a key written twice the
// same way, but compares nothing beyond the text, so `7` beside `0o7` decoded
// as one key and the later value silently replaced the earlier, under every -o
// (found on #300). YAML 1.2 makes keys with the same tag and canonical value
// duplicates, so these are parse errors too, reported like yaml.v3's own.
func TestReadRefusesKeysThatResolveAlike(t *testing.T) {
	cases := []struct{ name, doc, first, second string }{
		{"octal", "7: x\n0o7: y\n", "7", "0o7"},
		{"hex", "16: x\n0x10: y\n", "16", "0x10"},
		{"plus sign", "1: x\n+1: y\n", "1", "+1"},
		{"underscore", "1000: x\n1_000: y\n", "1000", "1_000"},
		{"bool case", "true: x\nTrue: y\n", "true", "True"},
		{"tilde null", "null: x\n~: y\n", "null", "~"},
		{"float digits", "1.0: x\n1.00: y\n", "1.0", "1.00"},
		{"infinity", ".inf: x\n.Inf: y\n", ".inf", ".Inf"},
		{"signed zero", "0.0: x\n-0.0: y\n", "0.0", "-0.0"},
		{"alias key", "&k a: x\n*k : y\n", "a", "a"},
		{"nested", "outer:\n  - in:\n      7: x\n      0o7: y\n", "7", "0o7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, format := range []string{"yaml", "json", "raw"} {
				out, err := execute(t, tc.doc, "-o", format, ".")
				if code := exitCode(err, io.Discard); code != 2 {
					t.Fatalf("-o %s: exit %d, want 2 (parse); output %s", format, code, out)
				}
				msg := ""
				if format == "json" {
					var je jsonError
					if jerr := json.Unmarshal([]byte(out), &je); jerr != nil {
						t.Fatalf("-o json: output is not a JSON error: %v\n%s", jerr, out)
					}
					if je.Kind != "parse" || je.Line != 2 && tc.name != "nested" {
						t.Errorf("-o json: got %+v, want kind parse at line 2", je)
					}
					msg = je.Error
				} else if err != nil {
					msg = err.Error()
				}
				for _, part := range []string{"already defined", strconv.Quote(tc.second), strconv.Quote(tc.first)} {
					if !strings.Contains(msg, part) {
						t.Errorf("-o %s: %q does not mention %s", format, msg, part)
					}
				}
			}
		})
	}
}

// TestReadKeepsKeysThatOnlyLookAlike: what the duplicate check must not
// refuse — keys whose tags differ (#300's int and float, which -o yaml keeps),
// NaN, which equals nothing, `yes`, which yaml.v3 reads as a string, and a key
// written out over one a merge brought in.
func TestReadKeepsKeysThatOnlyLookAlike(t *testing.T) {
	for _, doc := range []string{
		"1: x\n1.0: y\n",
		".nan: x\n.NaN: y\n",
		"yes: x\ntrue: y\n",
		"base: &b\n  k: 1\nm:\n  <<: *b\n  k: 2\n",
		"7: x\n\"0o7\": y\n",
	} {
		if out, err := execute(t, doc, "-o", "yaml", "."); err != nil {
			t.Errorf("%q: %v\n%s", doc, err, out)
		}
	}
}

// TestJSONNamesTheSameCollisionEveryRun: with more than one collision, the one
// named was whichever map iteration reached first.
func TestJSONNamesTheSameCollisionEveryRun(t *testing.T) {
	cases := []struct{ name, doc, spelled string }{
		{"two in one mapping", "{1: a, 1.0: b, 2: c, 2.0: d}\n", "1"},
		{"under string keys", "x: {2: c, 2.0: d}\ny: {1: a, 1.0: b}\n", "2"},
		{"in a key's value and beside it", "{1: {7: a, 7.0: b}, 2: c, 2.0: d}\n", "7"},
		{"under keys sharing a spelling", "{1: {7: a, 7.0: b}, 1.0: {8: c, 8.0: d}}\n", "8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := "spelled " + strconv.Quote(tc.spelled)
			for range 64 {
				out, err := execute(t, tc.doc, "-o", "json", ".")
				if err == nil {
					t.Fatalf("want an error, got %s", out)
				}
				var got jsonError
				if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
					t.Fatalf("output is not a JSON error: %v\n%s", jerr, out)
				}
				if !strings.Contains(got.Error, want) {
					t.Fatalf("error %q does not contain %s", got.Error, want)
				}
			}
		})
	}
}
