package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// The path-syntax reference exists twice: as the README's "### Path syntax"
// table, and as the Path syntax block in the root command's Long text, which
// schema reflects into its `long` field — the surface
// contrib/claude-skill/SKILL.md tells another repo's agent to read *instead
// of* --help. The two drifted: the README listed thirteen spellings and the
// help text eight, so an agent following the skill's own advice was told the
// tool has no recursive descent, the feature #200 added (#226).
//
// The tests below hold them level and hold both to what internal/path
// actually accepts. "(empty)" is the one row that names a spelling it cannot
// show — the empty path, the whole document — and both sources write it that
// way; emptySpelling is where that is turned back into "".
const emptySpelling = "(empty)"

var (
	backticked  = regexp.MustCompile("`([^`]*)`")
	columnBreak = regexp.MustCompile(`\s{2,}`)
)

// readmePathSyntax returns the spellings in the first column of the README's
// path-syntax table. A row may name more than one ("a[].b, a[*]"), and the
// Meaning column is dropped before extraction so its own backticks don't
// count as spellings.
func readmePathSyntax(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	var out []string
	inTable := false
	for _, line := range strings.Split(string(src), "\n") {
		switch {
		case strings.HasPrefix(line, "### Path syntax"):
			inTable = true
			continue
		case !inTable:
			continue
		case !strings.HasPrefix(line, "|"):
			if len(out) > 0 {
				return out
			}
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 3 || strings.HasPrefix(strings.TrimSpace(cols[1]), "---") ||
			strings.TrimSpace(cols[1]) == "Expression" {
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(cols[1], -1) {
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		t.Fatal("found no path-syntax table in README.md")
	}
	return out
}

// helpPathSyntax returns the spellings in the live command's Path syntax
// block, which is the text schema serves as `long`.
func helpPathSyntax(t *testing.T) []string {
	t.Helper()
	long := NewRootCommand().Long
	_, block, ok := strings.Cut(long, "Path syntax:\n")
	if !ok {
		t.Fatal("the root command's Long text has no Path syntax block")
	}
	block, _, _ = strings.Cut(block, "\n\n")
	var out []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		spellings := columnBreak.Split(line, 2)[0]
		for _, s := range strings.Split(spellings, ", ") {
			out = append(out, strings.TrimSpace(s))
		}
	}
	if len(out) == 0 {
		t.Fatal("found no rows in the Path syntax block")
	}
	return out
}

// TestPathSyntaxHelpAndReadmeNameTheSameSpellings is the guard #226 asked
// for: one list, two renderings, and nothing held them in agreement.
func TestPathSyntaxHelpAndReadmeNameTheSameSpellings(t *testing.T) {
	readme := set(readmePathSyntax(t))
	help := set(helpPathSyntax(t))
	for s := range readme {
		if !help[s] {
			t.Errorf("the README documents %q and the help text does not", display(s))
		}
	}
	for s := range help {
		if !readme[s] {
			t.Errorf("the help text documents %q and the README does not", display(s))
		}
	}
}

// TestPathSyntaxDocumentsOnlyWhatParses holds both renderings to the parser,
// so a row can't document a spelling the tool never accepted.
func TestPathSyntaxDocumentsOnlyWhatParses(t *testing.T) {
	for _, src := range []struct {
		where     string
		spellings []string
	}{
		{"README.md", readmePathSyntax(t)},
		{"the help text", helpPathSyntax(t)},
	} {
		for _, s := range src.spellings {
			expr := s
			if expr == emptySpelling {
				expr = ""
			}
			if _, err := path.Parse(expr); err != nil {
				t.Errorf("%s documents %q, which does not parse: %v", src.where, display(s), err)
			}
		}
	}
}

// TestPathSyntaxCoversEveryFanOutSpelling names the two features whose
// absence from the help text was the actual cost in #226: an agent that
// can't see ** falls back to a workaround, and one that can't see a negative
// index reaches for len first.
func TestPathSyntaxCoversEveryFanOutSpelling(t *testing.T) {
	help := set(helpPathSyntax(t))
	for _, want := range []string{"a[-1]", "**.b", "a.**.b", "a.**", "a[*]"} {
		if !help[want] {
			t.Errorf("the help text does not document %q", want)
		}
	}
}

func set(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		if s == "" {
			s = emptySpelling
		}
		m[s] = true
	}
	return m
}

// display names the empty path in a failure message, since printing %q of ""
// says nothing about which row is meant.
func display(s string) string {
	if s == "" || s == emptySpelling {
		return "the empty path"
	}
	return s
}
