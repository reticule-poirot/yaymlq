package cmd

import (
	"encoding/json"
	"io"
	"reflect"
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

// TestTwoKeysCannotStringifyAlike records why #239 needs no collision
// handling, which was the one thing it left open. Distinct YAML keys whose
// text matches — `1` and `"1"`, or `null` and `"null"` — cannot coexist:
// yaml.v3 rejects the document as a duplicate key before any of this code
// runs. So stringifying a key can never merge two members.
func TestTwoKeysCannotStringifyAlike(t *testing.T) {
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
