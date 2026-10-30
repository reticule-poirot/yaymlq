package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
)

// defaultMaxSuggestions caps how many keys a "path not found" error offers.
// A mapping with 500 keys would otherwise dump 500 keys into an error,
// which costs the caller more to read than the second query this is meant
// to save. 0 means no cap, the same sentinel --max-bytes uses.
const defaultMaxSuggestions = 10

// validateMaxSuggestions rejects a negative cap for the same reason
// readCapped rejects a negative --max-bytes: 0 already means "no cap", so a
// negative value is a mistake in whatever computed it, and silently reading
// it as unlimited would hide that. Checked up front, before any query runs,
// so it fails the same way whether or not the query would have missed.
func validateMaxSuggestions(n int) error {
	if n < 0 {
		return usageErr(fmt.Errorf("--max-suggestions must be >= 0 (0 = all of them), got %d", n))
	}
	return nil
}

// annotateNotFound adds the recovery information a *query.NotFoundError is
// already carrying to its message: the nearest key when the miss looks like
// a typo, and otherwise the keys that were there.
//
// Text mode only. Under -o json the same facts go into the object as data
// (see writeJSONError), where repeating them in the message would make a
// consumer parse prose it already has structurally.
//
// Anything that isn't a key miss against a mapping is returned unchanged:
// an out-of-range index, a key into a list and a scalar in the way all
// already say so in their own message.
func annotateNotFound(err error, maxKeys int) error {
	shown, all, target, ok := notFoundKeys(err, maxKeys)
	if !ok {
		return err
	}
	if len(all) == 0 {
		return fmt.Errorf("%w (the mapping has no keys)", err)
	}
	if s, found := nearest(target, all); found {
		return fmt.Errorf("%w (did you mean %s?)", err, keySuggestion(s))
	}
	detail := strings.Join(spellKeys(shown), ", ")
	if n := len(all) - len(shown); n > 0 {
		detail += fmt.Sprintf(", +%d more", n)
	}
	return fmt.Errorf("%w (available keys: %s)", err, detail)
}

// keySpelling renders a key the way a path expression names it, which is what
// a recovery message is for: the list and the suggestion are advice a caller
// pastes, so an offered key has to resolve when they do (#241).
//
// path.Format is the renderer rather than a rule of its own, so these
// messages, --paths and apply can't drift about what a key is called —
// quoteKey's list is the one place that decides, and #157/#158 are what
// happens when two spellings exist. Printed bare, a key named "" vanished
// from the list, `08` read as index 8, `a.b` as two segments, and `my key`
// could not be told from two keys; the suggestion used %q, Go's quoting,
// which looks right and emits escapes (\x01) a path rejects outright.
//
// Only text mode. Under -o json the keys stay raw names, since there they are
// data in a field rather than prose — a consumer must not have to strip
// quoting this added (see writeJSONError).
func keySpelling(key string) string {
	return path.Format([]path.Segment{{Key: key}})
}

// keySuggestion is keySpelling for the single-key "did you mean" form, which
// is always quoted. The quotes delimit one key in running prose, where the
// list has commas to do that — and they cost nothing, since a quoted segment
// is valid for any key, not only one that needs it. Wrapping is safe exactly
// when the key did not need quoting: that rules out both quote characters and
// the backslash, so there is nothing inside left to escape.
//
// This keeps the output a reader already sees — `did you mean "test"?` — while
// replacing %q, whose Go escapes (\x01) a path expression rejects.
func keySuggestion(key string) string {
	s := keySpelling(key)
	if strings.HasPrefix(s, `"`) || strings.HasPrefix(s, `'`) {
		return s
	}
	return `"` + s + `"`
}

func spellKeys(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = keySpelling(k)
	}
	return out
}

// notFoundKeys pulls the recovery facts out of a not-found error: the keys
// to display (capped at maxKeys, 0 for all of them), every key there was,
// and the key that missed — the one a suggestion is measured against.
//
// shown and all are returned separately because the cap bounds how much an
// error prints, not how hard it looks: searching only the displayed keys
// doesn't merely miss a suggestion for the 29th key of 30, it confidently
// offers the closest of the first ten instead.
//
// ok is false unless the error is a key miss against a mapping, which is
// the only case that has keys to offer. NotFoundError.Available is non-nil
// exactly then, empty mapping included.
func notFoundKeys(err error, maxKeys int) (shown, all []string, target string, ok bool) {
	var trail []path.Segment
	// Two error types, one for each half of the CLI: query resolves a path
	// against decoded values for the read verbs, ymledit walks a *yaml.Node
	// tree for the editing ones. They deliberately don't share resolution
	// code, so handling both here is cheaper than a dependency between them
	// — and it is what keeps the phrasing, the cap and the near-match rule
	// identical whichever half failed.
	var nfe *query.NotFoundError
	var ke *ymledit.KeyError
	switch {
	case errors.As(err, &nfe):
		all, trail = nfe.Available, nfe.Path
	case errors.As(err, &ke):
		all, trail = ke.Available, ke.Path
	}
	if all == nil || len(trail) == 0 {
		return nil, nil, "", false
	}
	shown = all
	if maxKeys > 0 && len(all) > maxKeys {
		shown = all[:maxKeys]
	}
	return shown, all, trail[len(trail)-1].Key, true
}

// noteWildcardNoMatch tells the caller a wildcard query found nothing.
//
// Zero matches is deliberately not an error, and that part doesn't change:
// once a wildcard has matched, a missing key or out-of-range index on an
// individual branch is skipped rather than reported, which is what makes
// `.services.*.image` usefully return only the services that set one. The
// cost is that zero matches in *total* — a typo'd leaf, or a wildcard at the
// wrong depth — has until now read exactly like a document that genuinely has
// none of those, since a concrete path's miss is an error carrying the keys
// that were there while a wildcard's is silence and exit 0 (#173).
//
// A note on stderr rather than a nonzero exit: stdout and the exit code stay
// as they are, so a pipeline that works today keeps working, and this stays
// additive. The same shape as the --paths --all-docs note and warnIfSymlink.
// A caller that wants the miss machine-readable already has -e.
//
// Lives here because this file is where both halves of the read side — run in
// root.go and runInspect in inspect.go — get their miss reporting, which is
// what keeps the two from wording the same situation differently.
func noteWildcardNoMatch(c *cobra.Command, expr string) {
	_, _ = fmt.Fprintf(c.ErrOrStderr(),
		"note: %s matched nothing; a wildcard that matches no branch is not an error (-e exits 1 instead)\n",
		expr)
}
