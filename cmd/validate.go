package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

type validateOptions struct {
	maxBytes int64
}

// newValidateCommand builds the read-only `validate` verb: it doesn't walk a
// path or transform a value, so it doesn't go through newInspectCommand.
func newValidateCommand() *cobra.Command {
	opts := &validateOptions{maxBytes: defaultMaxBytes}

	cmd := &cobra.Command{
		Use:   "validate [file...]",
		Short: "Check that each input is well-formed YAML",
		Long: `validate parses every document in each file (or stdin, when no files are
given) and reports whether it is well-formed YAML. It checks syntax only —
no schema, no path expression. All files given are checked, even after one
fails; the exit status is nonzero if any of them did.`,
		Example: `  yaymlq validate config.yaml
  yaymlq validate *.yaml
  cat config.yaml | yaymlq validate`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runValidate(c, opts, args)
		},
	}

	cmd.Flags().Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")
	return cmd
}

func runValidate(c *cobra.Command, opts *validateOptions, args []string) error {
	sources := args
	if len(sources) == 0 {
		sources = []string{"-"}
	}

	failed := false
	for i := range sources {
		name := sources[i]
		input := c.InOrStdin()
		var file *os.File
		if name != "-" {
			var err error
			file, err = os.Open(sources[i])
			if err != nil {
				failed = true
				_, _ = fmt.Fprintf(c.ErrOrStderr(), "%s: %v\n", name, err)
				continue
			}
			input = file
		}

		err := validateStream(input, opts.maxBytes)
		if file != nil {
			_ = file.Close()
		}
		if err != nil {
			failed = true
			label := name
			if label == "-" {
				label = "stdin"
			}
			_, _ = fmt.Fprintf(c.ErrOrStderr(), "%s: %v\n", label, err)
		}
	}

	if failed {
		return silentExit{code: 1}
	}
	return nil
}

// validateStream reads and parses one input, reporting only whether it
// succeeded — the whole stream is checked, not just one document.
func validateStream(input io.Reader, maxBytes int64) error {
	data, err := readCapped(input, maxBytes)
	if err != nil {
		return err
	}
	_, err = decodeDocs(data, 0, true)
	return err
}
