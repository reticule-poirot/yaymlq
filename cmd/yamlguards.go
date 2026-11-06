package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Two inputs yaml.v4 parses without complaint and reads wrong. Neither has an
// upstream report yet, so each guard is written to go quiet by itself rather
// than to misfire if the parser is fixed.

// retagPlainMerge makes a !!merge scalar outside key position plain text. Only
// a mapping key can merge, but yaml.v4 resolves a plain << to !!merge wherever
// it stands and its decoder then drops the entry, so `k: <<` reads as if k were
// never there. An alias to an anchored merge key becomes a copy of the text:
// retagging its target would stop that key merging.
//
// This is for a tree about to be decoded to a value. An edit's tree keeps its
// tag, which is what writes the source's plain << back.
func retagPlainMerge(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			retagPlainMerge(c)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Kind != yaml.ScalarNode && n.Content[i].Kind != yaml.AliasNode {
				retagPlainMerge(n.Content[i])
			}
			retagPlainMerge(n.Content[i+1])
		}
	case yaml.ScalarNode:
		if n.Tag == "!!merge" {
			n.Tag = "!!str"
		}
	case yaml.AliasNode:
		if t := n.Alias; t != nil && t.Kind == yaml.ScalarNode && t.Tag == "!!merge" {
			n.Kind, n.Tag, n.Value, n.Alias = yaml.ScalarNode, "!!str", t.Value, nil
		}
	}
}

// blockHeaderLine matches a line ending in a block scalar header, the group
// being its indicators.
var blockHeaderLine = regexp.MustCompile(`(?:^|[ \t])[|>]([-+0-9]*)[ \t]*(?:[ \t]#.*)?$`)

// rejectSwallowedMarker refuses a root block scalar that read a document marker
// as its own text. yaml.v4 lets a root block scalar auto-detect an indentation
// of zero and then does not stop at a marker, so `--- |`, `---`, `b: 1` reads
// as one string where the spec has two documents, and every later document is
// lost without an error. A value holding text a file stopped being able to
// reach is the one thing worse than a refusal.
//
// Only the source can tell: the node looks like any other scalar. An explicit
// indentation indicator or an indented first line can't be read this way. The
// marker must also be in the value, so a parser that stopped at it reads fine.
//
// lines is the stream's sourceLines, filled on first use, so only a stream with
// a root block scalar pays for it and it pays once.
func rejectSwallowedMarker(doc *yaml.Node, data []byte, lines *[]string) error {
	if len(doc.Content) != 1 {
		return nil
	}
	n := doc.Content[0]
	if n.Kind != yaml.ScalarNode || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		return nil
	}
	if *lines == nil {
		*lines = sourceLines(toUTF8(data))
	}
	if line := swallowedMarkerLine(n, *lines); line > 0 {
		return parseErr(fmt.Errorf("parsing YAML: line %d: a document marker after an unindented root block scalar, which the parser reads as text and would lose every document after it; indent the scalar's body or give it an explicit indentation indicator", line))
	}
	return nil
}

// swallowedMarkerLine is the 1-based line of the first marker n read as text,
// or 0. n.Line is where the node starts, which for a tagged one is the tag, so
// the header is looked for from there.
func swallowedMarkerLine(n *yaml.Node, lines []string) int {
	i := n.Line - 1
	for ; i < len(lines); i++ {
		if m := blockHeaderLine.FindStringSubmatch(lines[i]); m != nil {
			if strings.ContainsAny(m[1], "123456789") {
				return 0
			}
			break
		}
	}
	first := true
	for j := i + 1; j < len(lines); j++ {
		if first {
			if blockBlank(lines[j]) {
				continue
			}
			if lines[j][0] == ' ' || lines[j][0] == '\t' {
				return 0
			}
			first = false
		}
		if (isDocStart(lines[j]) || isDocEnd(lines[j])) && strings.Contains(n.Value, lines[j][:3]) {
			return j + 1
		}
	}
	return 0
}
