// Package ymledit performs in-place edits on a YAML document tree while
// preserving comments, key order, and formatting.
package ymledit

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"gopkg.in/yaml.v3"
)

// ErrUnsupported is returned for path shapes that Set and Delete cannot handle
// (wildcards, or the empty "whole document" path).
var ErrUnsupported = errors.New("unsupported path")

// ErrAnchored is returned when an edit would discard a node that carries a
// YAML anchor (`&name`) referenced elsewhere in the document via an alias
// (`*name`) or merge key (`<<: *name`). Overwriting or removing that node
// would leave the alias dangling, so Set and Delete refuse rather than
// silently writing a document that fails to parse back.
var ErrAnchored = errors.New("node has an anchor referenced elsewhere in the document")

// ErrNotResolved marks an error as "this document doesn't have that path" —
// a missing key, a scalar or list where a mapping was expected, an index out
// of range, an empty document. It is what lets a caller editing a whole
// stream skip a document the path isn't in, and is deliberately *not* carried
// by ErrUnsupported or ErrAnchored: those are wrong about the edit rather
// than about the document, so they are true of every document and must be
// reported rather than skipped (#170).
//
// The read side's equivalent is query.ErrNotFound. Separate sentinels for the
// same reason KeyError and query.NotFoundError are separate types: one walks
// decoded values, the other a node tree, and they share no resolution code.
var ErrNotResolved = errors.New("path did not resolve")

// notResolvedError tags an error with ErrNotResolved without changing what it
// prints, the same contract cmd's classifiedError has for exit codes:
// Error() delegates straight through, so tagging is invisible to a reader and
// visible only to errors.Is. Unwrap returns both, so errors.As still finds a
// wrapped *KeyError.
type notResolvedError struct{ err error }

func (e *notResolvedError) Error() string   { return e.err.Error() }
func (e *notResolvedError) Unwrap() []error { return []error{e.err, ErrNotResolved} }

// notResolved tags err, which must be non-nil.
func notResolved(err error) error { return &notResolvedError{err} }

// KeyError reports a path segment naming a key that the mapping it reached
// doesn't have — what Delete, Rename and Append return instead of editing
// something the caller didn't ask for. (Set creates a missing key rather
// than failing, so it never produces one; see #164.)
//
// Available lists that mapping's keys, sorted and deduplicated, so a caller
// can say what *was* there instead of sending the user back for a second
// command to find out. It is non-nil whenever a key lookup is what failed,
// an empty mapping included, which is how a caller tells that apart from an
// error with nothing to offer. Sorted rather than left in document order to
// match what `keys` prints and what a read miss offers, so the two halves of
// the CLI don't disagree about how keys are listed.
//
// Deliberately a separate type from query.NotFoundError rather than a shared
// one: query walks decoded `any` values and this walks a *yaml.Node tree,
// and the two have never shared resolution code. cmd handles both, which is
// cheaper than a dependency between them.
type KeyError struct {
	Path      []path.Segment
	Available []string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("%s: no such key", path.Format(e.Path))
}

// Unwrap tags a missing key as a path that didn't resolve, so a caller
// editing a whole stream can skip the document (#170) with the same check it
// uses for the other shapes.
func (e *KeyError) Unwrap() error { return ErrNotResolved }

// noSuchKey builds a KeyError for the key at segs[i] against mapping m.
func noSuchKey(segs []path.Segment, i int, m *yaml.Node) error {
	return &KeyError{
		Path:      slices.Clone(segs[:i+1]),
		Available: mappingKeys(m),
	}
}

