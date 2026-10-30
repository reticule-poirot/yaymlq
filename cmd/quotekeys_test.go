package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// awkwardKeyDoc holds a key of every kind quoteKey quotes: the empty string,
// a lone wildcard spelling, an integer, one with a dot, and one with
// whitespace — plus a plain key that must stay bare.
const awkwardKeyDoc = "\"\": empty\n\"a.b\": dotted\n\"08\": padded\n\"my key\": spaced\n\"*\": star\nplain: p\n"

// availableKeys pulls the list out of a "path not found" message.
func availableKeys(t *testing.T, msg string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(msg, "available keys: ")
	if !ok {
		t.Fatalf("message carries no key list: %s", msg)
	}
	list, _, ok := strings.Cut(rest, ")")
	if !ok {
		t.Fatalf("key list is unterminated: %s", msg)
	}
	return strings.Split(list, ", ")
}

// TestAvailableKeysAreSpelledAsPathSegments is #241. The list is advice a
// caller turns into a path, and it was printed bare — so a key named "" was
// invisible (`available keys: , *, 08, ...`, which reads as a stray comma),
// and `my key` could not be told from two keys in a comma-separated list.
// --paths has always rendered the same keys through path.Format; this list did
// not.
func TestAvailableKeysAreSpelledAsPathSegments(t *testing.T) {
	_, err := execute(t, awkwardKeyDoc, "-o", "raw", ".nope")
	if err == nil {
		t.Fatal("want a path-not-found error")
	}
	got := availableKeys(t, err.Error())

	want := []string{`""`, `"*"`, `"08"`, `"a.b"`, `"my key"`, "plain"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("available keys = %v, want %v", got, want)
	}
}

// TestEveryOfferedKeyParsesAndResolves is the property the spelling exists
// for, and the reason this is a defect rather than a formatting preference: a
// key the error offers has to name something when pasted as a path.
func TestEveryOfferedKeyParsesAndResolves(t *testing.T) {
	_, err := execute(t, awkwardKeyDoc, "-o", "raw", ".nope")
	if err == nil {
		t.Fatal("want a path-not-found error")
	}
	keys := availableKeys(t, err.Error())
	if len(keys) != 6 {
		t.Fatalf("want six keys from the fixture, got %d: %v", len(keys), keys)
	}
	for _, k := range keys {
		expr := "." + k
		if _, err := path.Parse(expr); err != nil {
			t.Errorf("offered key %s does not parse as %s: %v", k, expr, err)
			continue
		}
		if _, err := execute(t, awkwardKeyDoc, "-o", "raw", expr); err != nil {
			t.Errorf("offered key %s does not resolve as %s: %v", k, expr, err)
		}
	}
}

// TestDidYouMeanUsesTheSameSpelling: the suggestion is the same advice in the
// singular, so it cannot use a different quoting rule. It was rendered with
// %q — Go's quoting, which mostly coincides with a path's and is not the same
// thing.
func TestDidYouMeanUsesTheSameSpelling(t *testing.T) {
	// "a.b" is one edit from "a.c", so the typo branch fires.
	_, err := execute(t, "\"a.b\": dotted\n", "-o", "raw", `."a.c"`)
	if err == nil {
		t.Fatal("want a path-not-found error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "did you mean") {
		t.Fatalf("want the typo branch, got: %s", msg)
	}
	_, rest, _ := strings.Cut(msg, "did you mean ")
	spelling, _, _ := strings.Cut(rest, "?")
	if _, err := path.Parse("." + spelling); err != nil {
		t.Errorf("suggested %s, which does not parse as a path: %v", spelling, err)
	}
	if _, err := execute(t, "\"a.b\": dotted\n", "-o", "raw", "."+spelling); err != nil {
		t.Errorf("suggested %s, which does not resolve: %v", spelling, err)
	}
}

// TestDidYouMeanForAControlCharacterKey is where %q is not merely a different
// rule but a wrong one. Go escapes a control character as \x01; a path
// expression has exactly six escapes (\\ \" \' \n \t \r) and rejects \x, so
// the suggestion could not be pasted back:
//
//	path not found: a\x01c (did you mean "a\x01b"?)
//	-> unknown escape "\x" in path "\"a\\x01b\""
//
// Rendering it the way --paths does emits the key's own bytes, which parse.
func TestDidYouMeanForAControlCharacterKey(t *testing.T) {
	const doc = "\"a\\x01b\": v\n"
	_, err := execute(t, doc, "-o", "raw", ".a\x01c")
	if err == nil {
		t.Fatal("want a path-not-found error")
	}
	_, rest, ok := strings.Cut(err.Error(), "did you mean ")
	if !ok {
		t.Fatalf("want the typo branch, got: %s", err)
	}
	spelling, _, _ := strings.Cut(rest, "?")
	if _, perr := path.Parse("." + spelling); perr != nil {
		t.Errorf("suggested %q, which does not parse: %v", spelling, perr)
	}
}

// TestJSONAvailableKeysStayRawNames is the half that must not change, and the
// easiest to break by "making it consistent". Under -o json the keys are data
// in a field, not prose a human pastes, and CLAUDE.md's contract is that a
// recovery fact is reported once in one form — so a consumer holding
// available[i] must get the key's name, not its path spelling, or it has to
// strip quoting the tool just added.
func TestJSONAvailableKeysStayRawNames(t *testing.T) {
	out, _ := execute(t, awkwardKeyDoc, "-o", "json", ".nope")
	var payload struct {
		Available []string `json:"available"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not JSON (%v): %s", err, out)
	}
	want := map[string]bool{"": true, "*": true, "08": true, "a.b": true, "my key": true, "plain": true}
	if len(payload.Available) != len(want) {
		t.Fatalf("available = %q, want the six raw names", payload.Available)
	}
	for _, k := range payload.Available {
		if !want[k] {
			t.Errorf("available carries %q; JSON must hold raw key names, not path spellings", k)
		}
	}
}

// TestKeysVerbStaysBare pins the scope decision rather than leaving it
// implicit. keys emits a key's *name* as data, and it round-trips because the
// caller quotes it (`yaymlq ".$k"`). Quoting it here would double-quote that
// idiom into `.""08""`, so the two surfaces differ on purpose: one is prose,
// the other is data.
func TestKeysVerbStaysBare(t *testing.T) {
	out, err := execute(t, awkwardKeyDoc, "keys", "-o", "raw", ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, `"`) {
			t.Errorf("keys quoted %s; its output is a key name, which the caller quotes", line)
		}
	}
	if !strings.Contains(out, "my key") {
		t.Errorf("want the raw name %q in keys' output, got:\n%s", "my key", out)
	}
}
