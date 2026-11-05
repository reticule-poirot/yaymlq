package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"gopkg.in/yaml.v3"
)

// coreYAMLTags are the tags yaml.v3 resolves to a concrete Go value on its
// own, so nothing about the source is lost by decoding into `any`: the
// scalar kinds, the two collection kinds, and !!timestamp (retagged to
// !!str before decode — see retagTimestamps — so it never actually reaches
// a caller as a tag in its own right, but it isn't "unknown" either).
// !!merge and the non-specific "!" are structural, not a type a caller
// could have meant to keep. Anything else — a custom tag like !Ref, or
// !!binary, whose decoded bytes are not its source spelling — is unknown
// for tagWarnings' purposes.
var coreYAMLTags = map[string]bool{
	"!!str": true, "!!int": true, "!!float": true, "!!bool": true,
	"!!null": true, "!!timestamp": true, "!!seq": true, "!!map": true,
	"!!merge": true, "!": true,
}

// tagWarnings accumulates the distinct unknown tags a read actually printed,
// across every document and every match a run covers, so one note can list
// them all instead of firing per match.
type tagWarnings struct {
	line  map[string]int
	order []string
}

// collect records tag's first line, scanning the subtree segs resolves to
// in root — the node a match's value actually came from. A path that
// doesn't resolve against root (the query engine and this node-tree walk
// can disagree, e.g. a key only reachable through a merge yaml.v3 already
// expanded before query ever saw it) is silently skipped: this is a
// best-effort note, not a correctness check, and missing one is better than
// a false alarm or a panic.
func (w *tagWarnings) collect(root *yaml.Node, segs []path.Segment) {
	n := lookupNode(root, segs)
	if n == nil {
		return
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if tag := n.ShortTag(); !coreYAMLTags[tag] {
			if w.line == nil {
				w.line = map[string]int{}
			}
			if _, seen := w.line[tag]; !seen {
				w.line[tag] = n.Line
				w.order = append(w.order, tag)
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(n)
}

// note renders the accumulated tags as one line for stderr, or "" if
// nothing was collected.
func (w *tagWarnings) note() string {
	if len(w.order) == 0 {
		return ""
	}
	parts := make([]string, len(w.order))
	for i, tag := range w.order {
		parts[i] = fmt.Sprintf("%s (line %d)", tag, w.line[tag])
	}
	return fmt.Sprintf("note: unknown tag%s dropped on read, printed without %s: %s\n",
		plural(len(parts), "", "s"), plural(len(parts), "it", "them"), strings.Join(parts, ", "))
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func (w *tagWarnings) emit(stderr io.Writer) {
	if n := w.note(); n != "" {
		_, _ = io.WriteString(stderr, n)
	}
}

// lookupNode walks root along segs — a concrete path, every wildcard
// already resolved to the key or index it matched, the shape
// query.RunMatches produces — to the node the same path reaches in the
// source tree. nil if the two disagree on the way.
func lookupNode(root *yaml.Node, segs []path.Segment) *yaml.Node {
	if root == nil {
		return nil
	}
	n := root
	for _, seg := range segs {
		if n.Kind == yaml.AliasNode && n.Alias != nil {
			n = n.Alias
		}
		if seg.IsIndex {
			if n.Kind != yaml.SequenceNode || seg.Index < 0 || seg.Index >= len(n.Content) {
				return nil
			}
			n = n.Content[seg.Index]
			continue
		}
		if n.Kind != yaml.MappingNode {
			return nil
		}
		next := (*yaml.Node)(nil)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind == yaml.AliasNode && k.Alias != nil {
				k = k.Alias
			}
			if k.Value == seg.Key {
				next = n.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}
