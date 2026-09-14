package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

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
		return enc.Encode(value)
	case "raw":
		return renderRaw(w, value)
	default:
		return fmt.Errorf("unknown output format %q (want yaml|json|raw)", format)
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
// one, the same way --raw does.
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
