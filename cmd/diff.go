package cmd

import (
	"encoding/json"
	"fmt"
	"io"
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

// diagIdx maps diagonal k (of the same parity as d, ranging over -d..d step
// 2 — d+1 values in all) to its position within round d's snapshot slice.
func diagIdx(d, k int) int { return (k + d) / 2 }

// myersTrace runs the forward greedy search over one full-width working
// array v (size O(N+M), allocated once and mutated in place — that part of
// the algorithm is already linear), and records, once per round d, only the
// d+1 diagonal endpoints v[k] for k in -d..d step 2 that round actually
// touched — not a copy of the whole working array. myersBacktrack walks
// this from the end to recover the actual edit script.
//
// That distinction is the whole memory story: snapshotting the full O(N+M)
// array on every one of the up to D rounds costs O(D*(N+M)) — quadratic in
// document size whenever a reformat (a different --indent, say) pushes D
// close to N+M, gigabytes on an ordinary-sized file. Recording only each
// round's own d+1 values costs sum(d+1 for d in 0..D) = O(D²) instead,
// matching the package doc's O(N+D²) complexity claim.
func myersTrace(a, b []string) [][]int {
	n, m := len(a), len(b)
	maxD := n + m
	v := make([]int, 2*maxD+1)
	trace := make([][]int, 0, maxD+1)

	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, d+1)
		done := false
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
			snapshot[diagIdx(d, k)] = x
			if x >= n && y >= m {
				done = true
			}
		}
		trace = append(trace, snapshot)
		if done {
			return trace
		}
	}
	return trace
}

