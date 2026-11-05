package cmd

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// failReader's Read always fails the test — proof that whatever calls it
// touched the input, which a rejected invocation must never do.
type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Fatal("input was read before flag validation rejected the invocation")
	return 0, errors.New("unreachable")
}

// TestApplyEditValidatesFlagsBeforeReading is #323. applyEdit read the
// document — up to --max-bytes, 64 MiB by default — before checking any of
// --show-diff/--diff-format/--indent/--max-suggestions, unlike the read
// side's validateReadFlags, which runs before readDocs. A usage error used
// to pay for the read (and, before that, the mutate/decode) anyway; now it
// doesn't.
func TestApplyEditValidatesFlagsBeforeReading(t *testing.T) {
	cases := []struct {
		name    string
		mutOpts func(c *cobra.Command, opts *editOpts)
	}{
		{"show-diff without in-place", func(_ *cobra.Command, opts *editOpts) {
			opts.showDiff = true
		}},
		{"diff and show-diff together", func(_ *cobra.Command, opts *editOpts) {
			opts.diff, opts.showDiff, opts.inPlace = true, true, true
		}},
		{"negative max-suggestions", func(_ *cobra.Command, opts *editOpts) {
			opts.maxSugg = -1
		}},
		{"bad indent", func(c *cobra.Command, _ *editOpts) {
			if err := c.Flags().Set("indent", "0"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := &editOpts{maxBytes: defaultMaxBytes, diffFormat: "text", indent: 2}
			c := &cobra.Command{}
			bindEditFlags(c, opts, allDocsSkips)
			tc.mutOpts(c, opts)

			src := editSource{reader: failReader{t}}
			mutate := func(_ []*yaml.Node, _ []int) error {
				t.Fatal("mutate called: input was decoded before flag validation rejected the invocation")
				return nil
			}
			err := applyEdit(c, src, *opts, mutate)
			if err == nil {
				t.Fatal("want a usage error, got none")
			}
			if code := codeFor(err); code != 3 {
				t.Errorf("codeFor = %d, want 3 (usage); err: %v", code, err)
			}
		})
	}
}
