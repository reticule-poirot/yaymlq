// Package cmd wires up the yaymlq command-line interface.
package cmd

import (
	"fmt"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// options is get's flag set: the shared read flags, and nothing of its own
// left since #213 moved -e/-q/--default into readOpts for all four read
// verbs.
type options struct {
	readOpts
}

// NewRootCommand builds the root cobra command.
func NewRootCommand() *cobra.Command {
	opts := &options{readOpts{output: "yaml", docIdx: 0, maxBytes: defaultMaxBytes, maxSugg: defaultMaxSuggestions}}

	cmd := &cobra.Command{
		Use:   "yaymlq [flags] <path> [file]",
		Short: "Yet Another YAML Query — extract values from YAML by path",
		Long: strings.TrimSpace(`
yaymlq reads a YAML document from a file or stdin and prints the value at the
given path expression.

Path syntax:
  .a.b.c       map keys (leading dot optional)
  a[0].b       slice index
  a.0.b        bare numeric segment is a slice index
  "1", "null"  a non-string key (1:, null:) by its text; bare 1 is an index
  a[-1]        negative index counts from the end
  a.*.b        wildcard: every value of a mapping or list (may match many)
  a[].b, a[*]  wildcard, jq-style
  **.b         recursive descent: b at any depth, this level included
  a.**.b       the same, anywhere under a
  a.**         a and every node beneath it
  "a.b".c      quoted segment with a literal dot
  "a\nb"       inside quotes, \ escapes: \\ \" \' \n \t \r
  (empty), .   the whole document
`),
		Example: strings.TrimSpace(`
  yaymlq '.services.web.image' docker-compose.yml
  cat config.yaml | yaymlq metadata.labels
  yaymlq -o json '.items[0]' list.yaml
  yaymlq -e '.optional.flag' cfg.yaml && echo present
  yaymlq --paths '.jobs.*.steps[*].with.go-version' ci.yml
`),
		Args:          usageArgs(cobra.RangeArgs(0, 2)),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = c.Help()
				return silentExit{code: 1}
			}
			return handleErr(c, run(c, opts, args), opts.output, opts.maxSugg)
		},
	}
	// Inherited by every subcommand that doesn't set its own (none do), so
	// a bad flag anywhere in the tree — unknown flag, wrong value type —
	// is classified as a usage error (exit 3) instead of falling through
	// unclassified.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageErr(err)
	})

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVarP(&opts.print0, "print0", "0", false, "NUL-separate multiple results instead of newline, for xargs -0; implies -o raw")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")
	f.IntVar(&opts.maxSugg, "max-suggestions", opts.maxSugg, "max keys a \"path not found\" error lists; 0 = all of them")
	f.StringVar(&opts.defValue, "default", "", "value (parsed as YAML) to print when the path has no match; counts as a match, so -e still exits 0; not valid with -q")
	f.BoolVarP(&opts.exitStatus, "exit-status", "e", false, "exit 1 (no output) when the path has no match")
	f.BoolVar(&opts.paths, "paths", false, "print each match's resolved path instead of its value, for feeding back into apply; implies -o raw")
	f.BoolVarP(&opts.quiet, "quiet", "q", false, "no output; exit 0 on a match, 1 otherwise (mirrors grep -q)")
	f.BoolVarP(&opts.asString, "string", "s", false, "treat --default as a string, not parsed YAML")

	cmd.AddCommand(newValidateCommand())
	cmd.AddCommand(newSetCommand())
	cmd.AddCommand(newAppendCommand())
	cmd.AddCommand(newDeleteCommand())
	cmd.AddCommand(newRenameCommand())
	cmd.AddCommand(newApplyCommand())
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
	cmd.AddCommand(newSchemaCommand())
	return cmd
}

// queryResults resolves expr against doc, yielding either the matched
// values or — under --paths — each match's resolved path, rendered as an
// expression that parses back to the same place. Wildcards are what make
// this worth having: the values alone say that four things matched, never
// which four, so there was no route from a wildcard query to an apply
// script.
func queryResults(doc any, expr string, paths bool) ([]any, error) {
	if !paths {
		return query.Run(doc, expr)
	}
	matches, err := query.RunMatches(doc, expr)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(matches))
	for _, m := range matches {
		out = append(out, path.Format(m.Path))
	}
	return out, nil
}

func run(c *cobra.Command, opts *options, args []string) error {
	expr := args[0]

	input, closeInput, err := openReadSource(c, args)
	if err != nil {
		return err
	}
	defer closeInput()

	if err := validateReadFlags(c, &opts.readOpts); err != nil {
		return err
	}
	if opts.paths && c.Flags().Changed("default") {
		return usageErr(fmt.Errorf("--default cannot be combined with --paths: a value that isn't in the document has no path"))
	}

	policy, err := resolveMissPolicy(c, opts.readOpts)
	if err != nil {
		return err
	}
	misses := readMisses(policy, opts.readOpts)

	docs, targets, err := readDocs(input, opts.readOpts)
	if err != nil {
		return err
	}

	// A path is per-document, and text mode has nowhere to say which one, so
	// a multi-document listing is ambiguous — identical paths from different
	// documents, and a script built from them edits the first one repeatedly
	// (#159). stderr, so stdout stays pipeable and the exit code is
	// unchanged, the same shape as warnIfSymlink.
	if opts.paths && opts.allDocs && opts.output != "json" && !opts.quiet {
		_, _ = fmt.Fprintln(c.ErrOrStderr(),
			`note: --paths with --all-docs cannot say which document each path is in; use -o json for {"doc","path"} objects`)
	}

	out := c.OutOrStdout()
	var rw *resultWriter
	if !opts.quiet {
		rw = &resultWriter{out: out, format: opts.output, print0: opts.print0}
	}
	for _, i := range targets {
		results, err := queryResults(docs[i], expr, opts.paths)
		if err != nil {
			if err := misses.miss(err); err != nil {
				return pathErr(err)
			}
			// Skipped, and reported after the loop if no document matched at
			// all. Not a `continue`: a missing path is exactly what
			// --default is for, so the substitution below still runs.
			results = nil
		}
		if len(results) == 0 && policy.hasDefault {
			// Substituted before `matched` is decided, not after: asking
			// for a default says a miss is expected and names the answer,
			// so the caller has a value and the run succeeded. -e/-q used
			// to report the opposite while the default went to stdout, so
			// `v=$(yaymlq -e --default fallback .maybe) || die` died
			// holding "fallback" (#208).
			results = []any{policy.value}
		}
		if len(results) > 0 {
			misses.hit()
		}
		if rw == nil {
			continue
		}
		if opts.paths && opts.output == "json" {
			if err := emitPathsJSON(out, i, results); err != nil {
				return ioErr(err)
			}
			continue
		}
		for _, r := range results {
			if err := rw.emit(r); err != nil {
				return err
			}
		}
	}
	if rw != nil {
		if err := rw.flush(); err != nil {
			return err
		}
	}

	if (opts.exitStatus || opts.quiet) && !misses.matched {
		return silentExit{code: 1}
	}
	// Both of these are suppressed under -e/-q/--default (all of them soft),
	// which already report a miss their own way (#173).
	if !policy.soft {
		// Nothing in the stream had the path. Still a failure — --all-docs
		// is lenient about which documents match, not about matching
		// nothing.
		if err := misses.unresolved(); err != nil {
			return pathErr(err)
		}
		if !misses.matched {
			noteWildcardNoMatch(c, expr)
		}
	}
	return nil
}
