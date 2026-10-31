package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// editOpts holds the flags shared by every document-editing subcommand (set,
// delete). It is embedded in each command's option struct.
type editOpts struct {
	inPlace    bool
	diff       bool
	showDiff   bool
	diffFormat string
	docIdx     int
	maxBytes   int64
	indent     int
	maxSugg    int
	allDocs    bool
	// Only set and apply register --must-exist: delete, rename and append
	// already fail on a key the document doesn't have, so offering it there
	// would be a flag that promises something the command already does.
	mustExist bool
}

// editSource is the document an editing verb works on: the reader it comes
// from, the closer applyEdit calls before any write (Windows refuses to
// rename over an open file), and the filename — empty when the document
// arrived on stdin, which is the case --in-place has nothing to rewrite.
type editSource struct {
	reader   io.Reader
	close    func() error
	filename string
}

// editFileArg resolves the optional trailing file argument. fileArg is its
// position: 0 for apply, 1 for delete, 2 for set, append and rename, each of
// which takes its path — and its value or new key — first. A "-" there means
// stdin, the same as leaving it out.
//
// It also enforces the one rule every editing verb shares and none may skip:
// --in-place rewrites the file it was given, so there is nothing to rewrite
// when the document came from stdin. The check lives with the extraction
// because it is about what the extraction produced, and it runs before a verb
// parses its own arguments — the order each verb had when it spelled this out
// itself, four copies of it (#211).
func editFileArg(args []string, fileArg int, inPlace bool) (string, error) {
	filename := ""
	if len(args) > fileArg && args[fileArg] != "-" {
		filename = args[fileArg]
	}
	if inPlace && filename == "" {
		return "", usageErr(errors.New("--in-place needs a file argument"))
	}
	return filename, nil
}

// openEditSource opens filename, or takes the document from stdin when it is
// empty. Called once a verb has parsed its own arguments, so a bad path or an
// unparseable value never leaves an open file behind.
//
// The gosec exemption is the cost of sharing this: filename is the file
// argument the user typed, and opening the file you were asked to edit is
// what the command is for. The four per-verb opens this replaced went
// unflagged because the taint was resolvable within each function — as a
// parameter it no longer is, which is the failure apply.go's own
// script-opening comment warned that extraction would reintroduce.
func openEditSource(c *cobra.Command, filename string) (editSource, error) {
	src := editSource{reader: c.InOrStdin(), filename: filename}
	if filename == "" {
		return src, nil
	}
	file, err := os.Open(filename) //nolint:gosec // G304: filename is the user's own file argument, see above
	if err != nil {
		return editSource{}, ioErr(err)
	}
	src.reader, src.close = file, file.Close
	return src, nil
}

