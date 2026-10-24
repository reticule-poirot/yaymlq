// Package query resolves path expressions against decoded YAML values.
package query

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// ErrNotFound is returned when a non-wildcard path segment does not resolve.
var ErrNotFound = errors.New("path not found")

// NotFoundError carries the segment trail Run had resolved so far alongside
// ErrNotFound, for a caller that wants it structured instead of re-parsing
// Error()'s text — e.g. cmd's -o json error output. Path is the trail up to
// (and including) the segment that failed to resolve.
//
// Available lists the keys of the mapping the lookup missed, sorted, so a
// caller can tell the user what was there instead of making them run a
// second query to find out. It is set only when a key lookup against a
// mapping failed: an out-of-range index, a key into a list, or a scalar in
// the way all already say so in the message, and a key list there would be
// noise. It is never truncated — the caller decides what fits its output.
type NotFoundError struct {
	Path      []path.Segment
	Available []string
	err       error
}

func (e *NotFoundError) Error() string { return e.err.Error() }
func (e *NotFoundError) Unwrap() error { return e.err }

func notFoundf(trail []path.Segment, format string, args ...any) error {
	return &NotFoundError{Path: trail, err: fmt.Errorf(format, args...)}
}

// notFoundKeyf is notFoundf for the one case that can offer the caller a way
// out: a key that wasn't in a mapping whose other keys are right here.
func notFoundKeyf(trail []path.Segment, available []string, format string, args ...any) error {
	return &NotFoundError{Path: trail, Available: available, err: fmt.Errorf(format, args...)}
}

// Run walks doc following the given path expression and returns every value it
// resolves to.
//
// doc is expected to be the result of unmarshalling YAML into an `any`
// (map[string]any, []any, and scalars).
//
// A path without wildcards yields exactly one value, or ErrNotFound. A path
// containing a wildcard (`*`, `[]`, `[*]`) or a recursive descent (`**`)
// yields zero or more values in document order (map keys sorted); once one
// has matched, missing keys or out-of-range indices on individual branches
// are skipped rather than reported as errors.
//
// `**` matches at the node it is applied to as well as every node beneath it,
// so `**.x` finds everything `.x` finds and more, and a match nested inside
// another match is reported in its own right.
func Run(doc any, expr string) ([]any, error) {
	matches, err := run(doc, expr, false)
	if err != nil || matches == nil {
		return nil, err
	}
	out := make([]any, len(matches))
	for i, m := range matches {
		out[i] = m.Value
	}
	return out, nil
}

// Match is one resolved value together with the concrete path that reached
// it — every wildcard replaced by the key or index it actually matched.
type Match struct {
	Path  []path.Segment
	Value any
}

// RunMatches is Run, but each result carries its resolved path as well as
// its value. That is what turns a wildcard query into something `apply` can
// consume: the values alone say what matched, never where.
//
// Same resolution rules and same error contract as Run. Each Path is a copy,
// not a view into walk's working trail (see extend).
func RunMatches(doc any, expr string) ([]Match, error) {
	return run(doc, expr, true)
}

