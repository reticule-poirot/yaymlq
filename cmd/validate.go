package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

type validateOptions struct {
	maxBytes int64
	require  []string
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
no schema. All files given are checked, even after one fails; the exit
status is nonzero if any of them did.

--require <path> additionally asserts that a path resolves in at least one
document of each source (repeatable for more than one required path) — a
source that parses but is missing a required path is reported the same way
a parse failure is.`,
		Example: `  yaymlq validate config.yaml
  yaymlq validate *.yaml
  yaymlq validate --require .image.tag --require .replicas deployment.yaml
  cat config.yaml | yaymlq validate`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runValidate(c, opts, args)
		},
	}

	cmd.Flags().Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")
	cmd.Flags().StringArrayVar(&opts.require, "require", nil, "path that must resolve in every input (repeatable)")
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

		err := validateStream(input, opts.maxBytes, opts.require)
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

// validateStream reads and parses one input — the whole stream, not just one
// document — then, if require is non-empty, confirms each of those paths
// resolves in at least one of its documents.
func validateStream(input io.Reader, maxBytes int64, require []string) error {
	data, err := readCapped(input, maxBytes)
	if err != nil {
		return err
	}
	docs, err := decodeDocs(data, 0, true)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		// Every other command (run, runInspect, applyEdit) already treats an
		// empty stream as a parse failure rather than trivially "valid" —
		// otherwise a genuinely empty input and a read that silently
		// produced nothing (see readCapped) are indistinguishable from a
		// real validation success.
		return parseErr(errors.New("no YAML documents on input"))
	}
	return checkRequired(docs, require)
}

// checkRequired reports an error naming every path in require that failed to
// resolve in any document of docs. A query error (wrong type, out-of-range
// index, ...) counts as "not found" on that document, same as -e/--default
// treat any unresolved path elsewhere in yaymlq.
func checkRequired(docs []any, require []string) error {
	var missing []string
	for _, expr := range require {
		found := false
		for _, doc := range docs {
			if results, err := query.Run(doc, expr); err == nil && len(results) > 0 {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, expr)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required path(s): %s", strings.Join(missing, ", "))
}
