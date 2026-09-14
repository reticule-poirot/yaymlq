package cmd

import (
	"strconv"
	"strings"
)

// diffContext is the number of unchanged lines of context shown around each
// change, and the maximum gap between two change regions before they're
// rendered as separate hunks — the same defaults `diff -u`/`git diff` use.
const diffContext = 3

type opKind byte

const (
	opSame opKind = ' '
	opAdd  opKind = '+'
	opDel  opKind = '-'
)

type diffLine struct {
	kind opKind
	text string
}

// myersDiff computes the shortest edit script turning a into b — add/delete
// only, no substitutions — using Myers' O(ND) algorithm (D = the edit
// distance, not len(a)*len(b)). That matters here: set/append/delete/rename
// usually change a handful of lines in a document that itself might be
// large, and a naive O(N*M) LCS table would be quadratic in the *document*
// size regardless of how small the actual edit is.
func myersDiff(a, b []string) []diffLine {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	trace := myersTrace(a, b)
	return myersBacktrack(a, b, trace)
}

// myersTrace runs the forward greedy search, recording a snapshot of the
// diagonal-endpoint array v at the start of every round d. myersBacktrack
// walks this from the end to recover the actual edit script.
func myersTrace(a, b []string) [][]int {
	n, m := len(a), len(b)
	maxD := n + m
	v := make([]int, 2*maxD+1)
	trace := make([][]int, 0, maxD+1)

	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[maxD+k-1] < v[maxD+k+1]) {
				x = v[maxD+k+1]
			} else {
				x = v[maxD+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[maxD+k] = x
			if x >= n && y >= m {
				return trace
			}
		}
	}
	return trace
}

// myersBacktrack walks trace from its last round back to the origin,
// recovering the edit script (in forward order) that the greedy search
// found but didn't record directly.
func myersBacktrack(a, b []string, trace [][]int) []diffLine {
	n, m := len(a), len(b)
	maxD := n + m
	x, y := n, m
	var rev []diffLine

	for d := len(trace) - 1; d >= 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[maxD+k-1] < v[maxD+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[maxD+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			rev = append(rev, diffLine{opSame, a[x-1]})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, diffLine{opAdd, b[y-1]})
			} else {
				rev = append(rev, diffLine{opDel, a[x-1]})
			}
		}
		x, y = prevX, prevY
	}

	out := make([]diffLine, len(rev))
	for i, l := range rev {
		out[len(rev)-1-i] = l
	}
	return out
}

// splitTrailingNewlineChange makes a change to only the trailing-newline
// status visible in the edit script, even when it's otherwise a no-op.
//
// myersDiff compares line *text* only, so when a's and b's last lines are
// textually equal, its last op is opSame regardless of aNL/bNL — a source
// missing its final newline that gets one added by the encoder (a's and b's
// lines are otherwise identical) would diff as "no change" even though the
// write does change the file's bytes. Splitting that trailing opSame into a
// same-text opDel+opAdd pair makes the two sides' differing newline status
// visible, the same way `diff -u` itself renders a newline-only change: as
// a remove-and-reinsert of the one line whose bytes differ.
//
// When aNL and bNL already agree, or the last two lines already differ in
// text (last op isn't opSame — genuinely different content, not just a
// newline), there's nothing to do.
func splitTrailingNewlineChange(ops []diffLine, aNL, bNL bool) []diffLine {
	if aNL == bNL || len(ops) == 0 {
		return ops
	}
	last := ops[len(ops)-1]
	if last.kind != opSame {
		return ops
	}
	out := make([]diffLine, len(ops)+1)
	copy(out, ops[:len(ops)-1])
	out[len(ops)-1] = diffLine{opDel, last.text}
	out[len(ops)] = diffLine{opAdd, last.text}
	return out
}

