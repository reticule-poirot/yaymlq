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
type NotFoundError struct {
	Path []path.Segment
	err  error
}

func (e *NotFoundError) Error() string { return e.err.Error() }
func (e *NotFoundError) Unwrap() error { return e.err }

func notFoundf(trail []path.Segment, format string, args ...any) error {
	return &NotFoundError{Path: trail, err: fmt.Errorf(format, args...)}
}

// Run walks doc following the given path expression and returns every value it
// resolves to.
//
// doc is expected to be the result of unmarshalling YAML into an `any`
// (map[string]any, []any, and scalars).
//
// A path without wildcards yields exactly one value, or ErrNotFound. A path
// containing a wildcard (`*`, `[]`, `[*]`) yields zero or more values in
// document order (map keys sorted); once a wildcard has matched, missing keys
// or out-of-range indices on individual branches are skipped rather than
// reported as errors.
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
				if err := walk(c[k], rest, extend(trail, path.Segment{Key: fmt.Sprint(k)}), true, keepPath, out); err != nil {
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
			return notFoundf(trail, "%w: %s: cannot wildcard over %T", ErrNotFound, path.Format(trail), cur)
		}
		return nil

	case seg.IsIndex:
		here := extend(trail, seg)
		list, ok := cur.([]any)
		if !ok {
			if lenient {
				return nil
			}
			return notFoundf(here, "%w: %s: expected a list, got %T", ErrNotFound, path.Format(here), cur)
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
				return notFoundf(here, "%w: %s", ErrNotFound, path.Format(here))
			}
			return walk(v, rest, here, lenient, keepPath, out)
		case map[any]any:
			// Same non-string-key case as the wildcard branch above: match
			// by the key's string form, since seg.Key is always a string
			// (path expressions have no syntax for a typed key).
			for k, v := range m {
				if fmt.Sprint(k) == seg.Key {
					return walk(v, rest, here, lenient, keepPath, out)
				}
			}
			if lenient {
				return nil
			}
			return notFoundf(here, "%w: %s", ErrNotFound, path.Format(here))
		default:
			if lenient {
				return nil
			}
			return notFoundf(here, "%w: %s: expected a mapping, got %T", ErrNotFound, path.Format(here), cur)
		}
	}
}

// sortedAnyKeys returns m's keys in the same deterministic order a wildcard
// over a map[string]any uses (sorted), but keyed by each key's string form —
// m's keys are typed (int, bool, nil, ...), not necessarily strings.
func sortedAnyKeys(m map[any]any) []any {
	keys := make([]any, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
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
