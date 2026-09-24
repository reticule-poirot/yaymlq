package cmd

import "testing"

// TestNearest covers what a suggestion is for: the single most common way a
// path misses is a typo in one key, and the tool is holding the correct
// spelling at the moment it fails.
func TestNearest(t *testing.T) {
	jobs := []string{"changes", "govulncheck", "hygiene", "lint", "test"}
	tests := []struct {
		name, target string
		candidates   []string
		want         string
		wantOK       bool
	}{
		{"transposed letters", "tset", jobs, "test", true},
		{"one letter wrong", "linr", jobs, "lint", true},
		{"one letter missing", "hygiee", jobs, "hygiene", true},
		{"one letter extra", "testt", jobs, "test", true},
		{"wrong case only", "Lint", jobs, "lint", true},
		{"wrong case on a long key", "GoVulnCheck", jobs, "govulncheck", true},
		{"nothing close", "replicas", jobs, "", false},
		{"no candidates", "anything", nil, "", false},
		{"ambiguous between two", "ab", []string{"aa", "ac"}, "", false},
		{"short target needs a closer match", "ab", []string{"xy"}, "", false},
		{"one edit on a short target is enough", "ab", []string{"abc"}, "abc", true},
		{"case-insensitive match beats a closer edit distance", "Test", []string{"test", "tests"}, "test", true},
	}
	for _, tc := range tests {
		got, ok := nearest(tc.target, tc.candidates)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: nearest(%q, %q) = (%q, %v), want (%q, %v)",
				tc.name, tc.target, tc.candidates, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestEditDistance pins the metric itself: transposing two adjacent runes
// counts as one edit, not two, because that is the typo shape a plain
// Levenshtein distance is worst at recognising.
func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"test", "test", 0},
		{"test", "tset", 1},
		{"test", "tets", 1},
		{"test", "tes", 1},
		{"test", "tests", 1},
		{"test", "tost", 1},
		{"test", "toast", 2},
		{"", "abc", 3},
		{"abc", "", 3},
		{"", "", 0},
		{"kubernetes", "kubernets", 1},
		{"héllo", "hello", 1},
	}
	for _, tc := range tests {
		if got := editDistance([]rune(tc.a), []rune(tc.b)); got != tc.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