// myersBacktrack walks trace from its last round back to the origin,
// recovering the edit script (in forward order) that the greedy search
// found but didn't record directly.
//
// trace[d] holds round d's own frontier (diagonals of parity d); backtracking
// FROM round d always needs round d-1's frontier (parity d-1, one round
// earlier — a direct consequence of how the forward search interleaves
// parities), so this reads trace[d-1], not trace[d]. Round 0 has no
// predecessor round: its frontier's origin is simply (0, 0).
func myersBacktrack(a, b []string, trace [][]int) []diffLine {
	n, m := len(a), len(b)
	x, y := n, m
	var rev []diffLine

	for d := len(trace) - 1; d >= 0; d-- {
		k := x - y

		var prevX, prevY int
		if d == 0 {
			prevX, prevY = 0, 0
		} else {
			v := trace[d-1]
			var prevK int
			if k == -d || (k != d && v[diagIdx(d-1, k-1)] < v[diagIdx(d-1, k+1)]) {
				prevK = k + 1
			} else {
				prevK = k - 1
			}
			prevX = v[diagIdx(d-1, prevK)]
			prevY = prevX - prevK
		}

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

// numberLines annotates ops with each line's preA/preB position, so both the
// text and JSON renderers can derive 1-indexed line numbers without
// recomputing this walk themselves.
func numberLines(ops []diffLine) []numberedLine {
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
	return nums
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
	nums := numberLines(ops)

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
	out.WriteString("--- ")
	out.WriteString(aName)
	out.WriteString("\n+++ ")
	out.WriteString(bName)
	out.WriteString("\n")
	for _, h := range hunks {
		writeHunk(&out, nums, h, len(a), len(b), aNL, bNL)
	}
	return out.String()
}

// hunkInfo is one hunk's computed unified-diff header numbers plus the
// half-open [start,end) index range into nums spanning its lines. Shared by
// the text (writeHunk) and JSON (encodeHunkJSON) renderers so both agree on
// the same header-number arithmetic instead of duplicating it.
type hunkInfo struct {
	start, end     int
	aStart, aCount int
	bStart, bCount int
}

// computeHunkInfo derives a hunkInfo's header numbers from nums[start:end].
func computeHunkInfo(nums []numberedLine, start, end int) hunkInfo {
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

	return hunkInfo{start, end, aStart, aCount, bStart, bCount}
}

// diffHunks groups nums's change regions into hunks: each change carries up
// to diffContext lines of surrounding same-context, and hunks whose context
// windows touch or overlap are merged into one, the same way `diff -u`'s
// hunk grouping works.
func diffHunks(nums []numberedLine) []hunkInfo {
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

	var hunks []hunkInfo
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
		hunks = append(hunks, computeHunkInfo(nums, start, i))
	}
	return hunks
}

// writeHunk renders one @@ ... @@ header and its lines. aLen/bLen are the
// full line counts of each side, used only to know whether this hunk's last
// line is truly the file's last line (for the no-final-newline marker).
func writeHunk(out *strings.Builder, nums []numberedLine, h hunkInfo, aLen, bLen int, aNL, bNL bool) {
	fmtHeader(out, h.aStart, h.aCount, h.bStart, h.bCount)

	for idx := h.start; idx < h.end; idx++ {
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
	if lineNoNewline(n, aLen, bLen, aNL, bNL) {
		out.WriteString(marker)
	}
}

// lineNoNewline reports whether n is the true last line of a or b (whichever
// side(s) it touches) and that side lacks a trailing newline — the same
// condition writeNoNewlineMarker renders as text, reused by the JSON
// encoder's per-line NoNewline flag.
func lineNoNewline(n numberedLine, aLen, bLen int, aNL, bNL bool) bool {
	aLast := n.kind != opAdd && n.preA+1 == aLen
	bLast := n.kind != opDel && n.preB+1 == bLen
	return (aLast && !aNL) || (bLast && !bNL)
}

// diffJSON is --diff --diff-format json's top-level shape. Hunks is always
// non-nil (possibly empty) so "no change" still marshals to a valid JSON
// object, unlike unifiedDiff's empty-string convention.
type diffJSON struct {
	File    string         `json:"file"`
	Changed bool           `json:"changed"`
	Hunks   []diffHunkJSON `json:"hunks"`
}

type diffHunkJSON struct {
	AStart int            `json:"aStart"`
	ACount int            `json:"aCount"`
	BStart int            `json:"bStart"`
	BCount int            `json:"bCount"`
	Lines  []diffLineJSON `json:"lines"`
}

// diffLineJSON is one rendered line. ALine is omitted for an added line (no
// position on the "a" side), BLine omitted for a deleted line — so a
// consumer never has to re-derive either by counting from the hunk header.
type diffLineJSON struct {
	Op        string `json:"op"` // "same" | "add" | "del"
	Text      string `json:"text"`
	ALine     int    `json:"aLine,omitempty"`
	BLine     int    `json:"bLine,omitempty"`
	NoNewline bool   `json:"noNewline,omitempty"`
}

func opName(k opKind) string {
	switch k {
	case opAdd:
		return "add"
	case opDel:
		return "del"
	default:
		return "same"
	}
}

// unifiedDiffJSON mirrors unifiedDiff's pipeline, returning oldData vs
// newData's diff as a diffJSON value instead of rendering unified-diff text.
func unifiedDiffJSON(name string, oldData, newData []byte) diffJSON {
	a, aNL := splitLines(oldData)
	b, bNL := splitLines(newData)
	ops := myersDiff(a, b)
	ops = splitTrailingNewlineChange(ops, aNL, bNL)
	nums := numberLines(ops)

	hunks := diffHunks(nums)
	out := diffJSON{File: name, Changed: len(hunks) > 0, Hunks: []diffHunkJSON{}}
	for _, h := range hunks {
		out.Hunks = append(out.Hunks, encodeHunkJSON(nums, h, len(a), len(b), aNL, bNL))
	}
	return out
}

// encodeHunkJSON renders one hunk into its JSON shape, mirroring writeHunk.
func encodeHunkJSON(nums []numberedLine, h hunkInfo, aLen, bLen int, aNL, bNL bool) diffHunkJSON {
	out := diffHunkJSON{AStart: h.aStart, ACount: h.aCount, BStart: h.bStart, BCount: h.bCount}
	for idx := h.start; idx < h.end; idx++ {
		n := nums[idx]
		l := diffLineJSON{Op: opName(n.kind), Text: n.text, NoNewline: lineNoNewline(n, aLen, bLen, aNL, bNL)}
		if n.kind != opAdd {
			l.ALine = n.preA + 1
		}
		if n.kind != opDel {
			l.BLine = n.preB + 1
		}
		out.Lines = append(out.Lines, l)
	}
	return out
}

// writeDiffJSON writes oldData vs newData's diff as compact single-line JSON
// plus a trailing newline, the same style as writeJSONError in jsonerr.go —
// one structured event per invocation, not a pretty-printed data dump.
func writeDiffJSON(w io.Writer, name string, oldData, newData []byte) error {
	data, err := json.Marshal(unifiedDiffJSON(name, oldData, newData))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// validDiffFormat reports whether format is a recognized --diff-format
// value.
func validDiffFormat(format string) bool {
	return format == "text" || format == "json"
}
