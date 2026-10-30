package cmd

import (
	"errors"
	"fmt"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

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
// lenient is false when a miss is the run's failure outright. The read verbs
// compute it as soft || --all-docs (readMisses), since -e, -q and --default
// each opt into a miss being expected; an edit is lenient over whichever
// documents it was given. That is the caller's to decide, which is why it is
// a field rather than something recomputed here.
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

// missPolicy is what a read verb does about a path that doesn't resolve,
// settled from the three flags that say a miss is expected: -e and -q report
// one as an exit code instead of a message, --default names the answer to
// print in its place. All four read verbs offer all three (#213).
type missPolicy struct {
	// soft is any of the three: the user said a miss was expected, so it is
	// not reported as a failure.
	soft bool
	// hasDefault distinguishes --default "" — a legitimate empty string to
	// print — from --default never given.
	hasDefault bool
	// value is the parsed --default, meaningful only when hasDefault.
	value any
}

// resolveMissPolicy reads the three flags and parses --default, which is YAML
// so that a default can be a list or a mapping and not only a scalar. An
// unparseable one is a usage error: it is a mistake in the invocation, not in
// the document.
//
// --default with -q is refused for a reason worth keeping written down (#227).
// A default counts as a match (#208), so with output suppressed the pair exits
// 0 whether or not the path was there — `-q --default false .feature.enabled`
// reads as "quiet, and treat a missing flag as false" and is a check that can
// never fail. -e --default also always exits 0 and is fine: it prints, so the
// caller holds the value. Checked before the value is parsed, since the
// invocation is wrong whatever the value says.
func resolveMissPolicy(c *cobra.Command, o readOpts) (missPolicy, error) {
	p := missPolicy{hasDefault: c.Flags().Changed("default")}
	if p.hasDefault && o.quiet {
		return missPolicy{}, usageErr(fmt.Errorf("--default cannot be combined with -q/--quiet: a default is only useful if it is printed"))
	}
	p.soft = p.hasDefault || o.exitStatus || o.quiet
	if p.hasDefault {
		if err := yaml.Unmarshal([]byte(o.defValue), &p.value); err != nil {
			return missPolicy{}, usageErr(fmt.Errorf("parsing --default value: %w", err))
		}
	}
	return p, nil
}

// readMisses is the read side's docMisses. A per-document miss is tolerated
// either because the user opted into softness, or because they asked for the
// whole stream: --all-docs selects documents rather than asserting every one
// matches (#175). Either way the run still fails if *nothing* matched.
func readMisses(p missPolicy, o readOpts) docMisses {
	return docMisses{lenient: p.soft || o.allDocs, notResolved: query.ErrNotFound}
}
