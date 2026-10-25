package cmd

import "errors"

// docMisses applies the one leniency rule both halves of the tool share to a
// run over the documents --doc or --all-docs selected: a document the path
// doesn't resolve in is skipped, the run fails only if nothing matched, and
// the failure reported is the *first* miss, so #137's available keys and "did
// you mean" survive rather than the last document's (#170/#175).
//
// It used to be three implementations of the same eleven lines — run(),
// runInspect(), and editTargets — which is what #212 was: CLAUDE.md states
// the rule once ("--all-docs is a selector everywhere. Read and edit alike"),
// and the next change to it had three places to land with no test that would
// catch a miss in one of them.
//
// notResolved is the caller's own "the path didn't resolve here" sentinel:
// query.ErrNotFound for a read, ymledit.ErrNotResolved for an edit. The two
// resolution halves share no resolution code and must not learn about each
// other, so the rule is shared by taking the sentinel as a field rather than
// by one side reaching for the other's.
//
// lenient is false when a miss is the run's failure outright. get computes it
// as soft || --all-docs, since -e, -q and --default each opt into a miss
// being expected; the inspect verbs compute it as --all-docs alone, because
// they offer none of those three (#213). That difference is the caller's,
// which is why it is a field rather than something recomputed here.
type docMisses struct {
	lenient     bool
	notResolved error

	// matched is read directly by the callers, which decide what "nothing
	// matched" means for them: exit 1 under -e/-q, a wildcard note
	// otherwise.
	matched bool
	first   error
}

// hit records that a document produced a result, which is what keeps the run
// from failing once every target has been visited.
func (m *docMisses) hit() { m.matched = true }

// miss classifies a per-document failure: it returns err when the run has to
// fail on it, or nil when the document is skipped and the next one tried.
// Callers wrap a returned error their own way — the read side with pathErr,
// the edit side by passing it up to applyEdit's annotateNotFound.
//
// An error that isn't notResolved always fails the run, which is how a
// refusal about the *edit* rather than about the document stays reported:
// ymledit's ErrUnsupported and ErrAnchored deliberately carry no
// ErrNotResolved, since they are true of every document, and skipping each
// one in turn to then report "nothing matched" would name the wrong problem.
func (m *docMisses) miss(err error) error {
	if !m.lenient || !errors.Is(err, m.notResolved) {
		return err
	}
	if m.first == nil {
		m.first = err
	}
	return nil
}

// unresolved returns the failure to end the run with — the first miss, when
// no document matched at all — or nil.
//
// Nothing matched and nothing failed to resolve either is not a failure here.
// The only way that happens is a wildcard whose branches all came up empty,
// since a concrete path either resolves or errors, and each caller reports
// that its own way (noteWildcardNoMatch, #173).
func (m *docMisses) unresolved() error {
	if m.matched {
		return nil
	}
	return m.first
}
