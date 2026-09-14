package cmd

import (
	"errors"
	"os"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newRenameCommand() *cobra.Command {
	opts := &editOpts{maxBytes: defaultMaxBytes}

	cmd := &cobra.Command{
		Use:   "rename <path> <newkey> [file]",
		Short: "Rename the mapping key at a path, keeping its position and comments",
		Long: "Rename changes the key name at the given path and prints the whole document;\n" +
			"the key's position, value, and comments are untouched. The path must resolve\n" +
			"to a mapping key, not a list index or a wildcard. Renaming to a name that\n" +
			"already exists as a sibling is an error. With --in-place the file is\n" +
			"rewritten instead of printed.",
		Example: "  yaymlq rename '.services.web' webapp compose.yml\n" +
			"  yaymlq rename -i '.metadata.labels.\"app\"' name k8s.yaml\n" +
			"  cat cfg.yaml | yaymlq rename .oldName newName",
		Args:         usageArgs(cobra.RangeArgs(2, 3)),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runRename(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.inPlace, "in-place", "i", false, "rewrite the file instead of printing to stdout")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to edit in a multi-doc stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")

	return cmd
}

func runRename(c *cobra.Command, opts *editOpts, args []string) error {
	expr, newKey := args[0], args[1]
	filename := ""
	if len(args) == 3 && args[2] != "-" {
		filename = args[2]
	}

	if opts.inPlace && filename == "" {
		return usageErr(errors.New("--in-place needs a file argument"))
	}

	segs, err := path.Parse(expr)
	if err != nil {
		return pathErr(err)
	}

	src := c.InOrStdin()
	var closeSrc func() error
	if filename != "" {
		file, err := os.Open(filename)
		if err != nil {
			return ioErr(err)
		}
		src, closeSrc = file, file.Close
	}

	return applyEdit(c, src, closeSrc, filename, *opts, func(docs []*yaml.Node, i int) error {
		return ymledit.Rename(docs[i], segs, newKey)
	})
}
