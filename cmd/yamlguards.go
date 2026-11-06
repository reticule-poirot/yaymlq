package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// Input yaml.v4 parses without complaint and reads against YAML 1.2.2, which is
// the spec this program follows. None of it has an upstream report yet, so each
// guard is written to go quiet by itself rather than to misfire if the parser is
// fixed.

// retagForRead corrects what yaml.v4's decoder would get wrong about a value.
//
// A !!merge scalar outside key position becomes plain text. Only a mapping key
// can merge, but yaml.v4 resolves a plain << to !!merge wherever it stands and
// its decoder then drops the entry, so `k: <<` reads as if k were never there.
// An alias to an anchored merge key becomes a copy of the text: retagging its
// target would stop that key merging.
//
// A `-0` becomes the int it is (isNegativeZero), spelled `-00`, which yaml.v4 does
// resolve to int 0. Not `0`: its decoder compares keys by text, so that would
// collide with a string key "0", a different key.
//
// This is for a tree about to be decoded to a value. An edit's tree keeps its
// tags, which is what writes the source's spelling back.
func retagForRead(n *yaml.Node) {
	if isNegativeZero(n) {
		n.Tag, n.Value = "!!int", "-00"
		return
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			retagForRead(c)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			switch {
			case isNegativeZero(k):
				k.Tag, k.Value = "!!int", "-00"
			case k.Kind != yaml.ScalarNode && k.Kind != yaml.AliasNode:
				retagForRead(k)
			}
			retagForRead(n.Content[i+1])
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

// isNegativeZero reports whether n is a `-0` that YAML reads as an int and yaml.v4
// does not: the Core schema's int pattern [-+]? [0-9]+ matches it and comes first
// (§10.3.2), but yaml.v4 resolves this one spelling to a float, and refuses it
// outright under an explicit !!int. `!!float -0` is a float by request.
func isNegativeZero(n *yaml.Node) bool {
	if n.Kind != yaml.ScalarNode || n.Value != "-0" {
		return false
	}
	return n.Tag == "!!int" || (n.Tag == "!!float" && n.Style&yaml.TaggedStyle == 0)
}

// maxImplicitKey is YAML 1.2.2's limit on an implicit key, [154] and [155], in
// characters.
const maxImplicitKey = 1024

// rejectNonConforming refuses what YAML 1.2.2 does not allow and yaml.v4 reads
// anyway, in a whole document's tree. A key or value it accepted from such a file
// would otherwise be written back by an edit, for a conforming parser to refuse.
func rejectNonConforming(n *yaml.Node, data []byte, lines *[]string) error {
	if err := rejectRawControl(n); err != nil {
		return err
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if isInLineAfterEmptyKey(k, v) {
				return parseErr(fmt.Errorf("parsing YAML: line %d: a block collection on the same line as an empty key, which YAML does not allow; move it to the next line and indent it", k.Line))
			}
			// The source, not the value: an escape is longer in one than the other.
			if len(data) > maxImplicitKey {
				if *lines == nil {
					*lines = sourceLines(toUTF8(data))
				}
				if isLongImplicitKey(n, k, *lines) {
					return parseErr(fmt.Errorf("parsing YAML: line %d: an implicit mapping key over %d characters, which YAML does not allow; write it as an explicit \"? key\" entry", k.Line, maxImplicitKey))
				}
			}
		}
	}
	for _, c := range n.Content {
		if err := rejectNonConforming(c, data, lines); err != nil {
			return err
		}
	}
	return nil
}

// rejectRawControl refuses DEL or a C1 control other than NEL where only printable
// characters are allowed: a plain or block scalar and a comment, all made of
// nb-char ([27]), which c-printable ([1]) excludes them from. A quoted scalar may
// hold them, §5.1 and [2]. An escape reaches the value of a double-quoted scalar
// only, so a character found in one of these values was written raw.
func rejectRawControl(n *yaml.Node) error {
	check := func(text string) error {
		i := strings.IndexFunc(text, isNonPrintableControl)
		if i < 0 {
			return nil
		}
		r, _ := utf8.DecodeRuneInString(text[i:])
		return parseErr(fmt.Errorf("parsing YAML: line %d: control character %U outside a quoted scalar, which YAML does not allow; write it as an escape in a double-quoted scalar", n.Line, r))
	}
	if n.Kind == yaml.ScalarNode && n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) == 0 {
		if err := check(n.Value); err != nil {
			return err
		}
	}
	for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
		if err := check(c); err != nil {
			return err
		}
	}
	return nil
}

func isNonPrintableControl(r rune) bool {
	return r == 0x7f || (r >= 0x80 && r <= 0x9f && r != 0x85)
}

// isInLineAfterEmptyKey reports the value [194] does not allow: an implicit key's
// value is s-l+block-node, not the s-l+block-indented that has a compact form, so
// a block collection can't start on the line of the ":". yaml.v4 knows that about
// `a: b: c` and not about `: b: c`. An explicit key's ":" is on a later line than
// its empty node, so it never matches, and a tag or anchor above a collection
// leaves the collection's first entry on the next line.
func isInLineAfterEmptyKey(k, v *yaml.Node) bool {
	if k.Kind != yaml.ScalarNode || k.Tag != "!!null" || k.Value != "" || k.Style != 0 {
		return false
	}
	if (v.Kind != yaml.MappingNode && v.Kind != yaml.SequenceNode) || v.Style&yaml.FlowStyle != 0 || len(v.Content) == 0 {
		return false
	}
	return v.Content[0].Line == k.Line
}

// isLongImplicitKey reports whether k, a key of m, runs past the limit on an
// implicit key, measured from its start to the ":" as [154] and [155] do. The
// limit is on a block mapping's key and a flow sequence's single pair, not on an
// explicit key or an entry of a flow mapping, whose ns-flow-map-yaml-key-entry
// has no such rule.
func isLongImplicitKey(m, k *yaml.Node, lines []string) bool {
	if k.Kind != yaml.ScalarNode || k.Line < 1 || k.Line > len(lines) {
		return false
	}
	// A pair's mapping has no bracket of its own, so it starts where its key does.
	if m.Style&yaml.FlowStyle != 0 && (len(m.Content) != 2 || k.Line != m.Line || k.Column != m.Column) {
		return false
	}
	if len(lines[k.Line-1]) <= maxImplicitKey {
		return false
	}
	line := []rune(lines[k.Line-1])
	start := k.Column - 1
	if start < 0 || start >= len(line) || strings.HasSuffix(strings.TrimRight(string(line[:start]), " \t"), "?") {
		return false
	}
	colon := keyEnd(line, start, k)
	for colon < len(line) && (line[colon] == ' ' || line[colon] == '\t') {
		colon++
	}
	return colon < len(line) && line[colon] == ':' && colon-start > maxImplicitKey
}

// keyEnd is the index just past k's own spelling on line, which starts at start
// with any properties k carries. A plain key's value is its spelling, because an
// implicit key sits on one line; a quoted key's has to be read, since an escape
// is longer in the source than in the value.
func keyEnd(line []rune, start int, k *yaml.Node) int {
	i := start
	for i < len(line) && (line[i] == '&' || line[i] == '!') {
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
	}
	switch {
	case k.Style&yaml.DoubleQuotedStyle != 0:
		for i++; i < len(line) && line[i] != '"'; i++ {
			if line[i] == '\\' {
				i++
			}
		}
		return i + 1
	case k.Style&yaml.SingleQuotedStyle != 0:
		for i++; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				break
			}
		}
		return i + 1
	}
	return i + utf8.RuneCountInString(k.Value)
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
