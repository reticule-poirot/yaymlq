package cmd

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// applyOps reconstructs b from a by replaying ops — the strongest
// correctness check for a diff algorithm: whatever it says the edit script
// is, replaying that script against the original must reproduce the target
// exactly.
func applyOps(a []string, ops []diffLine) []string {
	var out []string
	ai := 0
	for _, op := range ops {
		switch op.kind {
		case opSame:
			out = append(out, a[ai])
			ai++
		case opDel:
			ai++
		case opAdd:
			out = append(out, op.text)
		}
	}
	return out
}

// TestMyersDiffMemoryStaysNearLinearForASmallEdit is the realistic case
// CLAUDE.md's rationale for hand-rolling this algorithm cites: a large
// document with only a small actual edit should stay cheap, since D (the
// edit distance) is tiny even though the document itself is not.
func TestMyersDiffMemoryStaysNearLinearForASmallEdit(t *testing.T) {
	n := 50000
	a := make([]string, n)
	for i := range a {
		a[i] = fmt.Sprintf("line-%d", i)
	}
	b := make([]string, n)
	copy(b, a)
	b[n/2] = "CHANGED"

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_ = myersDiff(a, b)
	runtime.ReadMemStats(&after)

	// Generous bound (the actual cost is a few MiB): this only needs to
	// catch a regression back to O(D*(N+M)) memory, which at D≈2 and
	// N+M=100000 would still allocate a full-width snapshot per round —
	// negligible here regardless, so a regression would instead show up as
	// this ceiling being blown by orders of magnitude, not by a little.
	const ceiling = 100 << 20 // 100 MiB
	if got := after.TotalAlloc - before.TotalAlloc; got > ceiling {
		t.Fatalf("myersDiff on a %d-line doc with 1 line changed allocated %d MiB, want well under %d MiB",
			n, got>>20, ceiling>>20)
	}
}

