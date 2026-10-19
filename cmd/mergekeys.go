package cmd

import (
	"regexp"

	"gopkg.in/yaml.v3"
)

// Merge keys are the one construct yaml.v3 round-trips visibly. Its *decoder*
// hands the `<<` key scalar an explicit tag:
//
//	kind=8 tag="!!merge" value="<<"
//
// and its encoder faithfully emits the tag it was given, so re-serializing a
// document turned every `<<: *name` into `!!merge <<: *name` — in every
// mapping, not just the one being edited (#171). Every other scalar comes
// back tagged `!!str`/`!!int`, which the encoder resolves implicitly and
// omits; `<<` was the one whose tag survived into the output.
//
// Clearing the tag lets the encoder resolve `<<` as a plain scalar again, and
// the parser treats a plain `<<` key as a merge key by its own convention, so
// the output re-decodes identically.
//
// The decoder reports an explicit `!!merge` in the source and an implicit one
// the same way, so a document that spelled the tag out would be rewritten the
// other way if the tag were cleared unconditionally — the same unrequested
// diff, mirrored. The raw source is the only thing that can tell them apart,
// so it is consulted here, the same way preserveBlankLines and
// recordCommentGutters consult it for what the node tree no longer carries.
var explicitMergeTag = regexp.MustCompile(`!!merge\s+<<`)

// mergeTagLines returns the 1-indexed source lines that spell out a `!!merge`
// tag, so normalizeMergeTags can leave those alone.
func mergeTagLines(data []byte) map[int]bool {
	locs := explicitMergeTag.FindAllIndex(data, -1)
	if locs == nil {
		return nil
	}
	lines := make(map[int]bool, len(locs))
	line, next := 1, 0
	for _, loc := range locs {
		for ; next < loc[0]; next++ {
			if data[next] == '\n' {
				line++
			}
		}
		lines[line] = true
	}
	return lines
}

// normalizeMergeTags clears the decoder-supplied `!!merge` tag from every
// merge key that didn't carry one in the source, so re-encoding reproduces
// the form the document was written in.
func normalizeMergeTags(n *yaml.Node, explicit map[int]bool) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!merge" && !explicit[n.Line] {
		n.Tag = ""
	}
	for _, c := range n.Content {
		normalizeMergeTags(c, explicit)
	}
}
