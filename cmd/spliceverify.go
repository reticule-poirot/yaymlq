package cmd

import (
	"bytes"
	"errors"
	"io"

	"go.yaml.in/yaml/v4"
)

// verifyDocs decodes out and compares each document with its counterpart in
// docs. bad names the documents that differ; ok is false when out does not
// decode or holds a different number of documents, which names none.
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
	// yaml.v4 tags a decoded "<<" key !!merge; one an edit made may be bare.
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

// decodeOne decodes data as a stream holding exactly one document. It is
// what every record's verification reads its text back with: yaml.Unmarshal
// stops after the first node and ignores whatever follows it, so a record
// that ran past the end of its construct — "[1]]" — read back as the value it
// was meant to hold and passed (#282).
func decodeOne(data []byte) (any, bool) {
	vs, ok := decodeValues(data)
	if !ok || len(vs) != 1 {
		return nil, false
	}
	return vs[0], true
}

func decodeValues(data []byte) ([]any, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	out := []any{}
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			return out, true
		}
		if err != nil {
			return nil, false
		}
		retagPlainMerge(&n)
		var v any
		if n.Decode(&v) != nil {
			return nil, false
		}
		out = append(out, v)
	}
}