// TestMyersDiffMemoryScalesQuadraticallyNotWorse guards the actual fix for
// #54: snapshotting each round's d+1 active diagonal endpoints instead of
// the whole O(N+M)-wide working array, so total snapshot memory is O(D²)
// rather than O(D*(N+M)). This is still quadratic in this specific
// worst-case input (nothing matches at all, so D≈2N) — that's inherent to
// exact Myers diff, not something this fix claims to eliminate — but it
// must not be worse than roughly D² up to a small constant factor.
func TestMyersDiffMemoryScalesQuadraticallyNotWorse(t *testing.T) {
	alloc := func(n int) uint64 {
		a := make([]string, n)
		b := make([]string, n)
		for i := 0; i < n; i++ {
			a[i] = fmt.Sprintf("old-%d", i)
			b[i] = fmt.Sprintf("new-%d", i)
		}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		_ = myersDiff(a, b)
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	small := alloc(1000)
	large := alloc(4000) // 4x the lines -> D also ~4x -> O(D²) predicts ~16x

	// Loose upper bound (24x, not 16x) to absorb allocator noise and Go
	// runtime overhead at small sizes; O(D*(N+M)) (the bug) would instead
	// predict a much larger ratio here since that term's snapshot width
	// also grows with N+M on top of the D factor — effectively ~64x for
	// this same 4x input growth (4x from D, 4x more from N+M's own width).
	if ratio := float64(large) / float64(small); ratio > 24 {
		t.Fatalf("memory ratio for a 4x larger fully-different input = %.1fx, want at most ~24x (O(D²)); "+
			"small=%d bytes large=%d bytes — this looks like O(D*(N+M)) again", ratio, small, large)
	}
}

func TestMyersDiffReconstructsB(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
	}{
		{"both empty", nil, nil},
		{"a empty", nil, []string{"x", "y"}},
		{"b empty", []string{"x", "y"}, nil},
		{"identical", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"one line changed", []string{"a", "b", "c"}, []string{"a", "B", "c"}},
		{"append", []string{"a", "b"}, []string{"a", "b", "c"}},
		{"prepend", []string{"b", "c"}, []string{"a", "b", "c"}},
		{"delete middle", []string{"a", "b", "c", "d"}, []string{"a", "d"}},
		{"reorder-ish", []string{"a", "b", "c"}, []string{"c", "b", "a"}},
		{"all different", []string{"a", "b"}, []string{"x", "y", "z"}},
		{"repeated lines", []string{"a", "a", "a"}, []string{"a", "a"}},
		{"single line to single line", []string{"x"}, []string{"y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops := myersDiff(tc.a, tc.b)
			got := applyOps(tc.a, ops)
			if !equalSlices(got, tc.b) {
				t.Fatalf("applying diff(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.b)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name       string
		data       string
		wantLines  []string
		wantFinalN bool
	}{
		{"empty", "", nil, true},
		{"one line no newline", "a", []string{"a"}, false},
		{"one line with newline", "a\n", []string{"a"}, true},
		{"two lines no trailing newline", "a\nb", []string{"a", "b"}, false},
		{"two lines with trailing newline", "a\nb\n", []string{"a", "b"}, true},
		{"just a newline", "\n", []string{""}, true},
		{"blank line in the middle", "a\n\nb\n", []string{"a", "", "b"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, finalNL := splitLines([]byte(tc.data))
			if !equalSlices(lines, tc.wantLines) {
				t.Errorf("lines = %#v, want %#v", lines, tc.wantLines)
			}
			if finalNL != tc.wantFinalN {
				t.Errorf("endsInNewline = %v, want %v", finalNL, tc.wantFinalN)
			}
		})
	}
}

func TestUnifiedDiffIdenticalIsEmpty(t *testing.T) {
	got := unifiedDiff("f", []byte("a: 1\nb: 2\n"), []byte("a: 1\nb: 2\n"))
	if got != "" {
		t.Fatalf("identical inputs should produce no diff, got %q", got)
	}
}

func TestUnifiedDiffOneLineChanged(t *testing.T) {
	old := "a: 1\nb: 2\nc: 3\n"
	newer := "a: 1\nb: 9\nc: 3\n"
	got := unifiedDiff("cfg.yaml", []byte(old), []byte(newer))

	want := "--- a/cfg.yaml\n" +
		"+++ b/cfg.yaml\n" +
		"@@ -1,3 +1,3 @@\n" +
		" a: 1\n" +
		"-b: 2\n" +
		"+b: 9\n" +
		" c: 3\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDiffAppendAtEnd(t *testing.T) {
	old := "a: 1\n"
	newer := "a: 1\nb: 2\n"
	got := unifiedDiff("f", []byte(old), []byte(newer))
	want := "--- a/f\n" +
		"+++ b/f\n" +
		"@@ -1,1 +1,2 @@\n" +
		" a: 1\n" +
		"+b: 2\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDiffNoTrailingNewlineMarker(t *testing.T) {
	old := "a: 1"         // no trailing newline
	newer := "a: 1\nb: 2" // no trailing newline
	got := unifiedDiff("f", []byte(old), []byte(newer))
	if !strings.Contains(got, "\\ No newline at end of file\n") {
		t.Fatalf("want a no-newline marker, got:\n%s", got)
	}
	// The marker follows the "+b: 2" line, not the unchanged "a: 1" line.
	if !strings.HasSuffix(got, "+b: 2\n\\ No newline at end of file\n") {
		t.Fatalf("marker not attached to the right line, got:\n%s", got)
	}
}

func TestUnifiedDiffTrailingNewlineOnlyChangeIsVisible(t *testing.T) {
	// Same lines on both sides — only the trailing newline differs. Before
	// the fix, myersDiff compares line text only, so this rendered as no
	// change at all, even though the write does change the file's bytes.
	old := "a: 1\nb: 2" // no trailing newline
	newer := "a: 1\nb: 2\n"
	got := unifiedDiff("f", []byte(old), []byte(newer))
	if got == "" {
		t.Fatal("a trailing-newline-only change must not be an empty diff")
	}
	want := "--- a/f\n" +
		"+++ b/f\n" +
		"@@ -1,2 +1,2 @@\n" +
		" a: 1\n" +
		"-b: 2\n" +
		"\\ No newline at end of file\n" +
		"+b: 2\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDiffNoNewlineMarkerOnRemovedLineWhenLastLineChanges(t *testing.T) {
	// The old side's last line is both removed (real content change) and
	// missing its trailing newline. Before the fix, the marker was only
	// ever checked at the diff's very last rendered line — here that's the
	// "+b: 3" line, not the "-b: 2" line the marker actually belongs to —
	// so it was dropped entirely, and the diff wasn't patch(1)-applicable.
	old := "a: 1\nb: 2" // no trailing newline
	newer := "a: 1\nb: 3\n"
	got := unifiedDiff("f", []byte(old), []byte(newer))
	want := "--- a/f\n" +
		"+++ b/f\n" +
		"@@ -1,2 +1,2 @@\n" +
		" a: 1\n" +
		"-b: 2\n" +
		"\\ No newline at end of file\n" +
		"+b: 3\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDiffBothSidesMissingNewlineSharedLastLineOneMarker(t *testing.T) {
	// A change elsewhere, but the true last line ("c: 3") is unchanged and
	// missing its trailing newline on both sides — exactly one marker,
	// right after that shared last line, not one per side.
	old := "a: 1\nb: 2\nc: 3"   // no trailing newline
	newer := "a: 9\nb: 2\nc: 3" // no trailing newline
	got := unifiedDiff("f", []byte(old), []byte(newer))
	want := "--- a/f\n" +
		"+++ b/f\n" +
		"@@ -1,3 +1,3 @@\n" +
		"-a: 1\n" +
		"+a: 9\n" +
		" b: 2\n" +
		" c: 3\n" +
		"\\ No newline at end of file\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDiffBothSidesMissingNewlineOnDifferingLastLine(t *testing.T) {
	// The last line itself changed, and neither side has a trailing
	// newline — each side gets its own marker, since each is independently
	// missing one relative to a newline-terminated file.
	old := "a: 1\nb: 2"   // no trailing newline
	newer := "a: 1\nb: 9" // no trailing newline
	got := unifiedDiff("f", []byte(old), []byte(newer))
	want := "--- a/f\n" +
		"+++ b/f\n" +
		"@@ -1,2 +1,2 @@\n" +
		" a: 1\n" +
		"-b: 2\n" +
		"\\ No newline at end of file\n" +
		"+b: 9\n" +
		"\\ No newline at end of file\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDiffAbsolutePathHasNoDoubledSlash(t *testing.T) {
	got := unifiedDiff("/tmp/f.yaml", []byte("a: 1\n"), []byte("a: 2\n"))
	if !strings.HasPrefix(got, "--- /tmp/f.yaml\n+++ /tmp/f.yaml\n") {
		t.Fatalf("want unprefixed absolute path on both sides, got:\n%s", got)
	}
}

func TestUnifiedDiffFarApartChangesAreSeparateHunks(t *testing.T) {
	// 20 unchanged lines between two single-line changes is well beyond
	// 2*diffContext, so this must produce two hunks.
	var oldLines, newLines []string
	for i := 0; i < 30; i++ {
		oldLines = append(oldLines, "line")
		newLines = append(newLines, "line")
	}
	oldLines[0] = "old-start"
	newLines[0] = "newer-start"
	oldLines[29] = "old-end"
	newLines[29] = "newer-end"

	got := unifiedDiff("f", []byte(strings.Join(oldLines, "\n")+"\n"), []byte(strings.Join(newLines, "\n")+"\n"))
	if n := strings.Count(got, "@@ -"); n != 2 {
		t.Fatalf("want 2 hunks, got %d in:\n%s", n, got)
	}
}

func TestUnifiedDiffCloseChangesMergeIntoOneHunk(t *testing.T) {
	// Two single-line changes only 2 lines apart (well within 2*diffContext)
	// must merge into one hunk.
	old := "a\nb\nc\nd\ne\n"
	newer := "A\nb\nc\nd\nE\n"
	got := unifiedDiff("f", []byte(old), []byte(newer))
	if n := strings.Count(got, "@@ -"); n != 1 {
		t.Fatalf("want 1 merged hunk, got %d in:\n%s", n, got)
	}
}

func TestUnifiedDiffJSONOneLineChanged(t *testing.T) {
	old := "a: 1\nb: 2\nc: 3\n"
	newer := "a: 1\nb: 9\nc: 3\n"
	got := unifiedDiffJSON("cfg.yaml", []byte(old), []byte(newer))

	want := diffJSON{
		File:    "cfg.yaml",
		Changed: true,
		Hunks: []diffHunkJSON{{
			AStart: 1, ACount: 3, BStart: 1, BCount: 3,
			Lines: []diffLineJSON{
				{Op: "same", Text: "a: 1", ALine: 1, BLine: 1},
				{Op: "del", Text: "b: 2", ALine: 2},
				{Op: "add", Text: "b: 9", BLine: 2},
				{Op: "same", Text: "c: 3", ALine: 3, BLine: 3},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%#v\nwant:\n%#v", got, want)
	}
}

func TestUnifiedDiffJSONIdenticalIsUnchanged(t *testing.T) {
	got := unifiedDiffJSON("f", []byte("a: 1\n"), []byte("a: 1\n"))
	if got.Changed {
		t.Fatalf("want Changed=false for identical input, got %+v", got)
	}
	if got.Hunks == nil || len(got.Hunks) != 0 {
		t.Fatalf("want a non-nil empty Hunks slice, got %#v", got.Hunks)
	}
}

func TestUnifiedDiffJSONOmitsALineForAddedAndBLineForDeleted(t *testing.T) {
	got := unifiedDiffJSON("f", []byte("a: 1\n"), []byte("a: 1\nb: 2\n"))
	if len(got.Hunks) != 1 {
		t.Fatalf("want 1 hunk, got %#v", got.Hunks)
	}
	lines := got.Hunks[0].Lines
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %#v", lines)
	}
	added := lines[1]
	if added.Op != "add" || added.ALine != 0 || added.BLine != 2 {
		t.Fatalf("added line = %+v, want ALine=0 (omitted) BLine=2", added)
	}
}

func TestUnifiedDiffJSONNoNewlineFlag(t *testing.T) {
	// Both old and new lack a trailing newline: "a: 1" is a's true last
	// line, "b: 2" is b's — each side flagged independently.
	old := "a: 1"         // no trailing newline
	newer := "a: 1\nb: 2" // no trailing newline
	got := unifiedDiffJSON("f", []byte(old), []byte(newer))
	if len(got.Hunks) != 1 {
		t.Fatalf("want 1 hunk, got %#v", got.Hunks)
	}
	lines := got.Hunks[0].Lines
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %#v", lines)
	}
	if !lines[0].ANoNewline {
		t.Fatalf("want the \"a: 1\" line flagged ANoNewline (a's last line), got %+v", lines[0])
	}
	if !lines[1].BNoNewline {
		t.Fatalf("want the \"+b: 2\" line flagged BNoNewline (b's last line), got %+v", lines[1])
	}
}

// TestUnifiedDiffJSONNoNewlineNamesTheSide is the #119 regression: on a
// "same" line both sides are live, so a single merged boolean couldn't say
// which side was missing its trailing newline. Here only the old side is.
func TestUnifiedDiffJSONNoNewlineNamesTheSide(t *testing.T) {
	old := "a: 1"           // no trailing newline
	newer := "a: 1\nb: 2\n" // has one
	got := unifiedDiffJSON("f", []byte(old), []byte(newer))
	lines := got.Hunks[0].Lines
	same := lines[0]
	if same.Op != "same" {
		t.Fatalf("expected the first line to be context, got %+v", same)
	}
	if !same.ANoNewline {
		t.Fatalf("old side lacks a final newline, want ANoNewline; got %+v", same)
	}
	if same.BNoNewline {
		t.Fatalf("new side ends in a newline, want BNoNewline false; got %+v", same)
	}
}

// TestUnifiedDiffJSONNoNewlineOmittedWhenBothSidesTerminated keeps the
// omitempty tags meaningful: an ordinary newline-terminated pair sets
// neither flag.
func TestUnifiedDiffJSONNoNewlineOmittedWhenBothSidesTerminated(t *testing.T) {
	got := unifiedDiffJSON("f", []byte("a: 1\nb: 2\n"), []byte("a: 1\nb: 9\n"))
	for _, l := range got.Hunks[0].Lines {
		if l.ANoNewline || l.BNoNewline {
			t.Fatalf("both sides end in a newline, want neither flag set; got %+v", l)
		}
	}
}

// TestWriteHunkAndEncodeHunkJSONAgreeOnHeaderNumbers protects the hunkInfo
// refactor from silently diverging the text and JSON renderers: both must
// report the same @@ header numbers for the same input.
func TestWriteHunkAndEncodeHunkJSONAgreeOnHeaderNumbers(t *testing.T) {
	cases := []struct {
		name       string
		old, newer string
	}{
		{"one line changed", "a: 1\nb: 2\nc: 3\n", "a: 1\nb: 9\nc: 3\n"},
		{"append at end", "a: 1\n", "a: 1\nb: 2\n"},
		{"delete middle", "a: 1\nb: 2\nc: 3\n", "a: 1\nc: 3\n"},
		{"far apart changes", strings.Repeat("x\n", 30), "y\n" + strings.Repeat("x\n", 28) + "z\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := unifiedDiff("f", []byte(tc.old), []byte(tc.newer))
			j := unifiedDiffJSON("f", []byte(tc.old), []byte(tc.newer))

			headerRe := regexp.MustCompile(`@@ -(\d+),(\d+) \+(\d+),(\d+) @@`)
			matches := headerRe.FindAllStringSubmatch(text, -1)
			if len(matches) != len(j.Hunks) {
				t.Fatalf("text has %d headers, JSON has %d hunks", len(matches), len(j.Hunks))
			}
			for i, m := range matches {
				aStart, _ := strconv.Atoi(m[1])
				aCount, _ := strconv.Atoi(m[2])
				bStart, _ := strconv.Atoi(m[3])
				bCount, _ := strconv.Atoi(m[4])
				h := j.Hunks[i]
				if h.AStart != aStart || h.ACount != aCount || h.BStart != bStart || h.BCount != bCount {
					t.Fatalf("hunk %d: text header (%d,%d,%d,%d) vs JSON (%d,%d,%d,%d)",
						i, aStart, aCount, bStart, bCount, h.AStart, h.ACount, h.BStart, h.BCount)
				}
			}
		})
	}
}