// mappingKeys returns m's keys, sorted and deduplicated. A well-formed
// document has no duplicates, but a hand-built or hand-edited tree can, and
// offering the same key twice reads like a bug in the tool rather than in
// the input.
func mappingKeys(m *yaml.Node) []string {
	keys := make([]string, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		keys = append(keys, m.Content[i].Value)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// EditIndex caches, for one document, state that Set/Delete/Append/Rename
// would otherwise recompute by walking the document from scratch on every
// call: a mapping's key -> value-position lookup, and which anchor names are
// currently referenced by an alias or merge key anywhere in the document.
// Passing the same *EditIndex to a run of calls against the same doc (a
// batch of edits from the same script — see cmd/apply.go) keeps each call's
// cost proportional to what it actually touches instead of the whole
// document; passing nil (every single-op command) disables the cache
// entirely, and every call falls back to exactly the fresh, from-scratch
// walk this package has always done.
//
// The cache stays correct across mutations by being updated at each point
// where a call actually adds, removes, or renames a mapping entry, or
// discards/inserts a subtree that might carry anchor references — not by
// invalidating and rebuilding it, which would give back the cost this
// exists to avoid. The one case that's cheaper to give up on than to track
// precisely is a key removal, which shifts every later key in that
// mapping's Content down by two slots: rather than re-index all of them,
// that one mapping's key cache is dropped and lazily rebuilt (one full
// scan) the next time it's looked up — still strictly better than today's
// every-call-pays-a-scan baseline for any mapping that isn't itself
// repeatedly deleted from.
//
// The zero value is ready to use.
type EditIndex struct {
	keyPos     map[*yaml.Node]map[string]int // mapping node -> key -> its value's Content index
	anchorRefs map[string]int                // anchor name -> live reference count
	seededRefs bool                          // anchorRefs reflects the document's current state
}

func (ei *EditIndex) noteKeyAdded(m *yaml.Node, key string, vi int) {
	if ei == nil {
		return
	}
	if km := ei.keyPos[m]; km != nil {
		km[key] = vi
	}
}

// noteKeyRemoved drops mapping m's cached key index entirely — see
// EditIndex's doc comment for why removal isn't tracked incrementally.
func (ei *EditIndex) noteKeyRemoved(m *yaml.Node) {
	if ei == nil {
		return
	}
	delete(ei.keyPos, m)
}

func (ei *EditIndex) noteKeyRenamed(m *yaml.Node, oldKey, newKey string, vi int) {
	if ei == nil {
		return
	}
	if km := ei.keyPos[m]; km != nil {
		delete(km, oldKey)
		km[newKey] = vi
	}
}

// noteMappingReset drops cur's cached key index: Set's auto-vivification
// reuses cur's pointer identity but replaces its entire Content, so any
// cached positions for it are stale.
func (ei *EditIndex) noteMappingReset(cur *yaml.Node) {
	if ei == nil {
		return
	}
	delete(ei.keyPos, cur)
}

// noteSubtreeRemoved updates ei's anchor-reference counts for a subtree
// about to be discarded (replaced or deleted). A no-op until anchorAliased
// has actually seeded anchorRefs — nothing to keep in sync yet.
func (ei *EditIndex) noteSubtreeRemoved(n *yaml.Node) {
	if ei == nil || !ei.seededRefs || n == nil {
		return
	}
	countAnchorRefs(n, -1, ei.anchorRefs)
}

// noteSubtreeInserted is noteSubtreeRemoved's counterpart for a subtree
// being inserted — e.g. a freshly parsed value that itself contains an
// anchor/alias pair.
func (ei *EditIndex) noteSubtreeInserted(n *yaml.Node) {
	if ei == nil || !ei.seededRefs || n == nil {
		return
	}
	countAnchorRefs(n, 1, ei.anchorRefs)
}

// Set walks doc along segs and replaces the value found there with value.
//
// doc may be a DocumentNode or a bare value node. Missing intermediate mapping
// keys are created (as `jq '.a.b = x'` would); a missing list index is an
// error. Wildcards are rejected. Comments attached to a replaced value node are
// carried over to value when value does not set its own. Replacing a node
// whose YAML anchor (`&name`) is referenced elsewhere in the document (an
// alias or merge key) is an ErrAnchored error instead of silently leaving
// that alias dangling.
//
// ei caches state across a batch of calls against the same doc (see
// EditIndex); pass nil for a single, one-off edit.
func Set(doc *yaml.Node, segs []path.Segment, value *yaml.Node, ei *EditIndex) error {
	return set(doc, segs, value, ei, false)
}

// SetMustExist is Set with creation refused: a segment the document doesn't
// already have is a *KeyError instead of a key to create, which is what the
// other verbs have always done. Set's creating default is correct and matches
// `jq '.a.b = x'`, but it makes a typo'd path indistinguishable from an
// intentional new key — exit 0, no output, nothing to notice (#164). The
// error is the same type Delete/Rename/Append produce, so #163's available
// keys and "did you mean" come along without a second message.
//
// A separate function rather than an option, because the signature is what
// cmd passes to runValueEdit as a value.
func SetMustExist(doc *yaml.Node, segs []path.Segment, value *yaml.Node, ei *EditIndex) error {
	return set(doc, segs, value, ei, true)
}

// set is the walk both forms share. mustExist turns every point where Set
// would create something into a miss.
func set(doc *yaml.Node, segs []path.Segment, value *yaml.Node, ei *EditIndex, mustExist bool) error {
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to replace the whole document", ErrUnsupported)
	}

	cur := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			// Refused before the mapping is created, not after: with
			// mustExist a rejected edit has to leave the document exactly
			// as it was, and creating the mapping and only then failing on
			// the missing key left an empty one behind (#204). Same shape
			// as the isNullish check below, one level up. Delete, Append
			// and Rename report an empty document the same way.
			if mustExist {
				return notResolved(errors.New("the document is empty"))
			}
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		cur = doc.Content[0]
	}

	for i, seg := range segs {
		last := i == len(segs)-1

		switch {
		case seg.Fans():
			return fmt.Errorf("%w: %s: wildcards cannot be used with set", ErrUnsupported, atSeg(segs, i))

		case seg.IsIndex:
			if cur.Kind != yaml.SequenceNode {
				return notResolved(fmt.Errorf("%s: expected a list, got %s", atSeg(segs, i), kindName(cur.Kind)))
			}
			idx := seg.Index
			if idx < 0 {
				idx += len(cur.Content)
			}
			if idx < 0 || idx >= len(cur.Content) {
				return notResolved(fmt.Errorf("%s: index %d out of range (len %d)", atSeg(segs, i), seg.Index, len(cur.Content)))
			}
			if last {
				old := cur.Content[idx]
				if anchorAliased(ei, doc, old.Anchor) {
					return fmt.Errorf("%w: %s: anchor %q", ErrAnchored, atSeg(segs, i), old.Anchor)
				}
				ei.noteSubtreeRemoved(old)
				ei.noteSubtreeInserted(value)
				cur.Content[idx] = carryComments(old, value)
				return nil
			}
			cur = cur.Content[idx]

		default: // map key
			if mustExist && isNullish(cur) {
				// Nil rather than an empty list: a null is not a mapping
				// with no keys in it, so there is nothing to offer. Checked
				// ahead of the reset below, which would change the tree and
				// only then fail.
				return &KeyError{Path: slices.Clone(segs[:i+1])}
			}
			if isNullish(cur) {
				if anchorAliased(ei, doc, cur.Anchor) {
					return fmt.Errorf("%w: %s: anchor %q", ErrAnchored, atSeg(segs, i), cur.Anchor)
				}
				ei.noteMappingReset(cur)
				*cur = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			if cur.Kind != yaml.MappingNode {
				return notResolved(fmt.Errorf("%s: expected a mapping, got %s", atSeg(segs, i), kindName(cur.Kind)))
			}
			vi := findValueIndex(ei, cur, seg.Key)
			if vi < 0 && mustExist {
				return noSuchKey(segs, i, cur)
			}
			if vi < 0 {
				keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.Key}
				valNode := value
				if !last {
					valNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				}
				cur.Content = append(cur.Content, keyNode, valNode)
				ei.noteKeyAdded(cur, seg.Key, len(cur.Content)-1)
				if last {
					ei.noteSubtreeInserted(valNode)
					return nil
				}
				cur = valNode
				continue
			}
			if last {
				old := cur.Content[vi]
				if anchorAliased(ei, doc, old.Anchor) {
					return fmt.Errorf("%w: %s: anchor %q", ErrAnchored, atSeg(segs, i), old.Anchor)
				}
				ei.noteSubtreeRemoved(old)
				ei.noteSubtreeInserted(value)
				cur.Content[vi] = carryComments(old, value)
				return nil
			}
			cur = cur.Content[vi]
		}
	}
	return nil
}

