package cmd

import (
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
