// Package cmd wires up the yaymlq command-line interface.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// version is overridden at build time via -ldflags.
var version = "dev"

type options struct {
	output     string
	raw        bool
	docIdx     int
	allDocs    bool
	maxBytes   int64
	defValue   string
	exitStatus bool
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
`),
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		RunE: func(c *cobra.Command, args []string) error {
			return run(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVar(&opts.raw, "raw", false, "shorthand for --output raw (unquoted scalars)")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")
	f.StringVar(&opts.defValue, "default", "", "value (parsed as YAML) to print when the path has no match")
	f.BoolVarP(&opts.exitStatus, "exit-status", "e", false, "exit 1 (no output) when the path has no match")

	cmd.AddCommand(newSetCommand())
	cmd.AddCommand(newDeleteCommand())
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
	return cmd
}

func run(c *cobra.Command, opts *options, args []string) error {
	expr := args[0]

	input := c.InOrStdin()
	if len(args) == 2 && args[1] != "-" {
		file, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		input = file
	}

	if opts.raw {
		opts.output = "raw"
	}

	hasDefault := c.Flags().Changed("default")
	var defValue any
	if hasDefault {
		if err := yaml.Unmarshal([]byte(opts.defValue), &defValue); err != nil {
			return fmt.Errorf("parsing --default value: %w", err)
		}
	}
	// A missing path is only "soft" (use default / exit status) when the user
	// opted in; otherwise it stays a hard error.
	soft := hasDefault || opts.exitStatus

	data, err := readCapped(input, opts.maxBytes)
	if err != nil {
		return err
	}

	docs, err := decodeDocs(data, opts.docIdx, opts.allDocs)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no YAML documents on input")
	}

	targets := []int{opts.docIdx}
	if opts.allDocs {
		targets = targets[:0]
		for i := range docs {
			targets = append(targets, i)
		}
	}

	out := c.OutOrStdout()
	matched := false
	for _, i := range targets {
		if i < 0 || i >= len(docs) {
			return fmt.Errorf("document index %d out of range (%d documents)", i, len(docs))
		}
		results, err := query.Run(docs[i], expr)
		if err != nil {
			if soft && errors.Is(err, query.ErrNotFound) {
				results = nil
			} else {
				return err
			}
		}
		if len(results) > 0 {
			matched = true
		}
		if len(results) == 0 && hasDefault {
			results = []any{defValue}
		}
		for _, r := range results {
			if err := render(out, r, opts.output); err != nil {
				return err
			}
		}
	}

	if opts.exitStatus && !matched {
		return silentExit{code: 1}
	}
	return nil
}
