package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// editOpts holds the flags shared by every document-editing subcommand (set,
// delete). It is embedded in each command's option struct.
type editOpts struct {
	inPlace  bool
	diff     bool
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
		return parseErr(fmt.Errorf("no YAML documents on input"))
	}
	// blankLines(data) scans the whole raw input once; computed here rather
	// than inside a per-document preserveBlankLines call (see its doc
	// comment), which used to redo that full scan for every document in the
	// stream — O(documents × input size) instead of O(input size).
	if blank := blankLines(data); len(blank) > 0 {
		for _, d := range docs {
			markBlankLines(d, blank)
		}
	}
	// commentGutterLines(data) scans the whole raw input once; computed here
	// rather than inside a per-document recordCommentGutters call (see its
	// doc comment), which would redo that full split for every document in
	// the stream — O(documents × input size) instead of O(input size).
	gutters := make(map[string][]int)
	lines := commentGutterLines(data)
	for _, d := range docs {
		recordCommentGutters(d, lines, gutters)
	}
	if opts.docIdx < 0 || opts.docIdx >= len(docs) {
		return usageErr(fmt.Errorf("document index %d out of range (%d documents)", opts.docIdx, len(docs)))
	}

	if err := mutate(docs, opts.docIdx); err != nil {
		return err
	}

	indent := opts.indent
	if c.Flags().Changed("indent") {
		if indent < 1 {
			return usageErr(fmt.Errorf("--indent must be at least 1, got %d", indent))
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
	out = widenCommentGutters(out, gutters)
	if hasCRLF(data) {
		out = restoreCRLF(out)
	}

	if opts.diff {
		name := filename
		if name == "" {
			name = "stdin"
		}
		_, err = io.WriteString(c.OutOrStdout(), unifiedDiff(name, data, out))
		return ioErr(err)
	}

	if opts.inPlace {
		warnIfSymlink(c, filename)
		return ioErr(writeFileAtomic(filename, out))
	}

	_, err = c.OutOrStdout().Write(out)
	return ioErr(err)
}

// bindDiffFlag registers --diff and --dry-run on cmd, both writing into the
// same opts.diff bool: --dry-run is the more conventional CLI name, --diff
// says exactly what you get. Shared by set/append (bindValueEditFlags) and
// delete/rename, so all four editing subcommands accept either spelling.
func bindDiffFlag(cmd *cobra.Command, opts *editOpts) {
	f := cmd.Flags()
	const usage = "print a unified diff of the change instead of writing or printing the document"
	f.BoolVar(&opts.diff, "diff", false, usage)
	f.BoolVar(&opts.diff, "dry-run", false, usage+" (alias for --diff)")
}

// writeFileAtomic replaces name's contents in a way that never leaves a
// truncated file behind: it writes a sibling temp file, flushes it to disk,
// then renames it over name (atomic on the same filesystem). If name is a
// symlink it is replaced, not written through. The target's POSIX permission
// bits are preserved — ownership, ACLs, and extended attributes are not,
// since the file is replaced rather than modified in place.
func writeFileAtomic(name string, data []byte) error {
	dir := filepath.Dir(name)

	// Every caller has just successfully opened name for reading, so a Stat
	// failure here means the ground shifted underneath us (removed or
	// replaced concurrently) — that's worth failing on, not papering over
	// with an invented permission that ignores the process umask.
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(name)+".yaymlq-*")
	if err != nil {
		// Reported against name, not the generated temp file: os.CreateTemp's
		// own error (a *fs.PathError) names the random temp filename it tried
		// to create (e.g. ".c.yaml.yaymlq-3070855576"), which is confusing to
		// see for someone who doesn't know --in-place writes a sibling file
		// first. The underlying cause (e.g. "permission denied") is kept via
		// %w; only the path component of the PathError is dropped.
		cause := err
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			cause = pathErr.Err
		}
		return fmt.Errorf("cannot create a temp file to write %s atomically: %w", name, cause)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// Chmod the open descriptor, not the path: os.Chmod follows symlinks, so
	// chmod-by-name would let a symlink swapped in at tmpName (visible to a
	// directory watcher the instant CreateTemp creates it) redirect the
	// permission change onto an attacker-chosen target instead of this temp
	// file. A descriptor-based chmod can't be redirected by a name change.
	if err := tmp.Chmod(perm); err != nil {
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
	if err := os.Rename(tmpName, name); err != nil {
		return err
	}

	// Best-effort: flush dir's own directory-entry metadata now that the
	// rename into it has succeeded, so a completed edit is less likely to
	// be lost to power loss even though its content was already fsync'd
	// before the rename. Errors are deliberately ignored — this only
	// strengthens a durability guarantee already met without it, it's a
	// no-op wherever syncing a directory handle isn't meaningful (including
	// Windows), and it must never turn an already-successful edit into a
	// reported failure. dir is filepath.Dir(name), and name was already
	// opened for reading by the caller before writeFileAtomic ever runs —
	// this isn't fresh untrusted input, just gosec's G304 unable to trace
	// taint through filepath.Dir.
	if f, err := os.Open(dir); err == nil { //nolint:gosec // G304: dir derives from a path the caller already opened
		_ = f.Sync()
		_ = f.Close()
	}
	return nil
}

// warnIfSymlink prints a one-line stderr note when name is a symlink, since
// writeFileAtomic replaces the link itself rather than writing through it —
// an edit to a symlinked path silently never reaches whatever it points at.
// Best-effort: an Lstat failure here isn't reported, since writeFileAtomic
// will surface the real error shortly after.
func warnIfSymlink(c *cobra.Command, name string) {
	info, err := os.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return
	}
	target, err := os.Readlink(name)
	if err != nil {
		target = "its target"
	}
	_, _ = fmt.Fprintf(c.ErrOrStderr(), "note: %s is a symlink; replacing the link, not %s\n", name, target)
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
			return nil, parseErr(fmt.Errorf("parsing YAML: %w", err))
		}
		docs = append(docs, &n)
	}
	return docs, nil
}
