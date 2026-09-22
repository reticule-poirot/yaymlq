package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

type validateOptions struct {
	maxBytes int64
	require  []string
	docIdx   int
	allDocs  bool
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
a parse failure is.

--all-docs makes --require strict: the path must resolve in every document
of a source, not just one. --doc N narrows it to a single document instead.
Both only scope --require; validate always parses the whole stream, so
neither weakens the syntax check.`,
		Example: `  yaymlq validate config.yaml
  yaymlq validate *.yaml
  yaymlq validate --require .image.tag --require .replicas deployment.yaml
  yaymlq validate --all-docs --require .metadata.name k8s.yaml
  cat config.yaml | yaymlq validate`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runValidate(c, opts, args)
		},
	}

	cmd.Flags().Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")
	cmd.Flags().StringArrayVar(&opts.require, "require", nil, "path that must resolve in at least one document of each input (repeatable)")
	cmd.Flags().IntVar(&opts.docIdx, "doc", 0, "check --require against this document only")
	cmd.Flags().BoolVar(&opts.allDocs, "all-docs", false, "require each --require path in every document, not just one")
	return cmd
}

func runValidate(c *cobra.Command, opts *validateOptions, args []string) error {
	// Parse every --require expression before opening any input. A malformed
	// expression is a bad argument, not a validation failure of the document:
	// it can never resolve against anything, so reporting it per-source as a
	// missing path (exit 1) points the reader at their YAML instead of at
	// their command line. pathErr gives it the same exit 3 every other
	// command returns for the same expression.
	// --doc/--all-docs only scope --require; validate parses the whole stream
	// either way. Accepting them without it would be a flag that silently
	// does nothing — the shape of #118.
	scoped := c.Flags().Changed("doc")
	if (scoped || opts.allDocs) && len(opts.require) == 0 {
		return usageErr(errors.New("--doc and --all-docs only scope --require; pass --require, or drop them"))
	}
	if err := validateDocSelection(c, opts.docIdx, opts.allDocs); err != nil {
		return err
	}
	for _, expr := range opts.require {
		if _, err := path.Parse(expr); err != nil {
			return pathErr(err)
		}
	}

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

		err := validateStream(input, opts.maxBytes, opts.require, scoped, opts.docIdx, opts.allDocs)
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
func validateStream(input io.Reader, maxBytes int64, require []string, scoped bool, docIdx int, allDocs bool) error {
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
	return checkRequired(docs, require, scoped, docIdx, allDocs)
}

// checkRequired reports an error naming every path in require that failed to
// resolve. Which documents have to satisfy it depends on the scope: one named
// by --doc, every document under --all-docs, or — the default — any single
// one. A query error (wrong type, out-of-range index, ...) counts as "not
// found" on that document, same as -e/--default treat any unresolved path
// elsewhere in yaymlq.
//
// An out-of-range --doc is reported as this source failing rather than as a
// usage error: validate takes many files and reports per source, so a file
// holding fewer documents than asked for is a property of that file, not of
// the command line.
func checkRequired(docs []any, require []string, scoped bool, docIdx int, allDocs bool) error {
	if scoped && (docIdx < 0 || docIdx >= len(docs)) {
		return fmt.Errorf("document %d out of range (%d documents)", docIdx, len(docs))
	}

	resolves := func(doc any, expr string) bool {
		results, err := query.Run(doc, expr)
		return err == nil && len(results) > 0
	}

	var missing []string
	for _, expr := range require {
		var ok bool
		switch {
		case scoped:
			ok = resolves(docs[docIdx], expr)
		case allDocs:
			ok = true
			for _, doc := range docs {
				if !resolves(doc, expr) {
					ok = false
					break
				}
			}
		default:
			for _, doc := range docs {
				if resolves(doc, expr) {
					ok = true
					break
				}
			}
		}
		if !ok {
			missing = append(missing, expr)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	list := strings.Join(missing, ", ")
	switch {
	case scoped:
		return fmt.Errorf("missing required path(s) in document %d: %s", docIdx, list)
	case allDocs:
		return fmt.Errorf("required path(s) missing from at least one document: %s", list)
	default:
		return fmt.Errorf("missing required path(s): %s", list)
	}
}
