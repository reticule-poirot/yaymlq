package cmd

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

// inspectOptions is keys/len/type's flag set, which is the shared read flags
// and nothing else: whatever get offers for resolving a path, these three
// offer too (#213). It stays a named type because one constructor builds all
// three verbs.
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

	bindReadFlags(cmd, &opts.readOpts, "value (parsed as YAML) to print when the path has no match, as given rather than inspected; counts as a match, so -e still exits 0; not valid with -q")

	return cmd
}

func runInspect(c *cobra.Command, opts *inspectOptions, transform func(any) ([]any, error), args []string) error {
	return runRead(c, &opts.readOpts, args, transform)
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
		// not as map[string]any) — key names as the spelling a path
		// addresses them by, which is also the order a wildcard over it
		// visits them in, since the resolver sorts on the same text.
		//
		// query.KeyText, not fmt.Sprint: the latter spells nil "<nil>", a
		// key no expression resolves, so keys advertised one spelling while
		// the rest of the tool answered to another — the same trap #203 took
		// out of the recovery path, left here until #242. It also sorted
		// "<nil>" ahead of every letter, so the "same order wildcards use"
		// the help promises was wrong whenever a null key was present.
		names := make([]string, 0, len(c))
		for k := range c {
			names = append(names, query.KeyText(k))
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
