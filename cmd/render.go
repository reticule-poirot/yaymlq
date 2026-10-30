package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"gopkg.in/yaml.v3"
)

// validOutputFormat reports whether format is a value -o/--output accepts.
// Checked up front by run() and runInspect() — before any output is
// produced, and regardless of --quiet — so an invalid value is a usage
// error (exit 3) rather than surfacing later as an unclassified render()
// failure (exit 1) that --quiet can also skip past entirely, since --quiet
// never calls render() at all.
func validOutputFormat(format string) bool {
	switch format {
	case "", "yaml", "yml", "json", "raw":
		return true
	default:
		return false
	}
}

// render writes value to w in the requested format (yaml|json|raw).
func render(w io.Writer, value any, format string) error {
	switch format {
	case "", "yaml", "yml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(value); err != nil {
			return err
		}
		return enc.Close()
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonKeyed(value))
	case "raw":
		return renderRaw(w, value)
	default:
		return fmt.Errorf("unknown output format %q (want yaml|json|raw)", format)
	}
}

// jsonKeyed rewrites value so every mapping has string keys, which is what a
// JSON object requires: a member name must be a string, and a YAML key need
// not be. yaml.v3 decodes a mapping with any non-string key as map[any]any
// rather than map[string]any, and the encoder refused it outright — a bool,
// null or float key failed the whole render with the encoder's own wording and
// the exit code for a path that didn't resolve, though the path had resolved
// and only the rendering could not be done (#239).
//
// The key's text is query.KeyText, the spelling a path expression addresses it
// by, so a member name read back out of the JSON still names the key it came
// from — `null`, not `<nil>` and not an empty string. An integer key already
// came out this way; the other scalar kinds simply never reached it.
//
// Two distinct keys cannot collide here: yaml.v3 rejects a document holding
// both `1:` and `"1":` as a duplicate key before this runs, so stringifying
// can never merge two members.
func jsonKeyed(value any) any {
	switch v := value.(type) {
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[query.KeyText(k)] = jsonKeyed(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = jsonKeyed(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = jsonKeyed(val)
		}
		return out
	default:
		return value
	}
}

// rawText is value's raw (unquoted) textual representation, the same one
// renderRaw prints, minus its trailing newline.
func rawText(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(v), nil
	default:
		// Fall back to compact YAML for maps and lists.
		b, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(b), "\n"), nil
	}
}

func renderRaw(w io.Writer, value any) error {
	s, err := rawText(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, s)
	return err
}

// resultWriter emits a command's results either through render (one write per
// result, newline by default) or, when print0 is set, buffered as raw text
// joined with NUL — no separator after the last item, for xargs -0. print0
// only makes sense with raw output; callers are expected to have already
// forced format to "raw" (or rejected the combination) before constructing
// one, which is resolveOutputFormat's job.
type resultWriter struct {
	out    io.Writer
	format string
	print0 bool
	parts  []string
}

func (rw *resultWriter) emit(v any) error {
	if !rw.print0 {
		return render(rw.out, v, rw.format)
	}
	s, err := rawText(v)
	if err != nil {
		return err
	}
	rw.parts = append(rw.parts, s)
	return nil
}

// flush writes the buffered NUL-joined output; a no-op unless print0 is set.
func (rw *resultWriter) flush() error {
	if !rw.print0 {
		return nil
	}
	_, err := io.WriteString(rw.out, strings.Join(rw.parts, "\x00"))
	return err
}
