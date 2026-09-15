package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/reticule-poirot/yaymlq/internal/editscript"
	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// applyOptions is apply's own flags plus the shared editOpts — apply runs on
// the same applyEdit pipeline as set/append/delete/rename, so it gets
// -i/--in-place, --doc, --max-bytes, --indent, and --diff/--dry-run for
// free.
type applyOptions struct {
	editOpts
	editsFile string
}

func newApplyCommand() *cobra.Command {
	opts := &applyOptions{editOpts: editOpts{maxBytes: defaultMaxBytes}}

	cmd := &cobra.Command{
		Use:   "apply -f <edits> [file]",
		Short: "Run a batch of set/append/delete/rename edits in one pass",
		Long: "Apply reads a small edit script (-f/--edits) — one operation per line —\n" +
			"and runs every op against the same in-memory document before a single\n" +
			"encode/write, instead of one parse/serialize cycle per edit. If any op\n" +
			"fails, nothing is written. Script format:\n\n" +
			"  set <path> = <value>       value is YAML, like set's own <value>\n" +
			"  append <path> = <value>    same\n" +
			"  delete <path>\n" +
			"  rename <path> = <newkey>   newkey is literal, like rename's own argument\n\n" +
			"Blank lines and lines starting with # are ignored.",
		Example: "  yaymlq apply -f edits.txt compose.yml\n" +
			"  yaymlq apply -f edits.txt -i compose.yml\n" +
			"  printf 'set .a = 1\\ndelete .b\\n' | yaymlq apply -f - compose.yml",
		Args:         usageArgs(cobra.RangeArgs(0, 1)),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runApply(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.editsFile, "edits", "f", "", "edit script file (required; - for stdin)")
	f.BoolVarP(&opts.inPlace, "in-place", "i", false, "rewrite the file instead of printing to stdout")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to edit in a multi-doc stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")
	f.IntVar(&opts.indent, "indent", 2, "spaces per indent level; auto-detected from the source when not given")
	bindDiffFlag(cmd, &opts.editOpts)

	return cmd
}

func runApply(c *cobra.Command, opts *applyOptions, args []string) error {
	filename := ""
	if len(args) == 1 && args[0] != "-" {
		filename = args[0]
	}

	if opts.editsFile == "" {
		return usageErr(errors.New("apply requires -f/--edits <path> (- for stdin)"))
	}
	if opts.inPlace && filename == "" {
		return usageErr(errors.New("--in-place needs a file argument"))
	}
	if opts.editsFile == "-" && filename == "" {
		return usageErr(errors.New("-f/--edits - reads the script from stdin, so the document needs a file argument"))
	}

	scriptSrc := c.InOrStdin()
	if opts.editsFile != "-" {
		f, err := os.Open(opts.editsFile)
		if err != nil {
			return ioErr(err)
		}
		defer func() { _ = f.Close() }()
		scriptSrc = f
	}
	// Bounded by --max-bytes too, same as the document argument — otherwise
	// the flag a user points at "cap all input to this command" would leave
	// the script itself unbounded.
	scriptData, err := readCapped(scriptSrc, opts.maxBytes)
	if err != nil {
		return err // already ioErr-classified by readCapped
	}
	ops, err := editscript.Parse(bytes.NewReader(scriptData))
	if err != nil {
		if errors.Is(err, editscript.ErrRead) {
			return ioErr(err)
		}
		return usageErr(err)
	}
	if len(ops) == 0 {
		return usageErr(errors.New("edit script is empty"))
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

	return applyEdit(c, src, closeSrc, filename, opts.editOpts, func(docs []*yaml.Node, i int) error {
		return runScriptOps(docs[i], ops)
	})
}

// runScriptOps runs every op against doc in order, aborting on the first
// failure. applyEdit only reaches the encode/write stage once mutate (this
// function) returns nil, so an op failing partway through never leaves a
// partial write.
//
// One *ymledit.EditIndex is shared across every op in the batch, so a
// script with many ops touching the same document (the same mapping's
// sibling keys, or a heavily-anchored document) stays close to linear in
// the number of ops instead of each op repaying an O(document size) scan —
// see EditIndex's doc comment.
func runScriptOps(doc *yaml.Node, ops []editscript.Op) error {
	ei := &ymledit.EditIndex{}
	for _, op := range ops {
		segs, err := path.Parse(op.Path)
		if err != nil {
			return pathErr(fmt.Errorf("line %d: %w", op.Line, err))
		}
		switch op.Verb {
		case editscript.Set:
			v, err := ymledit.ParseValue(op.Value, false)
			if err != nil {
				return usageErr(fmt.Errorf("line %d: parsing value: %w", op.Line, err))
			}
			if err := ymledit.Set(doc, segs, v, ei); err != nil {
				return fmt.Errorf("line %d: %w", op.Line, err)
			}
		case editscript.Append:
			v, err := ymledit.ParseValue(op.Value, false)
			if err != nil {
				return usageErr(fmt.Errorf("line %d: parsing value: %w", op.Line, err))
			}
			if err := ymledit.Append(doc, segs, v, ei); err != nil {
				return fmt.Errorf("line %d: %w", op.Line, err)
			}
		case editscript.Delete:
			if err := ymledit.Delete(doc, segs, ei); err != nil {
				return fmt.Errorf("line %d: %w", op.Line, err)
			}
		case editscript.Rename:
			if err := ymledit.Rename(doc, segs, op.Value, ei); err != nil {
				return fmt.Errorf("line %d: %w", op.Line, err)
			}
		}
	}
	return nil
}