// position is where a path's final segment landed: the mapping or sequence
// holding it, the index of the *value* node in that container's Content, and
// — for a mapping — the key that reached it. A mapping stores its key at
// index-1, which is what lets a caller rename the key or splice out the pair.
type position struct {
	parent  *yaml.Node
	index   int
	isIndex bool
	key     string
}

// node is the node the path resolved to.
func (p position) node() *yaml.Node { return p.parent.Content[p.index] }

// resolve walks doc along segs and reports where the final segment landed.
//
// Delete, Append and Rename share it. They differ in what they do once a
// path resolves, not in how it resolves, and three copies of this walk meant
// the sequence and mapping kind checks, the negative-index normalization and
// the bounds message existed in triplicate, where a fix could land in one
// and miss the other two — Append's copy of the bounds check was the one no
// unit test reached (#210). TestResolutionFailsIdenticallyAcrossVerbs is the
// property this enforces.
//
// verb appears in the wildcard refusal, the one message here that names its
// caller.
//
// set is deliberately not a caller. It creates missing keys and vivifies
// nulls as it descends, so it resolves *and* mutates at every level rather
// than only at the last; sharing this would mean threading a create flag
// through every branch, which is the version of this change that reads worse
// than the duplication it removes.
func resolve(doc *yaml.Node, segs []path.Segment, ei *EditIndex, verb string) (position, error) {
	if len(segs) == 0 {
		// Unreachable from the three callers: each refuses the empty path
		// first, in its own words ("refusing to delete the whole
		// document"), which is why that check isn't here. Kept so a fourth
		// caller gets an error rather than a nil parent out of the zero
		// position.
		return position{}, fmt.Errorf("%w: the path is empty", ErrUnsupported)
	}

	cur := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return position{}, notResolved(errors.New("the document is empty"))
		}
		cur = doc.Content[0]
	}

	var pos position
	for i, seg := range segs {
		last := i == len(segs)-1

		switch {
		case seg.Fans():
			return position{}, fmt.Errorf("%w: %s: wildcards cannot be used with %s", ErrUnsupported, atSeg(segs, i), verb)

		case seg.IsIndex:
			if cur.Kind != yaml.SequenceNode {
				return position{}, notResolved(fmt.Errorf("%s: expected a list, got %s", atSeg(segs, i), kindName(cur.Kind)))
			}
			idx := seg.Index
			if idx < 0 {
				idx += len(cur.Content)
			}
			if idx < 0 || idx >= len(cur.Content) {
				return position{}, notResolved(fmt.Errorf("%s: index %d out of range (len %d)", atSeg(segs, i), seg.Index, len(cur.Content)))
			}
			if last {
				pos = position{parent: cur, index: idx, isIndex: true}
			} else {
				cur = cur.Content[idx]
			}

		default: // map key
			if cur.Kind != yaml.MappingNode {
				return position{}, notResolved(fmt.Errorf("%s: expected a mapping, got %s", atSeg(segs, i), kindName(cur.Kind)))
			}
			vi := findValueIndex(ei, cur, seg.Key)
			if vi < 0 {
				return position{}, noSuchKey(segs, i, cur)
			}
			if last {
				pos = position{parent: cur, index: vi, key: seg.Key}
			} else {
				cur = cur.Content[vi]
			}
		}
	}
	return pos, nil
}

