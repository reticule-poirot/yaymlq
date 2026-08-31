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
	if opts.docIdx < 0 || opts.docIdx >= len(docs) {
		return fmt.Errorf("document index %d out of range (%d documents)", opts.docIdx, len(docs))
	}

	if err := mutate(docs, opts.docIdx); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return err
		}
	}
	if err := enc.Close(); err != nil {
		return err
	}

	if opts.inPlace {
		return writeFileAtomic(filename, buf.Bytes())
	}

	_, err = c.OutOrStdout().Write(buf.Bytes())
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