// splitLines splits data into lines (without their terminating "\n"), and
// reports whether the input's last line was itself newline-terminated —
// needed so the unified diff can print "\ No newline at end of file" the
// same way `diff -u` does, rather than silently dropping the distinction.
func splitLines(data []byte) (lines []string, endsInNewline bool) {
	if len(data) == 0 {
		return nil, true
	}
	s := string(data)
	endsInNewline = strings.HasSuffix(s, "\n")
	if endsInNewline {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n"), endsInNewline
}

// numberedLine is a diffLine plus the count of a/b lines already consumed
// *before* it (0-indexed) — enough to derive both its own 1-indexed line
// number on whichever side(s) it touches, and a hunk's starting line
// number on a side even when the hunk opens on an add/del.
type numberedLine struct {
	diffLine
	preA, preB int
}

// unifiedDiff renders a POSIX-style unified diff of oldData vs newData,
// labeled a/<name> and b/<name> (git's convention for "one file, two
// states", since that's exactly what an in-place edit's before/after is).
// Identical input produces an empty string, matching `diff -u` on two
// identical files.
func unifiedDiff(name string, oldData, newData []byte) string {
	a, aNL := splitLines(oldData)
	b, bNL := splitLines(newData)
	ops := myersDiff(a, b)
	ops = splitTrailingNewlineChange(ops, aNL, bNL)

	nums := make([]numberedLine, len(ops))
	aPos, bPos := 0, 0
	for i, op := range ops {
		nums[i] = numberedLine{op, aPos, bPos}
		switch op.kind {
		case opSame:
			aPos++
			bPos++
		case opDel:
			aPos++
		case opAdd:
			bPos++
		}
	}

	hunks := diffHunks(nums)
	if len(hunks) == 0 {
		return ""
	}

	// git's a/ b/ prefix reads oddly doubled against an absolute path
	// ("a//tmp/x"); only apply it to a relative one.
	aName, bName := name, name
	if !strings.HasPrefix(name, "/") {
		aName, bName = "a/"+name, "b/"+name
	}

	var out strings.Builder
	out.WriteString("--- " + aName + "\n")
	out.WriteString("+++ " + bName + "\n")
	for _, h := range hunks {
		writeHunk(&out, nums, h, len(a), len(b), aNL, bNL)
	}
	return out.String()
}

// diffHunks groups nums's change regions into hunks: each change carries up
// to diffContext lines of surrounding same-context, and hunks whose context
// windows touch or overlap are merged into one, the same way `diff -u`'s
// hunk grouping works. Returned as half-open [start, end) index ranges.
func diffHunks(nums []numberedLine) [][2]int {
	include := make([]bool, len(nums))
	for i, n := range nums {
		if n.kind == opSame {
			continue
		}
		include[i] = true
		for d := 1; d <= diffContext; d++ {
			if i-d >= 0 {
				include[i-d] = true
			}
			if i+d < len(nums) {
				include[i+d] = true
			}
		}
	}

	var hunks [][2]int
	i := 0
	for i < len(nums) {
		if !include[i] {
			i++
			continue
		}
		start := i
		for i < len(nums) && include[i] {
			i++
		}
		hunks = append(hunks, [2]int{start, i})
	}
	return hunks
}

// writeHunk renders one @@ ... @@ header and its lines. aLen/bLen are the
// full line counts of each side, used only to know whether this hunk's last
// line is truly the file's last line (for the no-final-newline marker).
func writeHunk(out *strings.Builder, nums []numberedLine, h [2]int, aLen, bLen int, aNL, bNL bool) {
	start, end := h[0], h[1]

	var aCount, bCount int
	for _, n := range nums[start:end] {
		if n.kind != opAdd {
			aCount++
		}
		if n.kind != opDel {
			bCount++
		}
	}

	// GNU diff's convention for a zero-length side (a pure insertion or
	// deletion, no context on that side): the reported start is the
	// position immediately before it — 0 when that's the very first line.
	aStart := nums[start].preA
	if aCount > 0 {
		aStart++
	}
	bStart := nums[start].preB
	if bCount > 0 {
		bStart++
	}

	fmtHeader(out, aStart, aCount, bStart, bCount)

	for idx := start; idx < end; idx++ {
		n := nums[idx]
		out.WriteByte(byte(n.kind))
		out.WriteString(n.text)
		out.WriteByte('\n')
		writeNoNewlineMarker(out, n, aLen, bLen, aNL, bNL)
	}
}

func fmtHeader(out *strings.Builder, aStart, aCount, bStart, bCount int) {
	out.WriteString("@@ -")
	out.WriteString(strconv.Itoa(aStart))
	out.WriteByte(',')
	out.WriteString(strconv.Itoa(aCount))
	out.WriteString(" +")
	out.WriteString(strconv.Itoa(bStart))
	out.WriteByte(',')
	out.WriteString(strconv.Itoa(bCount))
	out.WriteString(" @@\n")
}

// writeNoNewlineMarker appends diff -u's "\ No newline at end of file" note
// right after whichever rendered line is the true last line of a or b, when
// that side wasn't itself newline-terminated. Checked per line rather than
// only at the diff's final line: a's and b's last lines can render at
// different positions (e.g. the last line changed: the old one removed,
// immediately followed by the new one added), and each side's marker only
// depends on where *that side's* content ends, not on the other side's.
func writeNoNewlineMarker(out *strings.Builder, n numberedLine, aLen, bLen int, aNL, bNL bool) {
	const marker = "\\ No newline at end of file\n"
	aLast := n.kind != opAdd && n.preA+1 == aLen
	bLast := n.kind != opDel && n.preB+1 == bLen
	if (aLast && !aNL) || (bLast && !bNL) {
		out.WriteString(marker)
	}
}