// Delete walks doc along segs and removes the mapping key or list element found
// at the final segment.
//
// doc may be a DocumentNode or a bare value node. Wildcards are rejected, as is
// the empty path. A segment that does not resolve (missing key, out-of-range
// index) is an error — Delete does not silently no-op. Comments attached to the
// removed node go away with it; comments on its siblings are untouched.
// Removing a node whose YAML anchor (`&name`) is referenced elsewhere in the
// document (an alias or merge key) is an ErrAnchored error instead of
// silently leaving that alias dangling.
//
// ei caches state across a batch of calls against the same doc (see
// EditIndex); pass nil for a single, one-off edit.
func Delete(doc *yaml.Node, segs []path.Segment, ei *EditIndex) error {
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to delete the whole document", ErrUnsupported)
	}

	pos, err := resolve(doc, segs, ei, "delete")
	if err != nil {
		return err
	}

	old := pos.node()
	if anchorAliased(ei, doc, old.Anchor) {
		return fmt.Errorf("%w: %s: anchor %q", ErrAnchored, path.Format(segs), old.Anchor)
	}
	ei.noteSubtreeRemoved(old)
	if pos.isIndex {
		pos.parent.Content = append(pos.parent.Content[:pos.index], pos.parent.Content[pos.index+1:]...)
		return nil
	}
	ei.noteSubtreeRemoved(pos.parent.Content[pos.index-1]) // the key node too — rare, but it can itself be an alias
	ei.noteKeyRemoved(pos.parent)
	pos.parent.Content = append(pos.parent.Content[:pos.index-1], pos.parent.Content[pos.index+1:]...)
	return nil
}

