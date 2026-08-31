package cmd

import (
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
)

func newAppendCommand() *cobra.Command {
	opts := &valueEditOptions{editOpts: editOpts{maxBytes: defaultMaxBytes}}

	cmd := &cobra.Command{
		Use:   "append <path> <value> [file]",
		Short: "Append a value to the list at a path, preserving formatting",
		Long: "Append adds <value> as the last element of the list at <path> and prints\n" +
			"the whole document. The path must already resolve to a list; wildcards are\n" +
			"not allowed. <value> is parsed as YAML (a scalar or a collection); use\n" +
			"--string to take it verbatim. With --in-place the file is rewritten.",
		Example: "  yaymlq append '.services.web.ports' '\"9090:9090\"' compose.yml\n" +
			"  yaymlq append -i '.spec.template.spec.containers' '{name: proxy, image: envoy}' k8s.yaml\n" +
			"  cat cfg.yaml | yaymlq append .tags newtag",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runValueEdit(c, opts, args, ymledit.Append)
		},
	}
	bindValueEditFlags(cmd, opts)
	return cmd
}
