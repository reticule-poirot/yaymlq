package cmd

import (
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

	bindEditFlags(cmd, opts, allDocsSkips)

	return cmd
}

func runRename(c *cobra.Command, opts *editOpts, args []string) error {
	newKey := args[1]
	return runPathEdit(c, opts, args, 2, func(doc *yaml.Node, segs []path.Segment) error {
		return ymledit.Rename(doc, segs, newKey, nil)
	})
}
