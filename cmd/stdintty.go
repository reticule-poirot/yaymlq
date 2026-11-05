package cmd

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
)

// refuseInteractiveStdin errors instead of letting a read block forever on a
// stdin that will never produce anything: a terminal, with no file argument
// given and nothing piped into it. That's the common shape of #(A8) — a
// misspelled invocation (set 'path=val' file, say, where the file argument
// lands as part of the path or value instead) silently falls back to
// reading stdin rather than failing on the bad argument count, and a TTY
// then hangs with no clue why.
//
// c.InOrStdin() is only ever a real terminal when nothing has called SetIn
// — every test double (strings.Reader, bytes.Buffer, an open regular file)
// is not an *os.File wrapping a character device, so this has no opinion
// about them and every existing test keeps working untouched.
func refuseInteractiveStdin(c *cobra.Command) error {
	f, ok := c.InOrStdin().(*os.File)
	if !ok {
		return nil
	}
	fi, statErr := f.Stat()
	if statErr != nil {
		return nil //nolint:nilerr // can't tell; don't block a read that might be fine
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return usageErr(errors.New("no input: stdin is a terminal, and no file argument was given, so nothing will ever arrive; pass a file argument or pipe input, e.g. `cat file.yaml | yaymlq ...`"))
}
