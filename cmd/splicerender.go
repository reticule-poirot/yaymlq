package cmd

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Rendering the text an insertion brings in. Nothing the source already holds
// is rendered: only nodes the edit created, which have no source spelling to
// keep. Block collections are written here rather than by the encoder, which
// cannot be told to write a sequence indentless (yaml.v3 v3.0.1's Encoder has
// SetIndent and nothing else), so new text could not match a source written
// that way. Scalars and flow collections are still the encoder's spelling.

// checkNew declines a node the renderer cannot write faithfully: an alias, a
// collection with an anchor or a tag of its own, or a comment. top allows
// comments on n itself, which ymledit carries over from the value n replaced
// and which stay where the source has them.
func checkNew(n *yaml.Node, top bool) error {
	if n.Kind == yaml.AliasNode {
		return decline("an alias the edit brought in")
	}
	if !top && (n.HeadComment != "" || n.LineComment != "" || n.FootComment != "") {
		return decline("a comment the edit brought in")
	}
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		if n.Anchor != "" {
			return decline("an anchored collection the edit brought in")
		}
		if n.Tag != "" && n.Tag != "!!map" && n.Tag != "!!seq" {
			return decline("a tagged collection the edit brought in")
		}
	}
	for _, c := range n.Content {
		if err := checkNew(c, false); err != nil {
			return err
		}
	}
	return nil
}

// render writes n as lines relative to column 0. inline means it is one line
// that goes where a scalar would: after a key's colon, or after a dash.
func (s *splicer) render(n *yaml.Node) (lines []string, inline bool, err error) {
	switch {
	case n.Kind == yaml.ScalarNode:
		t, err := renderScalar(n, false)
		return []string{t}, true, err
	case n.Style&yaml.FlowStyle != 0 || len(n.Content) == 0:
		t, err := flowLine(n)
		return []string{t}, true, err
	case n.Kind == yaml.MappingNode:
		var out []string
		for i := 0; i+1 < len(n.Content); i += 2 {
			ls, err := s.renderPair(n.Content[i], n.Content[i+1])
			if err != nil {
				return nil, false, err
			}
			out = append(out, ls...)
		}
		return out, false, nil
	case n.Kind == yaml.SequenceNode:
		var out []string
		for _, it := range n.Content {
			ls, err := s.renderItem(it)
			if err != nil {
				return nil, false, err
			}
			out = append(out, ls...)
		}
		return out, false, nil
	}
	return nil, false, decline("rendering a %v", n.Kind)
}

// renderPair writes one mapping entry.
func (s *splicer) renderPair(k, v *yaml.Node) ([]string, error) {
	if k.Kind != yaml.ScalarNode {
		return nil, decline("a collection key")
	}
	kt, err := renderScalar(k, true)
	if err != nil {
		return nil, err
	}
	vl, inline, err := s.render(v)
	if err != nil {
		return nil, err
	}
	if inline {
		return []string{kt + ": " + vl[0]}, nil
	}
	return append([]string{kt + ":"}, indentLines(vl, s.childIndent(v))...), nil
}

// renderItem writes one sequence item: its first line after the dash, the
// rest under it.
func (s *splicer) renderItem(v *yaml.Node) ([]string, error) {
	vl, _, err := s.render(v)
	if err != nil {
		return nil, err
	}
	return append([]string{"- " + vl[0]}, indentLines(vl[1:], 2)...), nil
}

// childIndent is how far a block value is indented past its key: the indent
// the edit uses, or none for a sequence when the source writes them
// indentless.
func (s *splicer) childIndent(v *yaml.Node) int {
	if v.Kind == yaml.SequenceNode && s.indentless {
		return 0
	}
	return s.indent
}

// flowLine is the encoder's one-line flow spelling of a collection.
func flowLine(n *yaml.Node) (string, error) {
	c := *n
	c.Style |= yaml.FlowStyle
	c.Line, c.Column = 0, 0
	b, err := yaml.Marshal(&c)
	if err != nil {
		return "", decline("render: %v", err)
	}
	text := strings.TrimSuffix(string(b), "\n")
	if strings.ContainsAny(text, "\n\r") {
		return "", decline("a flow rendering over more than one line")
	}
	return text, nil
}

// flowEntry is the spelling of a new entry inside a flow collection: an item
// for a sequence (v nil), or a pair. It is rendered inside a one-entry flow
// collection and unwrapped, so the encoder quotes it for flow context, where
// a plain scalar may not hold a ",", "[", "]", "{" or "}".
func flowEntry(k, v *yaml.Node) (string, error) {
	wrap := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, Content: []*yaml.Node{k}}
	if v != nil {
		wrap = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle, Content: []*yaml.Node{k, v}}
	}
	t, err := flowLine(wrap)
	if err != nil {
		return "", err
	}
	if len(t) < 2 {
		return "", decline("an empty flow rendering")
	}
	return t[1 : len(t)-1], nil
}

// indentLines prefixes every line with n spaces.
func indentLines(lines []string, n int) []string {
	return indent(lines, n, false)
}

// indent is indentLines/indentNonEmpty's shared loop: pad every line with n
// spaces, skipping a blank line when skipEmpty is set.
func indent(lines []string, n int, skipEmpty bool) []string {
	pad := strings.Repeat(" ", n)
	out := make([]string, len(lines))
	for i, l := range lines {
		if l != "" || !skipEmpty {
			l = pad + l
		}
		out[i] = l
	}
	return out
}

// detectIndentless reports whether the source writes a block sequence that is
// a mapping value at its key's column. The first such sequence in the stream
// decides; with none, sequences are indented, the encoder's default.
func detectIndentless(docs []*yaml.Node, snap map[*yaml.Node]shallow) bool {
	found, indentless := false, false
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if found {
			return
		}
		if _, ok := snap[n]; !ok {
			return
		}
		for i, c := range n.Content {
			if n.Kind == yaml.MappingNode && i%2 == 1 && c.Kind == yaml.SequenceNode && c.Style&yaml.FlowStyle == 0 && len(c.Content) > 0 {
				if _, ok := snap[c]; ok && c.Line > 0 && n.Content[i-1].Line > 0 {
					found, indentless = true, c.Column == n.Content[i-1].Column
					return
				}
			}
			walk(c)
			if found {
				return
			}
		}
	}
	for _, d := range docs {
		walk(d)
	}
	return indentless
}
