// Package query resolves path expressions against decoded YAML values.
package query

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned when a path segment does not resolve to a value.
var ErrNotFound = errors.New("path not found")

// Run walks doc following the given path expression and returns the value found.
//
// doc is expected to be the result of unmarshalling YAML into an `any`
// (map[string]any, []any, and scalars).
func Run(doc any, expr string) (any, error) {
	segs, err := parsePath(expr)
	if err != nil {
		return nil, err
	}

	cur := doc
	for depth, seg := range segs {
		switch {
		case seg.isIndex:
			list, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("%w: %s: expected a list, got %T", ErrNotFound, pathTo(segs, depth), cur)
			}
			idx := seg.index
			if idx < 0 {
				idx += len(list)
			}
			if idx < 0 || idx >= len(list) {
				return nil, fmt.Errorf("%w: %s: index %d out of range (len %d)", ErrNotFound, pathTo(segs, depth), seg.index, len(list))
			}
			cur = list[idx]
		default:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: %s: expected a mapping, got %T", ErrNotFound, pathTo(segs, depth), cur)
			}
			v, ok := m[seg.key]
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrNotFound, pathTo(segs, depth))
			}
			cur = v
		}
	}
	return cur, nil
}

func pathTo(segs []segment, depth int) string {
	var b []byte
	for i := 0; i <= depth && i < len(segs); i++ {
		s := segs[i]
		if s.isIndex {
			b = append(b, s.String()...)
			continue
		}
		if len(b) > 0 {
			b = append(b, '.')
		}
		b = append(b, s.String()...)
	}
	return string(b)
}
