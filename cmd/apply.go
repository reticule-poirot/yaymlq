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
			"and runs every op against the same in-memory stream before a single\n" +
			"encode/write, instead of one parse/serialize cycle per edit. If any op\n" +
			"fails, nothing is written. Script format:\n\n" +
			"  set <path> = <value>       value is YAML, like set's own <value>\n" +
			"  append <path> = <value>    same\n" +
			"  delete <path>\n" +
			"  rename <path> = <newkey>   newkey is literal, like rename's own argument\n\n" +
			"Any verb may take --doc N (or --doc=N) before the path, naming the document\n" +
			"in a multi-document stream it applies to; without it an op follows the\n" +
			"command's own --doc. One script can therefore edit several documents in a\n" +
			"single pass, which is what `get --paths --all-docs -o json` output feeds.\n\n" +
			"Blank lines and lines starting with # are ignored.",
		Example: "  yaymlq apply -f edits.txt compose.yml\n" +
			"  yaymlq apply -f edits.txt -i compose.yml\n" +
			"  printf 'set --doc 1 .image = nginx:1.28\\n' | yaymlq apply -f - manifests.yml\n" +
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
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")
	f.IntVar(&opts.indent, "indent", 2, "spaces per indent level; auto-detected from the source when not given")
	bindEditFlags(cmd, &opts.editOpts)

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
		return runScriptOps(docs, i, ops)
	})
}

// runScriptOps runs every op in order, aborting on the first failure.
// applyEdit only reaches the encode/write stage once mutate (this function)
// returns nil, so an op failing partway through never leaves a partial
// write — including one that failed on a different document than the ops
// before it.
//
// An op applies to the document it names with --doc, or to defaultDoc (the
// invocation's own --doc) when it names none. That is what lets one script
// span a stream, which is what makes a `get --paths --all-docs -o json`
// listing usable as a script: paths repeat across documents, so the index
// has to travel with them (#159).
//
// One *ymledit.EditIndex per document, shared across that document's ops, so
// a script with many ops touching the same mapping's sibling keys (or a
// heavily-anchored document) stays close to linear in the number of ops
// instead of each op repaying an O(document size) scan — see EditIndex's doc
// comment. Per document rather than one for the batch because an index
// caches key positions for the nodes of one tree; handing document 1's ops
// document 0's index would answer from the wrong document.
func runScriptOps(docs []*yaml.Node, defaultDoc int, ops []editscript.Op) error {
	indexes := make(map[int]*ymledit.EditIndex, 1)
	for _, op := range ops {
		target := defaultDoc
		if op.HasDoc {
			target = op.Doc
		}
		if target < 0 || target >= len(docs) {
			return usageErr(fmt.Errorf("line %d: document index %d out of range (%d documents)", op.Line, target, len(docs)))
		}
		ei := indexes[target]
		if ei == nil {
			ei = &ymledit.EditIndex{}
			indexes[target] = ei
		}
		doc := docs[target]

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
