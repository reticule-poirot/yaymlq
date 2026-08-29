// Package query resolves path expressions against decoded YAML values.
package query

import (
	"errors"
	"fmt"
	"sort"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

// ErrNotFound is returned when a non-wildcard path segment does not resolve.
var ErrNotFound = errors.New("path not found")

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
	segs, err := path.Parse(expr)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := walk(doc, segs, nil, false, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func walk(cur any, segs, trail []path.Segment, lenient bool, out *[]any) error {
	if len(segs) == 0 {
		*out = append(*out, cur)
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
				if err := walk(c[k], rest, extend(trail, path.Segment{Key: k}), true, out); err != nil {
					return err
				}
			}
		case []any:
			for i, v := range c {
				if err := walk(v, rest, extend(trail, path.Segment{Index: i, IsIndex: true}), true, out); err != nil {
					return err
				}
			}
		default:
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: cannot wildcard over %T", ErrNotFound, path.Format(trail), cur)
		}
		return nil

	case seg.IsIndex:
		here := extend(trail, seg)
		list, ok := cur.([]any)
		if !ok {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: expected a list, got %T", ErrNotFound, path.Format(here), cur)
		}
		idx := seg.Index
		if idx < 0 {
			idx += len(list)
		}
		if idx < 0 || idx >= len(list) {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: index %d out of range (len %d)", ErrNotFound, path.Format(here), seg.Index, len(list))
		}
		return walk(list[idx], rest, here, lenient, out)

	default:
		here := extend(trail, seg)
		m, ok := cur.(map[string]any)
		if !ok {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: expected a mapping, got %T", ErrNotFound, path.Format(here), cur)
		}
		v, ok := m[seg.Key]
		if !ok {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s", ErrNotFound, path.Format(here))
		}
		return walk(v, rest, here, lenient, out)
	}
}

// extend returns trail with s appended, always on a fresh backing array so
// sibling branches of a wildcard never clobber each other's path.
func extend(trail []path.Segment, s path.Segment) []path.Segment {
	out := make([]path.Segment, len(trail)+1)
	copy(out, trail)
	out[len(trail)] = s
	return out
}
