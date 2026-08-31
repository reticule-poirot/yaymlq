package cmd

import (
	"errors"
	"os"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newDeleteCommand() *cobra.Command {
	opts := &editOpts{maxBytes: defaultMaxBytes}

	cmd := &cobra.Command{
		Use:     "delete <path> [file]",
		Aliases: []string{"del", "rm"},
		Short:   "Delete the key or list element at a path, preserving formatting",
		Long: "Delete removes the mapping key or list element at the given path and prints\n" +
			"the whole document. Wildcards are not allowed, and deleting a path that does\n" +
			"not exist is an error. With --in-place the file is rewritten instead of printed.",
		Example: "  yaymlq delete '.services.web.environment.APP_ENV' compose.yml\n" +
			"  yaymlq delete -i '.spec.template.spec.containers[1]' k8s.yaml\n" +
			"  cat cfg.yaml | yaymlq delete .debug",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runDelete(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.inPlace, "in-place", "i", false, "rewrite the file instead of printing to stdout")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to edit in a multi-doc stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")

	return cmd
}

func runDelete(c *cobra.Command, opts *editOpts, args []string) error {
	expr := args[0]
	filename := ""
	if len(args) == 2 && args[1] != "-" {
		filename = args[1]
	}

	if opts.inPlace && filename == "" {
		return errors.New("--in-place needs a file argument")
	}

	segs, err := path.Parse(expr)
	if err != nil {
		return err
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

	return applyEdit(c, src, closeSrc, filename, *opts, func(docs []*yaml.Node, i int) error {
		return ymledit.Delete(docs[i], segs)
	})
}