func run(doc any, expr string, keepPath bool) ([]Match, error) {
	segs, err := path.Parse(expr)
	if err != nil {
		return nil, err
	}
	var out []Match
	if err := walk(doc, segs, nil, false, keepPath, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func walk(cur any, segs, trail []path.Segment, lenient, keepPath bool, out *[]Match) error {
	if len(segs) == 0 {
		m := Match{Value: cur}
		if keepPath {
			// Copied, not referenced: trail's backing array is reused by
			// the siblings walked after this one (see extend).
			m.Path = slices.Clone(trail)
		}
		*out = append(*out, m)
		return nil
	}

	seg, rest := segs[0], segs[1:]

	switch {
	case seg.IsDescend:
		// `**` matches at the current node as well as beneath it, so `**.x`
		// is a strict superset of `.x` — the reading a caller expects, and
		// the one that states as a property a test can assert (#174).
		if err := walk(cur, rest, trail, true, keepPath, out); err != nil {
			return err
		}
		// Then the same descent at every child, in the order a single-level
		// wildcard already uses: document order, mappings sorted by key. The
		// current node is handled before its children, so a shallower match
		// is reported before a deeper one.
		//
		// Children are walked with segs rather than rest — that is what
		// makes this recursive — and a node that just matched is descended
		// into anyway, since an `x` nested inside a matched `x` is a
		// distinct result at a different depth, not a duplicate.
		//
		// The recursion is unbounded by design and still terminates: yaml.v3
		// refuses a self-referential anchor while parsing ("anchor 'x' value
		// contains itself"), so a decoded document is always a finite tree
		// and never a cyclic graph. TestDescendCannotHangOnACyclicAlias in
		// cmd pins that, since this walk's safety rests on it.
		switch c := cur.(type) {
		case map[string]any:
			for _, k := range sortedKeys(c) {
				if err := walk(c[k], segs, extend(trail, path.Segment{Key: k}), true, keepPath, out); err != nil {
					return err
				}
			}
		case map[any]any:
			for _, k := range sortedAnyKeys(c) {
				if err := walk(c[k], segs, extend(trail, path.Segment{Key: keyText(k)}), true, keepPath, out); err != nil {
					return err
				}
			}
		case []any:
			for i, v := range c {
				if err := walk(v, segs, extend(trail, path.Segment{Index: i, IsIndex: true}), true, keepPath, out); err != nil {
					return err
				}
			}
		}
		// A scalar has nothing beneath it, which is zero matches rather than
		// an error — descent fans out, so it follows the wildcard contract.
		return nil

	case seg.IsWildcard:
		switch c := cur.(type) {
		case map[string]any:
			keys := make([]string, 0, len(c))
			for k := range c {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if err := walk(c[k], rest, extend(trail, path.Segment{Key: k}), true, keepPath, out); err != nil {
					return err
				}
			}
		case map[any]any:
			// yaml.v3 decodes a mapping into this type instead of
			// map[string]any as soon as it has any non-string key (an int,
			// bool, or null key alongside ordinary string ones). Walk it the
			// same way, keyed by each key's string form.
			for _, k := range sortedAnyKeys(c) {
				if err := walk(c[k], rest, extend(trail, path.Segment{Key: keyText(k)}), true, keepPath, out); err != nil {
					return err
				}
			}
		case []any:
			for i, v := range c {
				if err := walk(v, rest, extend(trail, path.Segment{Index: i, IsIndex: true}), true, keepPath, out); err != nil {
					return err
				}
			}
		default:
			if lenient {
				return nil
			}
			return notFoundf(trail, "%w: %s: cannot wildcard over %s", ErrNotFound, path.Format(trail), kindName(cur))
		}
		return nil

	case seg.IsIndex:
		here := extend(trail, seg)
		list, ok := cur.([]any)
		if !ok {
			if lenient {
				return nil
			}
			return notFoundf(here, "%w: %s: expected a list, got %s", ErrNotFound, path.Format(here), kindName(cur))
		}
		idx := seg.Index
		if idx < 0 {
			idx += len(list)
		}
		if idx < 0 || idx >= len(list) {
			if lenient {
				return nil
			}
			return notFoundf(here, "%w: %s: index %d out of range (len %d)", ErrNotFound, path.Format(here), seg.Index, len(list))
		}
		return walk(list[idx], rest, here, lenient, keepPath, out)

	default:
		here := extend(trail, seg)
		switch m := cur.(type) {
		case map[string]any:
			v, ok := m[seg.Key]
			if !ok {
				if lenient {
					return nil
				}
				return notFoundKeyf(here, sortedKeys(m), "%w: %s", ErrNotFound, path.Format(here))
			}
			return walk(v, rest, here, lenient, keepPath, out)
		case map[any]any:
			// Same non-string-key case as the wildcard branch above: match
			// by the key's string form, since seg.Key is always a string
			// (path expressions have no syntax for a typed key).
			for k, v := range m {
				if keyText(k) == seg.Key {
					return walk(v, rest, here, lenient, keepPath, out)
				}
			}
			if lenient {
				return nil
			}
			return notFoundKeyf(here, stringFormKeys(m), "%w: %s", ErrNotFound, path.Format(here))
		default:
			if lenient {
				return nil
			}
			return notFoundf(here, "%w: %s: expected a mapping, got %s", ErrNotFound, path.Format(here), kindName(cur))
		}
	}
}

// kindName names a decoded value the way the editing half names a node, so
// "expected a mapping, got scalar" reads the same whether a read or an edit
// produced it — ymledit.kindName is the other half, and a null is a scalar
// node there too. #203 was this printing the Go type instead: "expected a
// list, got map[interface {}]interface {}".
//
// The `type` verb's vocabulary (object/array/string/...) is deliberately a
// different one: that is documented output in JSON's terms, not a message
// about what was standing in the way.
func kindName(v any) string {
	switch v.(type) {
	case map[string]any, map[any]any:
		return "mapping"
	case []any:
		return "list"
	default:
		// Every yaml.v3 scalar lands here: string, int, float, bool, nil,
		// time.Time.
		return "scalar"
	}
}

// keyText is a mapping key's string form — the spelling a path expression
// uses to address it, which is both what a key list offers and what a
// segment is matched against. Only nil needs saying: fmt.Sprint renders it
// "<nil>", which named a key no expression could resolve, so a document
// with a null key advertised one spelling and answered to another (#203).
// YAML spells that key `null`.
func keyText(k any) string {
	if k == nil {
		return "null"
	}
	return fmt.Sprint(k)
}

// sortedKeys returns m's keys in sorted order, for NotFoundError.Available.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stringFormKeys is sortedKeys for a mapping with non-string keys: each key
// in the string form a path expression uses to address it, which is what the
// caller would have to type to reach it.
func stringFormKeys(m map[any]any) []string {
	keys := make([]string, 0, len(m))
	for _, k := range sortedAnyKeys(m) {
		keys = append(keys, keyText(k))
	}
	return keys
}

// sortedAnyKeys returns m's keys in the same deterministic order a wildcard
// over a map[string]any uses (sorted), but keyed by each key's string form —
// m's keys are typed (int, bool, nil, ...), not necessarily strings.
func sortedAnyKeys(m map[any]any) []any {
	keys := make([]any, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyText(keys[i]) < keyText(keys[j]) })
	return keys
}

// extend returns trail with s appended, reusing trail's spare capacity via a
// plain append instead of always copying to a fresh array.
//
// That's safe specifically because walk is single-threaded and, on the
// first error any subtree produces, returns immediately without visiting
// any further siblings (every wildcard/map/list loop above is `if err :=
// walk(...); err != nil { return err }` — no continue past a failure). So a
// *NotFoundError's Path, once constructed, is never appended to afterward:
// nothing is still running that could grow into (and so overwrite) the
// same backing array. Below that error, deeper recursion is depth-first —
// one sibling's whole subtree completes (or errors, which unwinds
// everything above it) before the next sibling's extend runs — so a later
// sibling only ever overwrites positions an earlier, already-finished
// sibling no longer needs.
//
// RunMatches, which does keep a path past the walk, copies it out at the
// moment of the match rather than relying on the trail staying put.
//
// Cuts extend from O(depth) per call — O(depth²) total over a deep walk,
// amplified further by wildcard fan-out — down to amortized O(1).
func extend(trail []path.Segment, s path.Segment) []path.Segment {
	return append(trail, s)
}
