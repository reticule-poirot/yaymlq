package cmd

import (
	"bytes"
	"io"

	"go.yaml.in/yaml/v4"
)

// Every encoding of a node, and of a whole output value, goes through this file,
// for the two things yaml.v4's own entry points get wrong for this program:
// NewEncoder wraps a long scalar at 80 columns, and encoding a node strips the
// tags of the node it is handed (upstream #471), which is the tree verification
// later compares against. yaml.Marshal on a plain value has neither problem.

// encodeYAML writes v as one YAML document at the given indent, unwrapped. An
// indent outside the emitter's 2-9 falls back to 2, which is what the emitter
// itself does with one and what WithIndent refuses with an error instead.
func encodeYAML(w io.Writer, v any, indent int) error {
	if indent < 2 || indent > 9 {
		indent = 2
	}
	d, err := yaml.NewDumper(w, yaml.WithV3Defaults(), yaml.WithLineWidth(-1), yaml.WithIndent(indent))
	if err != nil {
		return err
	}
	if n, ok := v.(*yaml.Node); ok {
		v = cloneNode(n, map[*yaml.Node]*yaml.Node{})
	}
	if err := d.Dump(v); err != nil {
		return err
	}
	return d.Close()
}

// marshalNode is encodeYAML to a buffer at yaml.Marshal's default indent of 4.
func marshalNode(n *yaml.Node) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeYAML(&b, n, 4); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// cloneNode deep-copies n. An alias points at the copy of its anchor rather
// than the original, seen also being what stops a cycle through one.
func cloneNode(n *yaml.Node, seen map[*yaml.Node]*yaml.Node) *yaml.Node {
	if c, ok := seen[n]; ok {
		return c
	}
	c := *n
	seen[n] = &c
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, ch := range n.Content {
			c.Content[i] = cloneNode(ch, seen)
		}
	}
	if n.Alias != nil {
		c.Alias = cloneNode(n.Alias, seen)
	}
	return &c
}
