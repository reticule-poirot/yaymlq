package cmd

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// sameAsTree reports whether out decodes to exactly the trees in docs —
// kind, tag, anchor, value and shape, ignoring style, comments and position.
// It is the splice's safety net, and it compares against the edited tree,
// not against what the encoder would write for it: the tree is what the edit
// meant, and an encode of it is one more opinion about spelling.
func sameAsTree(out []byte, docs []*yaml.Node) bool {
	dec := yaml.NewDecoder(bytes.NewReader(out))
	for _, d := range docs {
		var g yaml.Node
		if err := dec.Decode(&g); err != nil {
			return false
		}
		if !nodeEqual(&g, d) {
			return false
		}
	}
	var extra yaml.Node
	return dec.Decode(&extra) != nil
}

// verifyDocs decodes out and compares each document with its counterpart in
// docs. bad names the documents that differ; ok is false when out does not
// decode or holds a different number of documents, which names none. The
// stream is decoded whole: an alias may name an anchor in an earlier document,
// so a document's bytes need not decode on their own.
func verifyDocs(out []byte, docs []*yaml.Node) (bad []bool, ok bool) {
	dec := yaml.NewDecoder(bytes.NewReader(out))
	bad = make([]bool, len(docs))
	for i, d := range docs {
		var g yaml.Node
		if err := dec.Decode(&g); err != nil {
			return nil, false
		}
		bad[i] = !nodeEqual(&g, d)
	}
	var extra yaml.Node
	if dec.Decode(&extra) == nil {
		return nil, false
	}
	return bad, true
}

func nodeEqual(a, b *yaml.Node) bool {
	// A null is a null however it is spelled: the encoder writes an empty one
	// in flow context as "null" (normalizeFlowNulls), and a re-render may.
	if a.Kind == yaml.ScalarNode && b.Kind == yaml.ScalarNode && a.Tag == "!!null" && b.Tag == "!!null" {
		return a.Anchor == b.Anchor
	}
	if a.Kind != b.Kind || a.Anchor != b.Anchor || a.Value != b.Value || len(a.Content) != len(b.Content) {
		return false
	}
	at, bt := a.Tag, b.Tag
	// yaml.v3 tags a decoded "<<" key !!merge; one an edit made may be bare.
	if a.Kind == yaml.ScalarNode && a.Value == "<<" {
		if at == "" {
			at = "!!merge"
		}
		if bt == "" {
			bt = "!!merge"
		}
	}
	if a.Kind != yaml.DocumentNode && a.Kind != yaml.AliasNode && at != bt {
		return false
	}
	if a.Kind == yaml.AliasNode && (a.Alias == nil) != (b.Alias == nil) {
		return false
	}
	for i := range a.Content {
		if !nodeEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}
