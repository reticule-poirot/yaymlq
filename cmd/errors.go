package cmd

import (
	"errors"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/spf13/cobra"
)

// classifiedError tags an error with the process exit code its class maps
// to, without changing what it prints: Error() delegates straight to the
// wrapped error, so classifying an error is invisible except to exitCode.
//
// An error left unclassified (including everything internal/query and
// internal/ymledit return for a path that doesn't resolve the way the
// caller expected) keeps the default exit 1 — the same code -e/-q's
// deliberate "no match" already uses, since both describe "the path/value
// didn't give you what you asked for" rather than a malformed
// invocation, a broken document, or a failed read/write.
type classifiedError struct {
	code int
	err  error
}

func (c *classifiedError) Error() string { return c.err.Error() }
func (c *classifiedError) Unwrap() error { return c.err }

// parseErr marks err as exit code 2: the input YAML itself didn't parse.
func parseErr(err error) error { return classify(2, err) }

// usageErr marks err as exit code 3: a bad flag, argument count, path
// expression, or value the user gave couldn't be understood.
func usageErr(err error) error { return classify(3, err) }

// ioErr marks err as exit code 4: reading or writing a file or stream failed.
func ioErr(err error) error { return classify(4, err) }

func classify(code int, err error) error {
	if err == nil {
		return nil
	}
	return &classifiedError{code, err}
}

// pathErr classifies a path.Parse or query.Run error: a malformed path
// *expression* (unterminated bracket/quote, bad index, invalid UTF-8) is a
// usage error, since it's the caller's argument that's broken. Anything
// else — query.ErrNotFound and its wrong-type/out-of-range siblings —
// passes through unclassified, into the same family as a deliberate
// -e/-q "no match".
func pathErr(err error) error {
	var se *path.SyntaxError
	if errors.As(err, &se) {
		return usageErr(err)
	}
	return err
}

// usageArgs wraps a cobra positional-arg validator so a count mismatch is
// classified as a usage error (exit 3) instead of falling through as an
// ordinary, unclassified command error.
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := validate(c, args); err != nil {
			return usageErr(err)
		}
		return nil
	}
}