// Append walks doc along segs to the node the path resolves to and adds value
// as its last element.
//
// The target must already exist and be a sequence — appending into a mapping or
// scalar, or through a missing key, is an error. Wildcards and the empty path
// are rejected. Comments on the existing elements are untouched.
//
// ei caches state across a batch of calls against the same doc (see
// EditIndex); pass nil for a single, one-off edit.
func Append(doc *yaml.Node, segs []path.Segment, value *yaml.Node, ei *EditIndex) error {
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to append to the whole document", ErrUnsupported)
	}

	pos, err := resolve(doc, segs, ei, "append")
	if err != nil {
		return err
	}

	target := pos.node()
	if target.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s: expected a list to append to, got %s", path.Format(segs), kindName(target.Kind))
	}
	target.Content = append(target.Content, value)
	ei.noteSubtreeInserted(value)
	return nil
}

// Rename walks doc along segs and renames the mapping key at the final
// segment to newKey, leaving its position, value, and comments untouched.
//
// The final segment must be a plain mapping key: a list index or a wildcard
// there is rejected, the same as Set and Delete. Renaming a key to its own
// name succeeds as a no-op. Renaming to a name that already exists as a
// sibling is an error — Rename never silently clobbers another key.
//
// ei caches state across a batch of calls against the same doc (see
// EditIndex); pass nil for a single, one-off edit.
func Rename(doc *yaml.Node, segs []path.Segment, newKey string, ei *EditIndex) error {
	if !utf8.ValidString(newKey) {
		return fmt.Errorf("new key %q is not valid UTF-8", newKey)
	}
	if len(segs) == 0 {
		return fmt.Errorf("%w: refusing to rename the whole document", ErrUnsupported)
	}
	switch last := segs[len(segs)-1]; {
	case last.IsIndex:
		return fmt.Errorf("%w: %s: a list index cannot be renamed", ErrUnsupported, path.Format(segs))
	case last.Fans():
		return fmt.Errorf("%w: %s: wildcards cannot be used with rename", ErrUnsupported, path.Format(segs))
	}

	// The final segment is a mapping key: the switch above rejected an index
	// and a wildcard there, so pos.isIndex is false and pos.key is the key.
	pos, err := resolve(doc, segs, ei, "rename")
	if err != nil {
		return err
	}

	if newKey == pos.key {
		return nil
	}
	if findValueIndex(ei, pos.parent, newKey) >= 0 {
		return fmt.Errorf("%s: %q already exists", path.Format(segs), newKey)
	}
	keyNode := pos.parent.Content[pos.index-1]
	keyNode.Value = newKey
	if keyNode.Kind == yaml.ScalarNode {
		// The old key may have been tagged !!bool, !!int, etc. if it wasn't
		// quoted in the source (`true: x`). newKey is always taken as a
		// plain string, so the tag must be reset too, or the encoder emits
		// an explicit tag with a value that doesn't match it (e.g.
		// `!!bool renamed`), which then fails to decode at all.
		keyNode.Tag = "!!str"
	}
	ei.noteKeyRenamed(pos.parent, pos.key, newKey, pos.index)
	return nil
}

// ParseValue decodes a value string into a node suitable for Set. The string is
// parsed as YAML, so it may be a scalar ("8080", "true", "nginx:1.27"), a flow
// or block collection ("{a: 1}", "[1, 2]", "k:\n  v: 1"), or empty (-> null).
// When asString is true the value is taken verbatim as a !!str scalar.
//
// A value whose first non-space character is '#' is rejected rather than
// parsed: in YAML a '#' there always starts a comment, no matter what
// follows it, so there is no unquoted spelling of a literal string like
// "#ffffff" — silently parsing it as YAML would decode it to null instead
// of erroring, discarding the value entirely. asString, or quoting the
// value ("#ffffff"), says the '#' is meant literally.
func ParseValue(s string, asString bool) (*yaml.Node, error) {
	if asString {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}, nil
	}
	if strings.HasPrefix(strings.TrimSpace(s), "#") {
		return nil, fmt.Errorf("value %q starts with '#', which YAML always reads as a comment (so it would be silently discarded, not treated as text) — quote it or pass -s/--string for a literal value", s)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return doc.Content[0], nil
}

