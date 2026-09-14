package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// editOpts holds the flags shared by every document-editing subcommand (set,
// delete). It is embedded in each command's option struct.
type editOpts struct {
	inPlace  bool
	docIdx   int
	maxBytes int64
	indent   int
}

// applyEdit is the read → mutate → write pipeline behind the editing
// subcommands. It reads YAML from src (closing it via closeSrc before any write,
// which Windows requires before os.Rename), runs mutate against the doc at
// opts.docIdx, then either rewrites filename (opts.inPlace) or writes the whole
// stream to the command's stdout.
func applyEdit(c *cobra.Command, src io.Reader, closeSrc func() error, filename string, opts editOpts, mutate func(docs []*yaml.Node, i int) error) error {
	data, err := readCapped(src, opts.maxBytes)
	if closeSrc != nil {
		_ = closeSrc()
	}
	if err != nil {
		return err
	}

	docs, err := decodeNodes(data)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no YAML documents on input")
	}
	for _, d := range docs {
		preserveBlankLines(d, data)
	}
	if opts.docIdx < 0 || opts.docIdx >= len(docs) {
		return fmt.Errorf("document index %d out of range (%d documents)", opts.docIdx, len(docs))
	}

	if err := mutate(docs, opts.docIdx); err != nil {
		return err
	}

	indent := opts.indent
	if c.Flags().Changed("indent") {
		if indent < 1 {
			return fmt.Errorf("--indent must be at least 1, got %d", indent)
		}
	} else if n := detectIndent(data); n > 0 {
		indent = n
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return err
		}
	}
	if err := enc.Close(); err != nil {
		return err
	}
	out := tidyBlankLines(buf.Bytes())
	if hasCRLF(data) {
		out = restoreCRLF(out)
	}

	if opts.inPlace {
		return writeFileAtomic(filename, out)
	}

	_, err = c.OutOrStdout().Write(out)
	return err
}

// writeFileAtomic replaces name's contents in a way that never leaves a
// truncated file behind: it writes a sibling temp file, flushes it to disk,
// then renames it over name (atomic on the same filesystem). If name is a
// symlink it is replaced, not written through. The target's permission bits are
// preserved (new files default to 0644).
func writeFileAtomic(name string, data []byte) error {
	dir := filepath.Dir(name)

	perm := os.FileMode(0o644)
	if info, err := os.Stat(name); err == nil {
		perm = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(name)+".yaymlq-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, name)
}

// detectIndent returns the source document's indent width in spaces — the
// smallest nonzero amount of leading whitespace on any line — or 0 if there's
// nothing to measure (a flat document, or one with no indented lines at all).
// Used as the default for --indent so re-serializing a 4-space file doesn't
// silently reflow it to yaml.v3's default of 2.
func detectIndent(source []byte) int {
	best := 0
	for _, line := range bytes.Split(source, []byte("\n")) {
		trimmed := bytes.TrimLeft(line, " ")
		n := len(line) - len(trimmed)
		if n == 0 || len(trimmed) == 0 {
			continue // unindented, or blank/whitespace-only
		}
		if best == 0 || n < best {
			best = n
		}
	}
	return best
}

func decodeNodes(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		docs = append(docs, &n)
	}
	return docs, nil
}
