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

Input is read to EOF before any output is produced, so a pipe that stays open
produces nothing: yaymlq queries a document, it does not follow a stream.

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

	bindReadFlags(cmd, &opts.readOpts, "value (parsed as YAML) to print when the path has no match; counts as a match, so -e still exits 0; not valid with -q")
	cmd.Flags().BoolVar(&opts.paths, "paths", false, "print each match's resolved path instead of its value, for feeding back into apply; implies -o raw")

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
	return runRead(c, &opts.readOpts, args, func(v any) ([]any, error) { return []any{v}, nil })
}

// runRead is the pipeline every read verb (get, keys, len, type) shares:
// open input, validate flags, decide the miss policy, decode the stream,
// then for each target document resolve expr and hand every match to
// transform, which says what to print for it. get's transform is identity
// (one match, one value); keys/len/type's is their own one-to-many rule
// (newInspectCommand).
//
// get's own features — --paths, and the --paths+-o json and
// --paths+--all-docs cases below — are harmless no-ops for the other three,
// since opts.paths is always false where they don't apply (inspectOptions
// never sets it).
func runRead(c *cobra.Command, opts *readOpts, args []string, transform func(any) ([]any, error)) error {
	expr := args[0]

	input, closeInput, err := openReadSource(c, args)
	if err != nil {
		return err
	}
	defer closeInput()

	if err := validateReadFlags(c, opts); err != nil {
		return err
	}
	if opts.paths && c.Flags().Changed("default") {
		return usageErr(fmt.Errorf("--default cannot be combined with --paths: a value that isn't in the document has no path"))
	}

	policy, err := resolveMissPolicy(c, *opts)
	if err != nil {
		return err
	}
	misses := readMisses(policy, *opts)

	docs, targets, err := readDocs(input, *opts)
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
			// all. The transform below has nothing to run on, so --default
			// is what fills the gap, exactly as it does on get.
			results = nil
		}
		// Transformed even under -q, which suppresses output and not the
		// question: a path that resolved and then turned out to be the wrong
		// kind of thing is a failure this verb can't answer its way out of,
		// not a missing path, so it is reported rather than counted as a
		// miss.
		var vals []any
		for _, r := range results {
			got, err := transform(r)
			if err != nil {
				return err
			}
			vals = append(vals, got...)
		}
		switch {
		case len(results) > 0:
			// The path, not the transform's output: keys of an empty mapping
			// emits nothing, and that is a path that resolved.
			misses.hit()
		case policy.hasDefault:
			// The default stands in for the answer, not for the value this
			// verb would have been handed — `len --default 0` answers 0 for
			// a path that isn't there rather than measuring the 0 — and it
			// counts as a match for the same reason it does on get (#208).
			vals = []any{policy.value}
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
		for _, v := range vals {
			if err := rw.emit(v); err != nil {
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
