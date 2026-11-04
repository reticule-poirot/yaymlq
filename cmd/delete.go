package cmd

import (
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
		Args:         usageArgs(cobra.RangeArgs(1, 2)),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runDelete(c, opts, args)
		},
	}

	bindEditFlags(cmd, opts, allDocsSkips)

	return cmd
}

func runDelete(c *cobra.Command, opts *editOpts, args []string) error {
	filename, err := editFileArg(args, 1, opts.inPlace)
	if err != nil {
		return err
	}
	segs, err := path.Parse(args[0])
	if err != nil {
		return pathErr(err)
	}
	src, err := openEditSource(c, filename)
	if err != nil {
		return err
	}

	return applyEdit(c, src, *opts, func(docs []*yaml.Node, targets []int) error {
		return editTargets(targets, func(i int) error {
			return ymledit.Delete(docs[i], segs, nil)
		})
	})
}