// applyEdit is the read → mutate → write pipeline behind the editing
// subcommands. It reads YAML from src (closing it before any write, which
// Windows requires before os.Rename), runs mutate against the doc at
// opts.docIdx, then either rewrites src.filename (opts.inPlace) or writes the
// whole stream to the command's stdout.
func applyEdit(c *cobra.Command, src editSource, opts editOpts, mutate func(docs []*yaml.Node, targets []int) error) error {
	data, err := readCapped(src.reader, opts.maxBytes)
	if src.close != nil {
		_ = src.close()
	}
	// All three run before any write, so a rejected invocation leaves the
	// target untouched — #118 was a check of this family that didn't fire,
	// and it turned `set -i --diff-format json` into a silent write.
	if opts.showDiff && !opts.inPlace {
		return usageErr(fmt.Errorf("--show-diff requires --in-place/-i; without it the edited document already goes to stdout"))
	}
	if opts.showDiff && opts.diff {
		return usageErr(fmt.Errorf("--diff and --show-diff are contradictory: --diff previews without writing, --show-diff writes and then prints the diff"))
	}
	// Checked before the format value itself: --diff-format only affects the
	// two branches that render a diff, so accepting it without either would
	// silently fall through to the in-place write.
	if c.Flags().Changed("diff-format") && !opts.diff && !opts.showDiff {
		return usageErr(fmt.Errorf("--diff-format requires --diff, --dry-run, or --show-diff"))
	}
	if !validDiffFormat(opts.diffFormat) {
		return usageErr(fmt.Errorf("unknown --diff-format %q (want text|json)", opts.diffFormat))
	}
	if err := validateMaxSuggestions(opts.maxSugg); err != nil {
		return err
	}
	if err != nil {
		return err
	}

	// Split once, here, and shared by every fixup that reads the raw text:
	// each used to split the input its own way, which is how they came to
	// disagree with the parser about where a line ends (#254). Computed per
	// stream rather than per document, which would redo this full-input scan
	// for every document — O(documents × input size) instead of O(input
	// size) (#71).
	lines := sourceLines(data)

	docs, err := decodeNodes(data, lines)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		// Unclassified on purpose, so it exits 1 rather than 2: the stream
		// parsed fine, it just holds nothing to query or edit (#209).
		return fmt.Errorf("no YAML documents on input")
	}
	// Recorded while Node.Line still lines up with source, before any
	// mutation (#250).
	blocks := recordBlockScalars(docs, lines, data)
	markers := recordDocMarkers(docs, lines)
	blank := blankLines(lines)
	// Before the marks are made: a blank line inside a block scalar is part of
	// a value, not spacing before the next node (#254).
	dropBlockContentBlanks(blank, blocks)
	if len(blank) > 0 {
		for _, d := range docs {
			markBlankLines(d, blank)
		}
	}
	gutters := make(map[string][]int)
	for _, d := range docs {
		recordCommentGutters(d, lines, gutters)
	}
	if err := validateDocSelection(c, opts.docIdx, opts.allDocs); err != nil {
		return err
	}
	targets, err := docTargets(opts.docIdx, opts.allDocs, len(docs))
	if err != nil {
		return err
	}

	if err := mutate(docs, targets); err != nil {
		// A key miss carries the mapping's keys (ymledit.KeyError); every
		// other failure passes through untouched. Annotated here, the one
		// place every editing command's mutate returns through, rather than
		// per command.
		return annotateNotFound(err, opts.maxSugg)
	}

	indent := opts.indent
	if c.Flags().Changed("indent") {
		if indent < 1 {
			return usageErr(fmt.Errorf("--indent must be at least 1, got %d", indent))
		}
	} else if n := detectIndent(lines); n > 0 {
		indent = n
	}

	// After the mutation, so a block scalar the edit replaced is recognised as
	// replaced and emitted for what it now is.
	maskBlockScalars(blocks)

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
	// Before widenCommentGutters, so the spliced-back comment is just another
	// one-space gutter for it to restore to the source's width (#172).
	out = rejoinStrandedTokens(out, lines)
	out = widenCommentGutters(out, gutters)
	// After both line scans, so a block's body is never read as a comment
	// gutter or an anchor to rejoin, and before the line endings, so the
	// restored lines get them too.
	out = restoreBlockScalars(out, blocks)
	// After every restore that matches on text, so a block's body is never read
	// as a document boundary, and before the line endings, so the markers get
	// them too.
	out = restoreDocMarkers(out, markers)
	out = restoreLineEnding(out, sourceLineEnding(data))

	if opts.diff {
		return ioErr(emitDiff(c, src.filename, data, out, opts.diffFormat))
	}

	if opts.inPlace {
		warnIfSymlink(c, src.filename)
		if err := writeFileAtomic(src.filename, out); err != nil {
			return ioErr(err)
		}
		// Written first, then reported: the diff is an account of a change
		// that already happened, so it must never be printed for a write
		// that failed.
		if opts.showDiff {
			return ioErr(emitDiff(c, src.filename, data, out, opts.diffFormat))
		}
		return nil
	}

	_, err = c.OutOrStdout().Write(out)
	return ioErr(err)
}

