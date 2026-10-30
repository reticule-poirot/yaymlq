package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// ordinals maps a ratio to the word the README uses for it. Only the range a
// manifest of this shape can plausibly land in — a figure outside it means the
// claim needs rewriting by hand, not a new table entry.
var ordinals = map[int]string{
	4: "quarter", 5: "fifth", 6: "sixth", 7: "seventh",
	8: "eighth", 9: "ninth", 10: "tenth",
}

// sizeClaims are the two places that describe how big the manifest is: the
// README for a person, and SKILL.md for an agent in another repo deciding
// whether a bare `schema` is worth the tokens (#155). SKILL.md held the
// staler pair of the two — "~27KB" against an actual 30,214 bytes — which is
// the half that mattered more, since it is advice about a token budget.
//
// Each is scoped to the paragraph that makes the claim rather than the whole
// file, so the assertions can't be satisfied, or broken, by text elsewhere.
var sizeClaims = []struct {
	file string
	lead string
}{
	{"../README.md", "A single command is about"},
	{"../contrib/claude-skill/SKILL.md", "For anything not in this table"},
}

func schemaParagraph(t *testing.T, file, lead string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(b), lead)
	if i < 0 {
		t.Fatalf("%s has no paragraph starting %q; this test is pinned to it", file, lead)
	}
	rest := string(b)[i:]
	if j := strings.Index(rest, "\n\n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// absoluteSize matches a byte figure quoted in prose: 30KB, 27 KB, 1.5MB.
var absoluteSize = regexp.MustCompile(`\d+(\.\d+)?\s?[KM]i?B`)

// TestREADMEQuotesNoAbsoluteManifestSize is #234. The README said "under
// 30KB"; 0.21.0's manifest is 30,214 bytes, so the claim was true only if KB
// is read as KiB — #213's nine new flag entries pushed it over during that
// release.
//
// The paragraph predicted its own failure in the very next sentence ("a byte
// count quoted in prose goes stale at exactly that rate") and got caught
// anyway, which is why the fix is to stop quoting the number rather than to
// round it again. This test is what keeps it from coming back: a figure that
// decays with every flag added has no stable correct value to assert, so the
// assertion is that it isn't there.
func TestDocsQuoteNoAbsoluteManifestSize(t *testing.T) {
	for _, c := range sizeClaims {
		t.Run(c.file, func(t *testing.T) {
			if got := absoluteSize.FindString(schemaParagraph(t, c.file, c.lead)); got != "" {
				t.Errorf("%s quotes an absolute manifest size (%q), which goes stale with every flag added — state the ratio instead (#234)", c.file, got)
			}
		})
	}
}

// TestREADMEManifestRatioMatchesTheLiveManifest holds the claim that survives.
// A ratio is stable as flags are added roughly evenly across verbs, and it is
// the figure a caller deciding between `schema` and `schema --command X`
// actually needs — so unlike the byte count, it is worth asserting rather than
// deleting.
//
// Measured off the live tree, the same way the manifest itself is built, so
// the README is checked against the tool and not against a second copy of the
// number.
func TestDocsManifestRatioMatchTheLiveManifest(t *testing.T) {
	full, err := execute(t, "", "schema")
	if err != nil {
		t.Fatal(err)
	}
	one, err := execute(t, "", "schema", "--command", "get")
	if err != nil {
		t.Fatal(err)
	}
	if len(one) == 0 {
		t.Fatal("schema --command get produced nothing")
	}

	ratio := float64(len(full)) / float64(len(one))
	nearest := int(ratio + 0.5)
	word, ok := ordinals[nearest]
	if !ok {
		t.Fatalf("a single command is now 1/%.1f of the manifest, outside the range the README's wording covers — rewrite the sentence (#234)", ratio)
	}
	for _, c := range sizeClaims {
		if para := schemaParagraph(t, c.file, c.lead); !strings.Contains(para, word) {
			t.Errorf("a single command is 1/%.1f of the manifest, so %s should say %q; paragraph reads: %s", ratio, c.file, word, para)
		}
	}
}
