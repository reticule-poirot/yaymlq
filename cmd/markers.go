package cmd

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Every stand-in marker the fidelity layer hands the encoder — a block
// scalar's, a flow collection's, a quoted scalar's, a lone dash's — is found
// again in the output by its core and swapped for something else. So a marker
// has to collide with nothing else the encoder writes, and that is decided
// here, once, for all four.
//
// Against the source alone was not enough (#286): an edit brings in values,
// keys, tags, anchors and comments the source never held, and one spelled like
// a marker was replaced by another node's text — or by nothing — on restore.
// So the corpus is the source plus every string the mutated trees carry. The
// source stays in because restored text re-enters the output before the later
// restores scan it. The tree's fields are enough on their own: a prefix is
// letters, "z"s and digits, and the encoder neither splits nor joins such a
// run across fields, so one in the output came whole from a single field.

// markerCorpus is the text a marker must not occur in.
type markerCorpus struct{ texts [][]byte }

// newMarkerCorpus collects source and the six string fields of every node in
// docs. An alias's name is its Value; its target is reached through Content
// elsewhere, so Alias is not followed.
func newMarkerCorpus(source []byte, docs []*yaml.Node) *markerCorpus {
	c := &markerCorpus{texts: [][]byte{source}}
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		for _, s := range [...]string{n.Value, n.Anchor, n.Tag, n.HeadComment, n.LineComment, n.FootComment} {
			if s != "" {
				c.texts = append(c.texts, []byte(s))
			}
		}
		for _, k := range n.Content {
			walk(k)
		}
	}
	for _, d := range docs {
		walk(d)
	}
	return c
}

// prefix extends core with "z"s until it occurs in none of the corpus. That is
// conservative rather than exact — a text containing the prefix might never
// read as a marker — but it is never unsafe: with the prefix absent, no greedy
// read of core, "z"s and digits can equal prefix plus an index.
func (c *markerCorpus) prefix(core string) string {
	p := []byte(core)
	for c.contains(p) {
		p = append(p, 'z')
	}
	return string(p)
}

func (c *markerCorpus) contains(p []byte) bool {
	for _, t := range c.texts {
		if bytes.Contains(t, p) {
			return true
		}
	}
	return false
}
