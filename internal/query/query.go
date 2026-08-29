// Package query resolves path expressions against decoded YAML values.
package query

import (
	"errors"
	"fmt"
	"sort"
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
	segs, err := parsePath(expr)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := walk(doc, segs, nil, false, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func walk(cur any, segs, trail []segment, lenient bool, out *[]any) error {
	if len(segs) == 0 {
		*out = append(*out, cur)
		return nil
	}

	seg, rest := segs[0], segs[1:]

	switch {
	case seg.isWildcard:
		switch c := cur.(type) {
		case map[string]any:
			keys := make([]string, 0, len(c))
			for k := range c {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if err := walk(c[k], rest, extend(trail, segment{key: k}), true, out); err != nil {
					return err
				}
			}
		case []any:
			for i, v := range c {
				if err := walk(v, rest, extend(trail, segment{index: i, isIndex: true}), true, out); err != nil {
					return err
				}
			}
		default:
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: cannot wildcard over %T", ErrNotFound, pathString(trail), cur)
		}
		return nil

	case seg.isIndex:
		here := extend(trail, seg)
		list, ok := cur.([]any)
		if !ok {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: expected a list, got %T", ErrNotFound, pathString(here), cur)
		}
		idx := seg.index
		if idx < 0 {
			idx += len(list)
		}
		if idx < 0 || idx >= len(list) {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: index %d out of range (len %d)", ErrNotFound, pathString(here), seg.index, len(list))
		}
		return walk(list[idx], rest, here, lenient, out)

	default:
		here := extend(trail, seg)
		m, ok := cur.(map[string]any)
		if !ok {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s: expected a mapping, got %T", ErrNotFound, pathString(here), cur)
		}
		v, ok := m[seg.key]
		if !ok {
			if lenient {
				return nil
			}
			return fmt.Errorf("%w: %s", ErrNotFound, pathString(here))
		}
		return walk(v, rest, here, lenient, out)
	}
}

// extend returns trail with s appended, always on a fresh backing array so
// sibling branches of a wildcard never clobber each other's path.
func extend(trail []segment, s segment) []segment {
	out := make([]segment, len(trail)+1)
	copy(out, trail)
	out[len(trail)] = s
	return out
}

// pathString renders a segment trail as a readable path like `a.b[0].c`.
func pathString(segs []segment) string {
	var b []byte
	for _, s := range segs {
		if s.isIndex {
			b = append(b, s.String()...)
			continue
		}
		if len(b) > 0 {
			b = append(b, '.')
		}
		b = append(b, s.String()...)
	}
	if len(b) == 0 {
		return "."
	}
	return string(b)
}
