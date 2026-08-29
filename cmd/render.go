package cmd

import (
	"encoding/json"
	"fmt"
	"io"

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

func renderRaw(w io.Writer, value any) error {
	switch v := value.(type) {
	case nil:
		_, err := fmt.Fprintln(w)
		return err
	case string:
		_, err := fmt.Fprintln(w, v)
		return err
	case bool, int, int64, uint64, float64:
		_, err := fmt.Fprintln(w, v)
		return err
	default:
		// Fall back to compact YAML for maps and lists.
		b, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
}
