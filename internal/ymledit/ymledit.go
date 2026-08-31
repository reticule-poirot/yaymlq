// Package ymledit performs in-place edits on a YAML document tree while
// preserving comments, key order, and formatting.
package ymledit

import (
	"errors"
	"fmt"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"gopkg.in/yaml.v3"
)

// ErrUnsupported is returned for path shapes that Set and Delete cannot handle
// (wildcards, or the empty "whole document" path).
var ErrUnsupported = errors.New("unsupported path")

// Set walks doc along segs and replaces the value found there with value.
//
// doc may be a DocumentNode or a bare value node. Missing intermediate mapping
// keys are created (as `jq '.a.b = x'` would); a missing list index is an
// error. Wildcards are rejected. Comments attached to a replaced value node are
// carried over to value when value does not set its own.
func Set(doc *yaml.Node, segs []path.Segment, value *yaml.Node) error {
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to replace the whole document", ErrUnsupported)
	}

	cur := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		cur = doc.Content[0]
	}

	for i, seg := range segs {
		at := path.Format(segs[:i+1])
		last := i == len(segs)-1

		switch {
		case seg.IsWildcard:
			return fmt.Errorf("%w: %s: wildcards cannot be used with set", ErrUnsupported, at)

		case seg.IsIndex:
			if cur.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s: expected a list, got %s", at, kindName(cur.Kind))
			}
			idx := seg.Index
			if idx < 0 {
				idx += len(cur.Content)
			}
			if idx < 0 || idx >= len(cur.Content) {
				return fmt.Errorf("%s: index %d out of range (len %d)", at, seg.Index, len(cur.Content))
			}
			if last {
				cur.Content[idx] = carryComments(cur.Content[idx], value)
				return nil
			}
			cur = cur.Content[idx]

		default: // map key
			if isNullish(cur) {
				*cur = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			if cur.Kind != yaml.MappingNode {
				return fmt.Errorf("%s: expected a mapping, got %s", at, kindName(cur.Kind))
			}
			vi := findValueIndex(cur, seg.Key)
			if vi < 0 {
				keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.Key}
				valNode := value
				if !last {
					valNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				}
				cur.Content = append(cur.Content, keyNode, valNode)
				if last {
					return nil
				}
				cur = valNode
				continue
			}
			if last {
				cur.Content[vi] = carryComments(cur.Content[vi], value)
				return nil
			}
			cur = cur.Content[vi]
		}
	}
	return nil
}

// Delete walks doc along segs and removes the mapping key or list element found
// at the final segment.
//
// doc may be a DocumentNode or a bare value node. Wildcards are rejected, as is
// the empty path. A segment that does not resolve (missing key, out-of-range
// index) is an error — Delete does not silently no-op. Comments attached to the
// removed node go away with it; comments on its siblings are untouched.
func Delete(doc *yaml.Node, segs []path.Segment) error {
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to delete the whole document", ErrUnsupported)
	}

	cur := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return errors.New("the document is empty")
		}
		cur = doc.Content[0]
	}

	for i, seg := range segs {
		at := path.Format(segs[:i+1])
		last := i == len(segs)-1

		switch {
		case seg.IsWildcard:
			return fmt.Errorf("%w: %s: wildcards cannot be used with delete", ErrUnsupported, at)

		case seg.IsIndex:
			if cur.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s: expected a list, got %s", at, kindName(cur.Kind))
			}
			idx := seg.Index
			if idx < 0 {
				idx += len(cur.Content)
			}
			if idx < 0 || idx >= len(cur.Content) {
				return fmt.Errorf("%s: index %d out of range (len %d)", at, seg.Index, len(cur.Content))
			}
			if last {
				cur.Content = append(cur.Content[:idx], cur.Content[idx+1:]...)
				return nil
			}
			cur = cur.Content[idx]

		default: // map key
			if cur.Kind != yaml.MappingNode {
				return fmt.Errorf("%s: expected a mapping, got %s", at, kindName(cur.Kind))
			}
			vi := findValueIndex(cur, seg.Key)
			if vi < 0 {
				return fmt.Errorf("%s: no such key", at)
			}
			if last {
				cur.Content = append(cur.Content[:vi-1], cur.Content[vi+1:]...)
				return nil
			}
			cur = cur.Content[vi]
		}
	}
	return nil
}

// Append walks doc along segs to the node the path resolves to and adds value
// as its last element.
//
// The target must already exist and be a sequence — appending into a mapping or
// scalar, or through a missing key, is an error. Wildcards and the empty path
// are rejected. Comments on the existing elements are untouched.
func Append(doc *yaml.Node, segs []path.Segment, value *yaml.Node) error {
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to append to the whole document", ErrUnsupported)
	}

	cur := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return errors.New("the document is empty")
		}
		cur = doc.Content[0]
	}

	for i, seg := range segs {
		at := path.Format(segs[:i+1])

		switch {
		case seg.IsWildcard:
			return fmt.Errorf("%w: %s: wildcards cannot be used with append", ErrUnsupported, at)

		case seg.IsIndex:
			if cur.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s: expected a list, got %s", at, kindName(cur.Kind))
			}
			idx := seg.Index
			if idx < 0 {
				idx += len(cur.Content)
			}
			if idx < 0 || idx >= len(cur.Content) {
				return fmt.Errorf("%s: index %d out of range (len %d)", at, seg.Index, len(cur.Content))
			}
			cur = cur.Content[idx]

		default: // map key
			if cur.Kind != yaml.MappingNode {
				return fmt.Errorf("%s: expected a mapping, got %s", at, kindName(cur.Kind))
			}
			vi := findValueIndex(cur, seg.Key)
			if vi < 0 {
				return fmt.Errorf("%s: no such key", at)
			}
			cur = cur.Content[vi]
		}
	}

	if cur.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s: expected a list to append to, got %s", path.Format(segs), kindName(cur.Kind))
	}
	cur.Content = append(cur.Content, value)
	return nil
}

// ParseValue decodes a value string into a node suitable for Set. The string is
// parsed as YAML, so it may be a scalar ("8080", "true", "nginx:1.27"), a flow
// or block collection ("{a: 1}", "[1, 2]", "k:\n  v: 1"), or empty (-> null).
// When asString is true the value is taken verbatim as a !!str scalar.
func ParseValue(s string, asString bool) (*yaml.Node, error) {
	if asString {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return doc.Content[0], nil
}

func findValueIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

func carryComments(old, next *yaml.Node) *yaml.Node {
	if next.HeadComment == "" {
		next.HeadComment = old.HeadComment
	}
	if next.LineComment == "" {
		next.LineComment = old.LineComment
	}
	if next.FootComment == "" {
		next.FootComment = old.FootComment
	}
	return next
}

func isNullish(n *yaml.Node) bool {
	return n.Kind == 0 || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "list"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return "empty"
	}
}
