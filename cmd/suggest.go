package cmd

import "strings"

// nearest picks the candidate closest to target and reports whether it is
// close enough to put in front of a user as "did you mean ...?".
//
// A candidate differing only in case wins outright, ahead of any edit
// distance: it is the one near miss that is almost never a coincidence.
// Otherwise the closest candidate within maxEdits wins, and a tie loses —
// two equally plausible keys make a worse error than no suggestion, since
// the reader now has to check both.
func nearest(target string, candidates []string) (string, bool) {
	for _, c := range candidates {
		if c != target && strings.EqualFold(c, target) {
			return c, true
		}
	}

	t := []rune(target)
	limit := maxEdits(len(t))
	best, bestDist, tied := "", limit+1, false
	for _, c := range candidates {
		r := []rune(c)
		// A length difference alone costs that many edits, so anything
		// further away than the limit can't beat it — and skipping those
		// keeps this linear in the number of keys for a wide mapping.
		if abs(len(r)-len(t)) > limit {
			continue
		}
		d := editDistance(t, r)
		switch {
		case d < bestDist:
			best, bestDist, tied = c, d, false
		case d == bestDist:
			tied = true
		}
	}
	if bestDist > limit || tied {
		return "", false
	}
	return best, true
}

// maxEdits is how far a candidate may be from the target and still be worth
// suggesting. Short keys get one edit: at two, "ab" is as close to "xy" as
// to anything else, and the suggestion is guesswork.
func maxEdits(n int) int {
	if n <= 3 {
		return 1
	}
	return 2
}

// editDistance is the optimal string alignment distance between a and b:
// Levenshtein plus transposition of two adjacent runes as a single edit,
// since swapped neighbours are the typo a plain Levenshtein distance scores
// worst (it charges two substitutions, the same as two unrelated letters).
//
// Compared over runes rather than bytes so a multi-byte character counts as
// one edit, not as its encoded length.
func editDistance(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	// Three rows are enough: the transposition case looks two back.
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
