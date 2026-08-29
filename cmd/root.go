// Package cmd wires up the yaymlq command-line interface.
package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// version is overridden at build time via -ldflags.
var version = "dev"

type options struct {
	output  string
	raw     bool
	docIdx  int
	allDocs bool
}

// NewRootCommand builds the root cobra command.
func NewRootCommand() *cobra.Command {
	opts := &options{output: "yaml", docIdx: 0}

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
  "a.b".c     quoted segment with a literal dot
`),
		Example: strings.TrimSpace(`
  yaymlq '.services.web.image' docker-compose.yml
  cat config.yaml | yaymlq metadata.labels
  yaymlq -o json '.items[0]' list.yaml
`),
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		Version:      version,
		RunE: func(c *cobra.Command, args []string) error {
			return run(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVar(&opts.raw, "raw", false, "shorthand for --output raw (unquoted scalars)")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")

	return cmd
}

func run(c *cobra.Command, opts *options, args []string) error {
	path := args[0]

	var input io.Reader = c.InOrStdin()
	if len(args) == 2 && args[1] != "-" {
		file, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}

	if opts.raw {
		opts.output = "raw"
	}

	docs, err := decodeAll(input)
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
	for _, i := range targets {
		if i < 0 || i >= len(docs) {
			return fmt.Errorf("document index %d out of range (%d documents)", i, len(docs))
		}
		result, err := query.Run(docs[i], path)
		if err != nil {
			return err
		}
		if err := render(out, result, opts.output); err != nil {
			return err
		}
	}
	return nil
}

func decodeAll(r io.Reader) ([]any, error) {
	dec := yaml.NewDecoder(r)
	var docs []any
	for {
		var doc any
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}
