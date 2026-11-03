package cmd

import "gopkg.in/yaml.v3"

// What an edit changed, found by comparison rather than reported by the edit.
//
// The editing verbs mutate a *yaml.Node tree through ymledit, which does not
// say what it touched. The splice needs to know, so it snapshots every node's
// own fields and child pointers before the mutation and compares after: a node
// whose fields differ was changed in place (a rename), and a node whose child
// list differs had a child replaced, removed or added. This depends on
// ymledit leaving every node it did not touch at the same address, which
// ymledit's TestEditsKeepUntouchedNodeIdentity holds it to.

// shallow is one node's own state: its fields and its child pointers, not the
// children's contents.
type shallow struct {
	kind               yaml.Kind
	tag, value, anchor string
	style              yaml.Style
	content            []*yaml.Node
	alias              *yaml.Node
	head, line, foot   string
}

func takeShallow(n *yaml.Node) shallow {
	return shallow{
		kind: n.Kind, tag: n.Tag, value: n.Value, anchor: n.Anchor, style: n.Style,
		content: append([]*yaml.Node(nil), n.Content...),
		alias:   n.Alias,
		head:    n.HeadComment, line: n.LineComment, foot: n.FootComment,
	}
}

// snapshotTree records every node reachable from docs, keyed by address.
func snapshotTree(docs []*yaml.Node) map[*yaml.Node]shallow {
	m := map[*yaml.Node]shallow{}
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if _, ok := m[n]; ok {
			return
		}
		m[n] = takeShallow(n)
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, d := range docs {
		walk(d)
	}
	return m
}

// ownChanged reports whether n's own fields differ from old's: what a rename
// does to a key. Comments are not compared; a comment changed in place is not
// something any verb does.
func ownChanged(old shallow, n *yaml.Node) bool {
	return old.kind != n.Kind || old.tag != n.Tag || old.value != n.Value || old.anchor != n.Anchor || old.alias != n.Alias
}

// contentChanged reports whether n's child list differs from old's.
func contentChanged(old shallow, n *yaml.Node) bool {
	return !samePtrs(old.content, n.Content)
}

func samePtrs(a, b []*yaml.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
