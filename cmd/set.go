package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type setOptions struct {
	editOpts
	asString bool
}

func newSetCommand() *cobra.Command {
	opts := &setOptions{editOpts: editOpts{maxBytes: defaultMaxBytes}}

	cmd := &cobra.Command{
		Use:   "set <path> <value> [file]",
		Short: "Set the value at a path, preserving comments and formatting",
		Long: "Set writes a new value at the given path and prints the whole document.\n" +
			"Missing intermediate mapping keys are created. Wildcards are not allowed.\n" +
			"The value is parsed as YAML — a scalar (8080, true) or a collection\n" +
			"({a: 1}, [1, 2]); use --string to take it verbatim. With --in-place the\n" +
			"file is rewritten instead of printed.",
		Example: "  yaymlq set '.services.web.image' nginx:1.28 compose.yml\n" +
			"  yaymlq set --string '.metadata.annotations.\"team\"' platform k8s.yaml\n" +
			"  cat cfg.yaml | yaymlq set .debug true",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runSet(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.inPlace, "in-place", "i", false, "rewrite the file instead of printing to stdout")
	f.BoolVarP(&opts.asString, "string", "s", false, "treat <value> as a string, not parsed YAML")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to edit in a multi-doc stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")

	return cmd
}

func runSet(c *cobra.Command, opts *setOptions, args []string) error {
	expr, rawValue := args[0], args[1]
	filename := ""
	if len(args) == 3 && args[2] != "-" {
		filename = args[2]
	}

	if opts.inPlace && filename == "" {
		return errors.New("--in-place needs a file argument")
	}

	segs, err := path.Parse(expr)
	if err != nil {
		return err
	}
	value, err := ymledit.ParseValue(rawValue, opts.asString)
	if err != nil {
		return fmt.Errorf("parsing value: %w", err)
	}

	src := c.InOrStdin()
	var closeSrc func() error
	if filename != "" {
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		src, closeSrc = file, file.Close
	}

	return applyEdit(c, src, closeSrc, filename, opts.editOpts, func(docs []*yaml.Node, i int) error {
		return ymledit.Set(docs[i], segs, value)
	})
}