// editTargets runs edit against each target document under docMisses' rule,
// which is the edit side of the leniency #170 and #175 settled: a document
// the path doesn't resolve in is skipped, and the run fails only if no
// document matched, so one flag spelled the same way on both sides means the
// same thing.
//
// The line is ymledit.ErrNotResolved — a missing key, a scalar in the way, an
// index out of range, an empty document — which is this half's own sentinel;
// see docMisses for why the rule takes it as a parameter rather than knowing
// about either half.
//
// With one target this is just edit(target) plus its error, which is what
// every single-document edit still is.
func editTargets(targets []int, edit func(i int) error) error {
	misses := docMisses{lenient: true, notResolved: ymledit.ErrNotResolved}
	for _, i := range targets {
		err := edit(i)
		if err == nil {
			misses.hit()
			continue
		}
		if err := misses.miss(err); err != nil {
			return err
		}
	}
	return misses.unresolved()
}

// allDocsSkips and allDocsCreates are the two things --all-docs can mean to
// an editing verb. It selects documents rather than asserting every one has
// the path (#170/#175), so a document the path doesn't resolve in is skipped
// — except on a verb that creates what it can't find, where nothing is ever
// skipped and the key lands in every document, --must-exist being the opt-out
// (#164). #202 was all five verbs sharing the first wording.
const (
	allDocsSkips   = "edit every document in the stream, skipping those the path isn't in"
	allDocsCreates = "edit every document in the stream; a document without the path has it created, not skipped — --must-exist skips instead"
)

// bindEditFlags registers the flags every editing subcommand shares, and is
// called by set/append (via bindValueEditFlags), delete, rename and apply —
// one registration point, so none of them can end up offering a different
// subset.
//
// allDocsUsage is the one value the callers don't share: the verbs that
// create pass allDocsCreates, the three that require an existing path pass
// allDocsSkips. Which verbs those are is observable from the tree — they are
// the ones offering --must-exist — and TestAllDocsUsageSaysWhetherTheVerbCreates
// holds the two in agreement.
//
// --diff and --dry-run both write into the same opts.diff bool: --dry-run is
// the more conventional CLI name, --diff says exactly what you get.
// --max-suggestions is the same flag get/keys/len/type carry, since an edit
// that misses a key reports it the same way a read does.
func bindEditFlags(cmd *cobra.Command, opts *editOpts, allDocsUsage string) {
	f := cmd.Flags()
	f.IntVar(&opts.maxSugg, "max-suggestions", defaultMaxSuggestions, "max keys a \"no such key\" error lists; 0 = all of them")
	const usage = "print a unified diff of the change instead of writing or printing the document"
	f.BoolVar(&opts.diff, "diff", false, usage)
	f.BoolVar(&opts.diff, "dry-run", false, usage+" (alias for --diff)")
	f.BoolVar(&opts.allDocs, "all-docs", false, allDocsUsage)
	f.BoolVar(&opts.showDiff, "show-diff", false, "with --in-place/-i: write the file and print a unified diff of what changed")
	f.StringVar(&opts.diffFormat, "diff-format", "text", "--diff/--dry-run/--show-diff output format: text|json")
}

// emitDiff renders the change between old and updated in the requested
// format. Shared by --diff (which renders instead of writing) and
// --show-diff (which renders after writing), so the two can't drift apart.
func emitDiff(c *cobra.Command, filename string, old, updated []byte, format string) error {
	name := filename
	if name == "" {
		name = "stdin"
	}
	if format == "json" {
		return writeDiffJSON(c.OutOrStdout(), name, old, updated)
	}
	_, err := io.WriteString(c.OutOrStdout(), unifiedDiff(name, old, updated))
	return err
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
func detectIndent(lines []string) int {
	best := 0
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
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

func decodeNodes(data []byte, srcLines []string) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	explicitLines := mergeTagLines(data)
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, explainParseError(data, err)
		}
		// The map-decode path rejects a collection used as a mapping key;
		// without this the node path would accept a template directive and
		// rewrite it into explicit-key form on the way out.
		if err := rejectMappingKeys(&n, data); err != nil {
			return nil, err
		}
		// The decoder tags `<<` explicitly and the encoder emits what it was
		// given, so without this every edit rewrites every merge key in the
		// document (#171).
		normalizeMergeTags(&n, explicitLines)
		// An anchored key's comment is decoded onto its value's first child,
		// so without this every edit moves it a line down (#172).
		restoreKeyComments(&n, srcLines)
		docs = append(docs, &n)
	}
	return docs, nil
}
