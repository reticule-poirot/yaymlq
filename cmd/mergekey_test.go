package cmd

import (
	"io"
	"strings"
	"testing"
)

// mergeDoc holds both shapes a merge key takes: one alias, and a sequence of
// them. `one.x` and `many.x` resolve for a read and are not keys of the
// mapping that answers for them.
const mergeDoc = `base: &base
  x: 1
extra: &extra
  z: 3
one:
  <<: *base
  y: 2
many:
  <<: [*base, *extra]
  w: 4
`

// mergedPaths are paths whose last segment arrives through a merge key, with
// the anchor each one comes from.
var mergedPaths = []struct {
	path   string
	anchor string
}{
	{".one.x", "base"},
	{".many.x", "base"},
	{".many.z", "extra"},
}

// editVerbsOnAPath builds each editing verb's invocation for a path, so the
// refusal can be checked everywhere it has to appear rather than on delete
// alone.
func editVerbsOnAPath(p string) [][]string {
	return [][]string{
		{"delete", p},
		{"rename", p, "renamed"},
		{"append", p, "9"},
		{"set", "--must-exist", p, "9"},
	}
}

// TestEditingAMergedKeyNamesTheMergeKey is #240. Refusing is right — the key
// is not physically in the mapping, so there is nothing there to delete or
// rename — but the refusal borrowed "no such key" and then offered a typo
// suggestion for a key the tool had just resolved for a read:
//
//	$ yaymlq -o raw 'one.x' mk.yaml      -> 1
//	$ yaymlq delete 'one.x' mk.yaml      -> one.x: no such key (did you mean "y"?)
//
// A caller reading that concludes the path was wrong and retries with the
// suggestion, silently editing a different field.
func TestEditingAMergedKeyNamesTheMergeKey(t *testing.T) {
	for _, mp := range mergedPaths {
		for _, args := range editVerbsOnAPath(mp.path) {
			t.Run(args[0]+mp.path, func(t *testing.T) {
				_, err := execute(t, mergeDoc, args...)
				if err == nil {
					t.Fatalf("%v: want a refusal, got success", args)
				}
				msg := err.Error()
				if strings.Contains(msg, "no such key") {
					t.Errorf("%v: still reported as a missing key: %s", args, msg)
				}
				if strings.Contains(msg, "did you mean") {
					t.Errorf("%v: offered a typo suggestion for a key that resolves: %s", args, msg)
				}
				if !strings.Contains(msg, "merge key") {
					t.Errorf("%v: message does not name the merge key: %s", args, msg)
				}
				if !strings.Contains(msg, mp.anchor) {
					t.Errorf("%v: message does not name the source anchor %q: %s", args, mp.anchor, msg)
				}
			})
		}
	}
}

// TestAMergedKeyStillReadsFine guards the half that must not move: the read
// side resolves through a merge key and always did. The two halves disagreeing
// is the bug; making them agree by breaking reads would be worse.
func TestAMergedKeyStillReadsFine(t *testing.T) {
	for _, mp := range mergedPaths {
		out, err := execute(t, mergeDoc, "-o", "raw", mp.path)
		if err != nil {
			t.Errorf("%s: want a successful read, got %v", mp.path, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s: read produced nothing", mp.path)
		}
	}
}

// TestAGenuinelyMissingKeyStillSaysSo is the other side of the new branch: it
// must fire only for a key that really is merged in. A key that is simply
// absent keeps "no such key" and its suggestion, which is the whole value of
// #137's recovery facts.
func TestAGenuinelyMissingKeyStillSaysSo(t *testing.T) {
	_, err := execute(t, mergeDoc, "delete", ".one.yy")
	if err == nil {
		t.Fatal("want a refusal for an absent key")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no such key") {
		t.Errorf("an absent key should still say so: %s", msg)
	}
	if !strings.Contains(msg, `"y"`) {
		t.Errorf("an absent key should still get its suggestion: %s", msg)
	}
	if strings.Contains(msg, "merge key") {
		t.Errorf("an absent key was blamed on a merge key: %s", msg)
	}
}

// TestMergedKeyRefusalIsNotAMissedPath pins the classification, which is the
// decision this error embodies. Like ErrUnsupported and ErrAnchored it must
// not carry ErrNotResolved: the document does have the path — a read proves
// it — so --all-docs must report this rather than skip the document and go on
// to say nothing matched, which would name the wrong problem.
func TestMergedKeyRefusalIsNotAMissedPath(t *testing.T) {
	const stream = "base: &base\n  x: 1\none:\n  <<: *base\n---\none:\n  x: 5\n"
	_, err := execute(t, stream, "delete", "--all-docs", ".one.x")
	if err == nil {
		t.Fatal("want the merged-key refusal reported, not skipped")
	}
	if !strings.Contains(err.Error(), "merge key") {
		t.Errorf("want the merge-key refusal, got %v", err)
	}
	if code := exitCode(err, io.Discard); code != 1 {
		t.Errorf("want exit 1, got %d", code)
	}
}

// TestSetWithoutMustExistStillShadows records the decision #240 left open
// rather than leaving it implicit. set creates a key it cannot find (#164),
// so on a mapping that merges one in it writes a real key that shadows the
// merged value. That stays: it is what set means everywhere else, and the
// result is a document whose own key wins, which is what the caller asked
// for. Only --must-exist, which promises not to create, gets the refusal.
func TestSetWithoutMustExistStillShadows(t *testing.T) {
	out, err := execute(t, mergeDoc, "set", ".one.x", "9")
	if err != nil {
		t.Fatalf("want set to create the shadowing key, got %v", err)
	}
	if !strings.Contains(out, "x: 9") {
		t.Errorf("want a real x: 9 written into one, got:\n%s", out)
	}
	if !strings.Contains(out, "<<: *base") {
		t.Errorf("the merge key should survive the write:\n%s", out)
	}
}
