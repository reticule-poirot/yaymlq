package cmd

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDeleteLastEntryScalesLinearlyWithMappingSize is #331. container's
// delete-detection rebuilt a full copy of the remaining entries ("rest") for
// every candidate deletion index before comparing it against the new
// content, which is O(n) work per candidate and O(n) candidates — O(n²) for
// a delete near the end of a large mapping or sequence, with an allocation
// per candidate on top. Deleting the last of 20k keys took ~2s before the
// fix (measured); well under a second after.
func TestDeleteLastEntryScalesLinearlyWithMappingSize(t *testing.T) {
	const n = 20000
	var b strings.Builder
	for i := range n {
		b.WriteString("k")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\n")
	}
	in := b.String()

	start := time.Now()
	if _, err := execute(t, in, "delete", ".k"+strconv.Itoa(n-1)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deleting the last of %d keys took %v, want well under 1s — looks like the quadratic bug is back", n, elapsed)
	}
}
