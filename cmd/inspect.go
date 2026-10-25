package cmd

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

// inspectOptions is keys/len/type's flag set, which is currently the shared
// read flags and nothing else. It stays a named type because the three verbs
// are built by one constructor and because the flags they don't yet offer —
// -e, -q, --default (#213) — belong here rather than in readOpts, which get
// shares.
type inspectOptions struct {
	readOpts
}

// newInspectCommand builds a read-only subcommand that resolves <path> and emits
// transform's output for each matched value. keys, len, and type are all built
// this way.
func newInspectCommand(use, short, long, example string, transform func(any) ([]any, error)) *cobra.Command {
	opts := &inspectOptions{readOpts{output: "raw", maxBytes: defaultMaxBytes, maxSugg: defaultMaxSuggestions}}

	cmd := &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         long,
		Example:      example,
		Args:         usageArgs(cobra.RangeArgs(1, 2)),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return handleErr(c, runInspect(c, opts, transform, args), opts.output, opts.maxSugg)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVarP(&opts.print0, "print0", "0", false, "NUL-separate multiple results instead of newline, for xargs -0; implies --output raw")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")
	f.IntVar(&opts.maxSugg, "max-suggestions", opts.maxSugg, "max keys a \"path not found\" error lists; 0 = all of them")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")

	return cmd
}

func runInspect(c *cobra.Command, opts *inspectOptions, transform func(any) ([]any, error), args []string) error {
	expr := args[0]

	input, closeInput, err := openReadSource(c, args)
	if err != nil {
		return err
	}
	defer closeInput()

	if err := validateReadFlags(c, &opts.readOpts); err != nil {
		return err
	}

	docs, targets, err := readDocs(input, opts.readOpts)
	if err != nil {
		return err
	}

	out := c.OutOrStdout()
	rw := &resultWriter{out: out, format: opts.output, print0: opts.print0}
	// Same rule as get's, with --all-docs as the only way into it: these
	// verbs offer none of get's -e/-q/--default, so there is no other way to
	// say a miss was expected (#213).
	misses := docMisses{lenient: opts.allDocs, notResolved: query.ErrNotFound}
	for _, i := range targets {
		results, err := query.Run(docs[i], expr)
		if err != nil {
			// A transform error below is deliberately not covered — a path
			// that resolved and then turned out to be the wrong kind of
			// thing is a question this verb genuinely can't answer, not a
			// missing path.
			if err := misses.miss(err); err != nil {
				return pathErr(err)
			}
			continue
		}
		if len(results) > 0 {
			misses.hit()
		}
		for _, r := range results {
			vals, err := transform(r)
			if err != nil {
				return err
			}
			for _, v := range vals {
				if err := rw.emit(v); err != nil {
					return err
				}
			}
		}
	}
	if err := rw.flush(); err != nil {
		return err
	}
	// No document in the stream had the path: lenient about which documents
	// match, not about matching nothing.
	if err := misses.unresolved(); err != nil {
		return pathErr(err)
	}
	// Same gap as get's, and these verbs have no -e/-q/--default to opt out
	// of it (#173).
	if !misses.matched {
		noteWildcardNoMatch(c, expr)
	}
	return nil
}

// inspectKeys lists a mapping's keys (sorted, matching the tool's wildcard
// ordering) or a list's indices.
func inspectKeys(v any) ([]any, error) {
	switch c := v.(type) {
	case map[string]any:
		names := make([]string, 0, len(c))
		for k := range c {
			names = append(names, k)
		}
		sort.Strings(names)
		out := make([]any, len(names))
		for i, k := range names {
			out[i] = k
		}
		return out, nil
	case map[any]any:
		// A mapping with any non-string key (yaml.v3 decodes it this way,
		// not as map[string]any) — key names as their string form, same
		// order a wildcard over it visits them in.
		names := make([]string, 0, len(c))
		for k := range c {
			names = append(names, fmt.Sprint(k))
		}
		sort.Strings(names)
		out := make([]any, len(names))
		for i, k := range names {
			out[i] = k
		}
		return out, nil
	case []any:
		out := make([]any, len(c))
		for i := range c {
			out[i] = i
		}
		return out, nil
	default:
		return nil, fmt.Errorf("keys: a %s has no keys", jsonType(v))
	}
}

// inspectLen reports the entry count of a mapping or list, the rune count of a
// string, or 0 for null.
func inspectLen(v any) ([]any, error) {
	switch c := v.(type) {
	case map[string]any:
		return []any{len(c)}, nil
	case map[any]any:
		return []any{len(c)}, nil
	case []any:
		return []any{len(c)}, nil
	case string:
		return []any{utf8.RuneCountInString(c)}, nil
	case nil:
		return []any{0}, nil
	default:
		return nil, fmt.Errorf("len: a %s has no length", jsonType(v))
	}
}

// inspectType names a value using JSON's type vocabulary.
func inspectType(v any) ([]any, error) {
	return []any{jsonType(v)}, nil
}

// jsonType maps a decoded YAML value to null|boolean|number|string|array|object.
func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case int, int64, uint64, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any, map[any]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
