package cmd

import (
	"bytes"
	"testing"

	"go.yaml.in/yaml/v4"
)

// encoderReproduces encodes a scalar holding value in the three positions a block
// scalar can take — a mapping value, a sequence item, an explicit key — and
// reports whether each reads back as value.
func encoderReproduces(value string, style yaml.Style, indent int) bool {
	leaf := func() *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: style}
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	probe := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		str("v"), leaf(),
		str("s"), {Kind: yaml.SequenceNode, Content: []*yaml.Node{leaf()}},
		str("k"), {Kind: yaml.MappingNode, Content: []*yaml.Node{leaf(), {Kind: yaml.ScalarNode, Tag: "!!null", Value: "~"}}},
	}}
	var buf bytes.Buffer
	if encodeYAML(&buf, probe, indent) != nil {
		return false
	}
	var back yaml.Node
	if yaml.Unmarshal(buf.Bytes(), &back) != nil || len(back.Content) != 1 {
		return false
	}
	m := back.Content[0]
	if m.Kind != yaml.MappingNode || len(m.Content) != 6 {
		return false
	}
	s, k := m.Content[3], m.Content[5]
	if s.Kind != yaml.SequenceNode || len(s.Content) != 1 || k.Kind != yaml.MappingNode || len(k.Content) != 2 {
		return false
	}
	for _, g := range []*yaml.Node{m.Content[1], s.Content[0], k.Content[0]} {
		if g.Kind != yaml.ScalarNode || g.Value != value {
			return false
		}
	}
	return true
}

// TestEncoderWritesEveryScalarBack: nothing stands between the encoder and a
// re-render's text any more. yaml.v3 got about one case in ten of this wrong, so a
// guard quoted what it couldn't write back. yaml.v4 gets them right and the guard
// went (#389), but a value it broke would be refused at verification, exit 1 with
// nothing written, so this is what would say a yaml upgrade had brought them back.
// The values below that end in a blank line, or open with a space, are ones v3
// failed.
func TestEncoderWritesEveryScalarBack(t *testing.T) {
	pieces := []string{
		"a", " ", "  ", "\t", "\n", "\n\n", "#", "- ", ": ", "\r", "\u0085", " ", "é",
		"...", "---", "%", "!", "|", ">", "'", "\"", "\\", "x y", "~", "1", "?", "&a", "*a", "{", "[",
	}
	values := make([]string, 0, 9+(6+4*len(pieces))*len(pieces))
	values = append(values, "aa\n\n", "a\t\n\n", " a", " aa", " a\n", " a\n\n", "\none", "\none\n", "\ta\nz")
	for _, a := range pieces {
		values = append(values, a, a+"\n", "\n"+a, a+"\n\n", " "+a+"\n", "\t"+a+"\nz")
		for _, b := range pieces {
			values = append(values, a+b, a+"\n"+b, a+b+"\n", a+"\n\n"+b)
		}
	}
	styles := map[string]yaml.Style{"plain": 0, "literal": yaml.LiteralStyle, "folded": yaml.FoldedStyle}
	failed := 0
	for name, style := range styles {
		for _, v := range values {
			for indent := 2; indent <= 9; indent++ {
				if !encoderReproduces(v, style, indent) {
					if failed++; failed <= 20 {
						t.Errorf("%s %q at indent %d does not read back", name, v, indent)
					}
				}
			}
		}
	}
	if failed > 20 {
		t.Errorf("and %d more", failed-20)
	}
}
