package cmd

import (
	"fmt"
	"os"
	"sort"
	"unicode/utf8"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	output   string
	print0   bool
	docIdx   int
	allDocs  bool
	maxBytes int64
}

// newInspectCommand builds a read-only subcommand that resolves <path> and emits
// transform's output for each matched value. keys, len, and type are all built
// this way.
func newInspectCommand(use, short, long, example string, transform func(any) ([]any, error)) *cobra.Command {
	opts := &inspectOptions{output: "raw", maxBytes: defaultMaxBytes}

	cmd := &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         long,
		Example:      example,
		Args:         usageArgs(cobra.RangeArgs(1, 2)),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return handleErr(c, runInspect(c, opts, transform, args), opts.output)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVarP(&opts.print0, "print0", "0", false, "NUL-separate multiple results instead of newline, for xargs -0; implies --output raw")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")

	return cmd
}

func runInspect(c *cobra.Command, opts *inspectOptions, transform func(any) ([]any, error), args []string) error {
	expr := args[0]

	input := c.InOrStdin()
	if len(args) == 2 && args[1] != "-" {
		file, err := os.Open(args[1])
		if err != nil {
			return ioErr(err)
		}
		defer func() { _ = file.Close() }()
		input = file
	}

	format, err := resolveOutputFormat(c, opts.output, opts.print0, false)
	if err != nil {
		return err
	}
	opts.output = format
	if err := validateDocSelection(c, opts.docIdx, opts.allDocs); err != nil {
		return err
	}

	data, err := readCapped(input, opts.maxBytes)
	if err != nil {
		return err
	}
	docs, err := decodeDocs(data, opts.docIdx, opts.allDocs)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return parseErr(fmt.Errorf("no YAML documents on input"))
	}

	targets := []int{opts.docIdx}
	if opts.allDocs {
		targets = targets[:0]
		for i := range docs {
			targets = append(targets, i)
		}
	}

	out := c.OutOrStdout()
	rw := &resultWriter{out: out, format: opts.output, print0: opts.print0}
	for _, i := range targets {
		if i < 0 || i >= len(docs) {
			return usageErr(fmt.Errorf("document index %d out of range (%d documents)", i, len(docs)))
		}
		results, err := query.Run(docs[i], expr)
		if err != nil {
			return pathErr(err)
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
	return rw.flush()
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
