package cmd

import (
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// nonStringKeyDoc holds one key of each scalar kind YAML allows as a key,
// plus a plain one, so both the spelling and the ordering can be checked at
// once.
const nonStringKeyDoc = "null: n\ntrue: t\n1.5: f\nalpha: a\n"

// TestKeysSpellsANullKeyTheWayAPathDoes is #242. keys rendered its keys with
// fmt.Sprint, which spells nil "<nil>" — a key no path expression can
// resolve. The tool then contradicted itself in one exchange, since the
// recovery path has used query.KeyText since #203:
//
//	$ yaymlq keys -o raw '.' nk.yaml      -> 1.5, <nil>, true
//	$ yaymlq -o raw '.<nil>' nk.yaml      -> path not found: <nil>
//	                                         (available keys: 1.5, null, true)
func TestKeysSpellsANullKeyTheWayAPathDoes(t *testing.T) {
	out, err := execute(t, nonStringKeyDoc, "keys", "-o", "raw", ".")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<nil>") {
		t.Errorf("keys emitted Go's nil spelling, which no path resolves:\n%s", out)
	}
	if !strings.Contains(out, "null") {
		t.Errorf("want the null key spelled %q, got:\n%s", "null", out)
	}
}

// TestKeysOrderMatchesTheWildcardOrder is the other half, which the issue did
// not mention and the help has always promised: "keys prints one key per
// line, sorted (the same order wildcards use)". Sorting fmt.Sprint's output
// put "<nil>" before every letter, so the two disagreed whenever a null key
// was present.
//
// The wildcard's own order is read back through path.Parse rather than
// hard-coded, so this compares keys against the live resolver instead of
// against a second copy of the expectation.
func TestKeysOrderMatchesTheWildcardOrder(t *testing.T) {
	keysOut, err := execute(t, nonStringKeyDoc, "keys", "-o", "raw", ".")
	if err != nil {
		t.Fatal(err)
	}
	pathsOut, err := execute(t, nonStringKeyDoc, "--paths", "-o", "raw", ".*")
	if err != nil {
		t.Fatal(err)
	}

	var viaWildcard []string
	for _, p := range strings.Split(strings.TrimSpace(pathsOut), "\n") {
		if p == "" {
			continue
		}
		segs, err := path.Parse(p)
		if err != nil {
			t.Fatalf("--paths emitted %q, which does not parse: %v", p, err)
		}
		if len(segs) != 1 {
			t.Fatalf("want one segment per top-level match, got %d for %q", len(segs), p)
		}
		viaWildcard = append(viaWildcard, segs[0].Key)
	}

	viaKeys := strings.Fields(strings.TrimSpace(keysOut))
	if strings.Join(viaKeys, ",") != strings.Join(viaWildcard, ",") {
		t.Errorf("keys order %v does not match the wildcard order %v, though the help says it does",
			viaKeys, viaWildcard)
	}
}

// TestEveryKeyKeysEmitsIsAddressable is the property that makes the spelling
// the right one rather than merely different: a key name keys prints has to
// name something, quoted as a path segment.
//
// Quoted, which is the current contract: keys prints a key's *name*, not a
// path, so a name needing quotes (1.5, which bare would read as index [1]
// then .5) is the caller's to quote. Whether keys should quote for them is a
// separate question, noted on #241.
func TestEveryKeyKeysEmitsIsAddressable(t *testing.T) {
	out, err := execute(t, nonStringKeyDoc, "keys", "-o", "raw", ".")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, k := range strings.Fields(strings.TrimSpace(out)) {
		seen++
		expr := `."` + k + `"`
		if _, err := execute(t, nonStringKeyDoc, "-o", "raw", expr); err != nil {
			t.Errorf("keys emitted %q, which does not resolve as %s: %v", k, expr, err)
		}
	}
	if seen != 4 {
		t.Fatalf("want four keys from the fixture, got %d", seen)
	}
}

// TestKeysOfAllStringMappingIsUnchanged: the overwhelmingly common case goes
// through a different branch (yaml.v3 decodes an all-string mapping as
// map[string]any), and must stay exactly as it was.
func TestKeysOfAllStringMappingIsUnchanged(t *testing.T) {
	out, err := execute(t, "b: 2\na: 1\nc: 3\n", "keys", "-o", "raw", ".")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(strings.TrimSpace(out)); strings.Join(got, ",") != "a,b,c" {
		t.Errorf("want a,b,c, got %v", got)
	}
}