// atSeg formats the path up through segs[i] (inclusive), for an error
// message. Called only from error branches — not unconditionally at the top
// of each loop iteration — so a long path costs nothing extra on the common,
// no-error case; formatting the whole prefix on every single iteration
// regardless of whether it's ever used made Set (which walks the full
// segment count even against a tiny document, since it auto-creates missing
// keys) quadratic in path length.
func atSeg(segs []path.Segment, i int) string {
	return path.Format(segs[:i+1])
}

// findValueIndex returns the content index of key's value in mapping m, or -1
// if key is absent. A mapping should not have duplicate keys, but if the
// source document does anyway, the *last* occurrence wins — matching how
// yaml.v3 decodes into a Go map, and so how query.Run (and thus `get`) sees
// the mapping. Editing the first occurrence instead would silently target a
// key that every reader treats as already shadowed.
//
// With ei non-nil, m's key->index mapping is built once (a single scan) and
// cached, so repeated lookups against the same mapping (a batch of ops from
// the same script, each touching a different sibling key) don't each pay a
// fresh O(len(m.Content)) scan.
func findValueIndex(ei *EditIndex, m *yaml.Node, key string) int {
	if ei == nil {
		return scanKeyIndex(m, key)
	}
	km := ei.keyPos[m]
	if km == nil {
		km = make(map[string]int, len(m.Content)/2)
		for i := 0; i+1 < len(m.Content); i += 2 {
			km[m.Content[i].Value] = i + 1 // later entries overwrite earlier ones: last wins
		}
		if ei.keyPos == nil {
			ei.keyPos = make(map[*yaml.Node]map[string]int)
		}
		ei.keyPos[m] = km
	}
	if vi, ok := km[key]; ok {
		return vi
	}
	return -1
}

// scanKeyIndex is findValueIndex's uncached fallback: a full linear scan.
func scanKeyIndex(m *yaml.Node, key string) int {
	found := -1
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			found = i + 1
		}
	}
	return found
}

func carryComments(old, next *yaml.Node) *yaml.Node {
	if next.HeadComment == "" {
		next.HeadComment = old.HeadComment
	}
	if next.LineComment == "" {
		next.LineComment = old.LineComment
	}
	if next.FootComment == "" {
		next.FootComment = old.FootComment
	}
	return next
}

// anchorAliased reports whether doc contains an alias or merge key node
// (Kind == yaml.AliasNode; a merge key's value is an alias too) whose Value
// names the given anchor. Called before Set/Delete would discard a node that
// carries that anchor, so an edit never leaves a dangling *alias behind.
//
// With ei non-nil, doc's full set of live anchor references is walked once
// (lazily, on first use) and kept as a reference count that Set/Delete/
// Append update incrementally as they insert or discard subtrees — see
// EditIndex's doc comment — instead of every call re-walking the whole
// document.
func anchorAliased(ei *EditIndex, doc *yaml.Node, anchor string) bool {
	if anchor == "" || doc == nil {
		return false
	}
	if ei == nil {
		return scanAnchorAliased(doc, anchor)
	}
	if !ei.seededRefs {
		if ei.anchorRefs == nil {
			ei.anchorRefs = make(map[string]int)
		}
		countAnchorRefs(doc, 1, ei.anchorRefs)
		ei.seededRefs = true
	}
	return ei.anchorRefs[anchor] > 0
}

// scanAnchorAliased is anchorAliased's uncached fallback: a full walk of doc.
func scanAnchorAliased(doc *yaml.Node, anchor string) bool {
	if doc.Kind == yaml.AliasNode && doc.Value == anchor {
		return true
	}
	for _, c := range doc.Content {
		if scanAnchorAliased(c, anchor) {
			return true
		}
	}
	return false
}

// countAnchorRefs walks n, adding delta to refs' count for every alias or
// merge-key node's target anchor name found within — delta=+1 records a
// subtree being inserted, -1 a subtree being discarded, so refs always
// reflects which anchors are currently referenced somewhere live in the
// document.
func countAnchorRefs(n *yaml.Node, delta int, refs map[string]int) {
	if n == nil {
		return
	}
	if n.Kind == yaml.AliasNode {
		refs[n.Value] += delta
	}
	for _, c := range n.Content {
		countAnchorRefs(c, delta, refs)
	}
}

func isNullish(n *yaml.Node) bool {
	return n.Kind == 0 || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "list"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return "empty"
	}
}
