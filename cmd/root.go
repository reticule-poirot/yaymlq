// Package cmd wires up the yaymlq command-line interface.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// version is overridden at build time via -ldflags.
var version = "dev"

type options struct {
	output     string
	print0     bool
	quiet      bool
	docIdx     int
	allDocs    bool
	maxBytes   int64
	defValue   string
	exitStatus bool
	paths      bool
}

// NewRootCommand builds the root cobra command.
func NewRootCommand() *cobra.Command {
	opts := &options{output: "yaml", docIdx: 0, maxBytes: defaultMaxBytes}

	cmd := &cobra.Command{
		Use:   "yaymlq [flags] <path> [file]",
		Short: "Yet Another YAML Query — extract values from YAML by path",
		Long: strings.TrimSpace(`
yaymlq reads a YAML document from a file or stdin and prints the value at the
given path expression.

Path syntax:
  .a.b.c      map keys (leading dot optional)
  a[0].b      slice index
  a.0.b       bare numeric segment is a slice index
  a.*.b       wildcard: every value of a mapping or list (may yield many results)
  a[].b       wildcard, jq-style
  "a.b".c     quoted segment with a literal dot
`),
		Example: strings.TrimSpace(`
  yaymlq '.services.web.image' docker-compose.yml
  cat config.yaml | yaymlq metadata.labels
  yaymlq -o json '.items[0]' list.yaml
  yaymlq -e '.optional.flag' cfg.yaml && echo present
  yaymlq --paths '.jobs.*.steps[*].with.go-version' ci.yml
`),
		Args:          usageArgs(cobra.RangeArgs(0, 2)),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = c.Help()
				return silentExit{code: 1}
			}
			return handleErr(c, run(c, opts, args), opts.output)
		},
	}
	// Inherited by every subcommand that doesn't set its own (none do), so
	// a bad flag anywhere in the tree — unknown flag, wrong value type —
	// is classified as a usage error (exit 3) instead of falling through
	// unclassified.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageErr(err)
	})

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVarP(&opts.print0, "print0", "0", false, "NUL-separate multiple results instead of newline, for xargs -0; implies -o raw")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")
	f.StringVar(&opts.defValue, "default", "", "value (parsed as YAML) to print when the path has no match")
	f.BoolVarP(&opts.exitStatus, "exit-status", "e", false, "exit 1 (no output) when the path has no match")
	f.BoolVar(&opts.paths, "paths", false, "print each match's resolved path instead of its value, for feeding back into apply; implies -o raw")
	f.BoolVarP(&opts.quiet, "quiet", "q", false, "no output; exit 0 on a match, 1 otherwise (mirrors grep -q)")

	cmd.AddCommand(newValidateCommand())
	cmd.AddCommand(newSetCommand())
	cmd.AddCommand(newAppendCommand())
	cmd.AddCommand(newDeleteCommand())
	cmd.AddCommand(newRenameCommand())
	cmd.AddCommand(newApplyCommand())
	cmd.AddCommand(newInspectCommand(
		"keys <path> [file]",
		"List the keys of a mapping, or the indices of a list, at a path",
		"keys prints one key per line, sorted (the same order wildcards use), or\n"+
			"0..n-1 for a list. It errors on a scalar. A wildcard path emits the keys of\n"+
			"every match.",
		"  yaymlq keys .services compose.yml\n"+
			"  yaymlq keys -o json .metadata.labels k8s.yaml",
		inspectKeys,
	))
	cmd.AddCommand(newInspectCommand(
		"len <path> [file]",
		"Print the number of entries, elements, or characters at a path",
		"len prints the entry count of a mapping, the element count of a list, the\n"+
			"rune count of a string, or 0 for null. It errors on a number or boolean.",
		"  yaymlq len .services.web.ports compose.yml\n"+
			"  cat cfg.yaml | yaymlq len .",
		inspectLen,
	))
	cmd.AddCommand(newInspectCommand(
		"type <path> [file]",
		"Print the type at a path: null, boolean, number, string, array, or object",
		"type names the value using JSON's vocabulary (object/array/string/...).",
		"  yaymlq type .spec compose.yml\n"+
			"  yaymlq type -o json .spec.replicas k8s.yaml",
		inspectType,
	))
	cmd.AddCommand(newSchemaCommand())
	return cmd
}

// queryResults resolves expr against doc, yielding either the matched
// values or — under --paths — each match's resolved path, rendered as an
// expression that parses back to the same place. Wildcards are what make
// this worth having: the values alone say that four things matched, never
// which four, so there was no route from a wildcard query to an apply
// script.
func queryResults(doc any, expr string, paths bool) ([]any, error) {
	if !paths {
		return query.Run(doc, expr)
	}
	matches, err := query.RunMatches(doc, expr)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(matches))
	for _, m := range matches {
		out = append(out, path.Format(m.Path))
	}
	return out, nil
}

func run(c *cobra.Command, opts *options, args []string) error {
	expr := args[0]

	input := c.InOrStdin()
	if len(args) == 2 && args[1] != "-" {
		file, err := os.Open(args[1])
		if err != nil {
			return ioErr(err)
		}
		defer func() { _ = file.Close() }()
		input = file
	}

	// Resolved here rather than left to render() so a bad -o value is a usage
	// error (exit 3) even under --quiet, which never calls render() at all.
	format, err := resolveOutputFormat(c, opts.output, opts.print0, opts.paths)
	if err != nil {
		return err
	}
	opts.output = format
	if err := validateDocSelection(c, opts.docIdx, opts.allDocs); err != nil {
		return err
	}
	if opts.paths && c.Flags().Changed("default") {
		return usageErr(fmt.Errorf("--default cannot be combined with --paths: a value that isn't in the document has no path"))
	}

	hasDefault := c.Flags().Changed("default")
	var defValue any
	if hasDefault {
		if err := yaml.Unmarshal([]byte(opts.defValue), &defValue); err != nil {
			return usageErr(fmt.Errorf("parsing --default value: %w", err))
		}
	}
	// A missing path is only "soft" (use default / exit status) when the user
	// opted in; otherwise it stays a hard error.
	soft := hasDefault || opts.exitStatus || opts.quiet

	data, err := readCapped(input, opts.maxBytes)
	if err != nil {
		return err
	}

	docs, err := decodeDocs(data, opts.docIdx, opts.allDocs)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return parseErr(fmt.Errorf("no YAML documents on input"))
	}

	targets := []int{opts.docIdx}
	if opts.allDocs {
		targets = targets[:0]
		for i := range docs {
			targets = append(targets, i)
		}
	}

	out := c.OutOrStdout()
	var rw *resultWriter
	if !opts.quiet {
		rw = &resultWriter{out: out, format: opts.output, print0: opts.print0}
	}
	matched := false
	for _, i := range targets {
		if i < 0 || i >= len(docs) {
			return usageErr(fmt.Errorf("document index %d out of range (%d documents)", i, len(docs)))
		}
		results, err := queryResults(docs[i], expr, opts.paths)
		if err != nil {
			if soft && errors.Is(err, query.ErrNotFound) {
				results = nil
			} else {
				return pathErr(err)
			}
		}
		if len(results) > 0 {
			matched = true
		}
		if len(results) == 0 && hasDefault {
			results = []any{defValue}
		}
		if rw != nil {
			for _, r := range results {
				if err := rw.emit(r); err != nil {
					return err
				}
			}
		}
	}
	if rw != nil {
		if err := rw.flush(); err != nil {
			return err
		}
	}

	if (opts.exitStatus || opts.quiet) && !matched {
		return silentExit{code: 1}
	}
	return nil
}
