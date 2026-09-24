package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
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
		return fmt.Errorf("%w (did you mean %q?)", err, s)
	}
	detail := strings.Join(shown, ", ")
	if n := len(all) - len(shown); n > 0 {
		detail += fmt.Sprintf(", +%d more", n)
	}
	return fmt.Errorf("%w (available keys: %s)", err, detail)
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
