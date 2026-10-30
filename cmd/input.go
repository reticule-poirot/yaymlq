package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// defaultMaxBytes caps how much input yaymlq will buffer. It guards against
// memory exhaustion from an oversized or hostile file/stream. A value of 0 on
// the --max-bytes flag disables the cap.
const defaultMaxBytes int64 = 64 << 20 // 64 MiB

// errInputTooLarge is returned when the input exceeds the configured cap.
var errInputTooLarge = errors.New("input too large")

// readOpts is the part of a read verb's flags the shared prologue owns: what
// get and keys/len/type both ask for, in the same words. Embedded in options
// and inspectOptions, so each verb still binds its flags to one struct and
// opts.output still reads as its own field.
//
// paths is the one member only get registers — --paths is a route from a
// query to an edit, which keys/len/type have nothing to offer. It stays false
// for them, which is the value runInspect used to pass resolveOutputFormat by
// hand.
type readOpts struct {
	output   string
	print0   bool
	paths    bool
	docIdx   int
	allDocs  bool
	maxBytes int64
	maxSugg  int

	// The three flags that say a path missing is expected rather than an
	// error, shared by all four read verbs since #213. resolveMissPolicy
	// settles what they mean together.
	quiet      bool
	exitStatus bool
	defValue   string

	// asString turns off the YAML parse of defValue, the same question -s
	// answers about set/append's <value>. A placeholder is the usual thing to
	// hand --default, and the likeliest one, `-`, is a YAML sequence (#232).
	asString bool
}

// readCapped reads all of r into memory, refusing to buffer more than limit
// bytes. limit == 0 means unlimited; a negative limit is a usage error — it
// can only come from a misconfigured --max-bytes (e.g. a wrapper script's
// arithmetic going negative), and silently treating it as "unlimited" would
// turn that mistake into exactly the exposure the flag exists to prevent.
func readCapped(r io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, usageErr(fmt.Errorf("--max-bytes must be >= 0 (0 = unlimited), got %d", limit))
	}
	if limit == 0 {
		data, err := io.ReadAll(r)
		return data, ioErr(err)
	}
	// Read exactly limit bytes (never limit+1: that overflows and silently
	// disables the cap when limit == math.MaxInt64). If that fills the
	// buffer, one more byte is read directly from r to tell "input is
	// exactly limit bytes" from "input is larger and got cut off".
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, ioErr(err)
	}
	if int64(len(data)) == limit {
		var extra [1]byte
		n, err := io.ReadFull(r, extra[:])
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, ioErr(err)
		}
		if n > 0 {
			return nil, ioErr(fmt.Errorf("%w: exceeds %d bytes; raise --max-bytes (0 = unlimited) to override", errInputTooLarge, limit))
		}
	}
	return data, nil
}

// validateDocSelection checks --doc/--all-docs for a usage error before any
// input is read: --doc explicitly set together with --all-docs is a silent
// contradiction otherwise (targets ends up built from every document,
// discarding --doc entirely with no warning), and a negative --doc is
// always out of range — rejecting it up front avoids decodeDocs' early-stop
// optimization being defeated (a negative want never satisfies its
// len(docs) > want check, so the whole stream gets decoded first) for no
// benefit, since the index was never going to be valid.
func validateDocSelection(c *cobra.Command, docIdx int, allDocs bool) error {
	if allDocs && c.Flags().Changed("doc") {
		return usageErr(errors.New("--doc and --all-docs cannot be used together"))
	}
	if !allDocs && docIdx < 0 {
		return usageErr(fmt.Errorf("--doc must be >= 0, got %d", docIdx))
	}
	return nil
}

// resolveOutputFormat settles the output format from -o, --print0 and
// --paths, and rejects a contradiction between them, returning the format to
// use.
//
// The order matters and is the whole reason this is one function: --print0
// and --paths both mean raw output, so their conflict check has to run
// *before* the assignment below it, or it compares "raw" against "raw" and
// never fires. That is exactly what #123 was — a removed --raw alias
// assigned first, silently switching the check off — and keeping the two
// steps together is what stops a caller reintroducing it by doing them in
// the wrong order.
//
// It takes the resolved value rather than a default because root starts at
// "yaml" and the inspect verbs at "raw"; neither default is referenced here,
// only Changed("output") and the current value, so both callers share it
// unchanged.
func resolveOutputFormat(c *cobra.Command, output string, print0, paths bool) (string, error) {
	// Both flags emit plain text rather than a rendered value, so both
	// force raw and both reject an -o that contradicts it. The exception is
	// --paths with -o json, which is not a rendered value either: it is the
	// one form that can carry a path's document index alongside it (#159),
	// so it is allowed through and handled by the caller.
	pathsJSON := paths && c.Flags().Changed("output") && output == "json"
	forced := ""
	switch {
	case print0:
		forced = "--print0/-0"
	case paths && !pathsJSON:
		forced = "--paths"
	}
	if forced != "" {
		if c.Flags().Changed("output") && output != "raw" {
			return "", usageErr(fmt.Errorf("%s only makes sense with raw output, not -o %s", forced, output))
		}
		output = "raw"
	}
	if !validOutputFormat(output) {
		return "", usageErr(fmt.Errorf("unknown output format %q (want yaml|json|raw)", output))
	}
	return output, nil
}

// decodeDocs decodes the YAML document stream in data.
//
// When all is false, decoding stops as soon as document index `want` has been
// read, so a huge trailing stream is never parsed just to reach an early
// document.
func decodeDocs(data []byte, want int, all bool) ([]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// yaml.v3 (>= v3.0.1) itself rejects alias-expansion bombs with
			// "excessive aliasing"; surface that and any other parse error.
			return nil, explainParseError(data, err)
		}
		docs = append(docs, doc)
		if !all && want >= 0 && len(docs) > want {
			break
		}
	}
	return docs, nil
}

// openReadSource opens the optional trailing file argument, or takes the
// document from the command's stdin when there is none, or when it is "-".
// Both read verbs spell their arguments "<path> [file]", so the file is
// always args[1] — unlike the editing verbs, whose own value or new key comes
// first and which need editFileArg's position parameter.
//
// The returned close is never nil and can be deferred unconditionally; for
// stdin it does nothing.
//
// It needs no gosec exemption where openEditSource, opening the same kind of
// file, does: G304 fires on a filename that arrives as its own parameter, and
// here the path is read out of args. That is a difference in gosec's taint
// tracking, not in what the two functions do — in both the path is the file
// argument the user typed.
func openReadSource(c *cobra.Command, args []string) (io.Reader, func(), error) {
	if len(args) < 2 || args[1] == "-" {
		return c.InOrStdin(), func() {}, nil
	}
	file, err := os.Open(args[1])
	if err != nil {
		return nil, nil, ioErr(err)
	}
	return file, func() { _ = file.Close() }, nil
}

// validateReadFlags makes the checks both read verbs make before reading
// anything, in the order they both made them, and settles o.output to the
// format the run will actually use.
//
// resolveOutputFormat is called here rather than left to render() so a bad -o
// value is a usage error (exit 3) even under --quiet, which never calls
// render() at all.
func validateReadFlags(c *cobra.Command, o *readOpts) error {
	format, err := resolveOutputFormat(c, o.output, o.print0, o.paths)
	if err != nil {
		return err
	}
	o.output = format
	if err := validateDocSelection(c, o.docIdx, o.allDocs); err != nil {
		return err
	}
	return validateMaxSuggestions(o.maxSugg)
}

// readDocs reads the whole input and decodes it into the documents a read
// verb works on, with the indexes of the ones this run covers.
//
// It is called after the verb's own flag checks, not before: which errors a
// mistaken invocation reports first is part of the surface, and reading
// megabytes to then reject the flags would change it.
func readDocs(input io.Reader, o readOpts) ([]any, []int, error) {
	data, err := readCapped(input, o.maxBytes)
	if err != nil {
		return nil, nil, err
	}
	docs, err := decodeDocs(data, o.docIdx, o.allDocs)
	if err != nil {
		return nil, nil, err
	}
	if len(docs) == 0 {
		// Unclassified on purpose, so it exits 1 rather than 2: the stream
		// parsed fine, it just holds nothing to query or edit (#209).
		return nil, nil, fmt.Errorf("no YAML documents on input")
	}
	targets, err := docTargets(o.docIdx, o.allDocs, len(docs))
	if err != nil {
		return nil, nil, err
	}
	return docs, targets, nil
}

// docTargets lists the documents a run covers: every one of them under
// --all-docs, or the single one --doc selected. n is how many were decoded
// and is expected to be positive — an empty stream is its caller's error to
// report (#209), not a run over no documents.
//
// It is also the only bounds check on --doc, for the read and edit sides
// alike. There used to be three: inside the loop in run() and runInspect(),
// each with its own i < 0 guard, and before the loop in applyEdit() without
// one, relying on validateDocSelection having rejected a negative --doc
// earlier (#212). A list built in one place can be checked in one place, and
// the guard is kept rather than inherited, so the check stands on its own.
func docTargets(docIdx int, allDocs bool, n int) ([]int, error) {
	if allDocs {
		targets := make([]int, n)
		for i := range targets {
			targets[i] = i
		}
		return targets, nil
	}
	if docIdx < 0 || docIdx >= n {
		return nil, usageErr(fmt.Errorf("document index %d out of range (%d documents)", docIdx, n))
	}
	return []int{docIdx}, nil
}

// templateDirectiveLine returns the 1-based line of the first unquoted "{{"
// in data, or 0 if there is none.
//
// A Go/Helm template directive is syntactically a YAML flow mapping nested
// inside another, so it parses. What happens next depends on the decode
// target, which is why it has to be detected here rather than left to
// yaml.v3: decoding into map[string]any trips the "map used as a map key"
// check and fails with a %#v dump that never mentions templating, while
// decoding into a *yaml.Node accepts it outright (see rejectMappingKeys).
//
// Quoted directives are excluded because `name: "{{ .Values.x }}"` is
// ordinary, valid YAML that round-trips correctly. The scan is per line and
// does not track quotes spanning lines — it only picks the line to report,
// never whether to fail, so a miss costs a line number, not correctness.
func templateDirectiveLine(data []byte) int {
	for i, line := range bytes.Split(data, []byte("\n")) {
		var quote byte
		for j := 0; j+1 < len(line); j++ {
			switch c := line[j]; {
			case quote != 0:
				if c == quote {
					quote = 0
				}
			case c == '\'' || c == '"':
				quote = c
			case c == '{' && line[j+1] == '{':
				return i + 1
			}
		}
	}
	return 0
}

// rejectMappingKeys fails a document that uses a mapping or sequence as a
// mapping key. yaml.v3 already rejects this when decoding into map[string]any
// — it is what makes `get` refuse a chart template — but decoding into a
// *yaml.Node applies no such check, so the editing commands used to accept a
// template, re-emit the directive in YAML's explicit-key form
// (`{? {include "x" .: ”} : ”}`), and write that over the original.
//
// Bringing the node path in line with the map path is the point: the two
// disagreeing is what made `set -i` destructive on input `get` refused.
func rejectMappingKeys(n *yaml.Node, data []byte) error {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; k.Kind == yaml.MappingNode || k.Kind == yaml.SequenceNode {
				return parseErr(fmt.Errorf("parsing YAML: %s", describeMappingKey(k.Line, data)))
			}
		}
	}
	for _, c := range n.Content {
		if err := rejectMappingKeys(c, data); err != nil {
			return err
		}
	}
	return nil
}

// describeMappingKey names the template as the cause when the offending line
// holds a directive, and otherwise states the general rule — a mapping key
// that is itself a collection is legal YAML but not something this tool edits.
func describeMappingKey(line int, data []byte) string {
	if tl := templateDirectiveLine(data); tl > 0 {
		return fmt.Sprintf("line %d: {{ ... }} template directive, not plain YAML; render the template first (e.g. `helm template`) or edit the chart's values.yaml", tl)
	}
	return fmt.Sprintf("line %d: a mapping or sequence used as a mapping key is not supported", line)
}

// explainParseError replaces yaml.v3's message when the real cause is a
// template directive. Its own text for that case is a %#v dump of a decoded
// Go value, and — uniquely among parse failures — carries no line number,
// because it fails at the map-key check rather than as a syntax error with a
// position. Gated on the message lacking a line: an ordinary syntax error in
// a file that also contains a directive keeps its own message and position.
func explainParseError(data []byte, err error) error {
	if _, hasLine := lineInMessageOf(err.Error()); hasLine {
		return parseErr(fmt.Errorf("parsing YAML: %w", err))
	}
	if tl := templateDirectiveLine(data); tl > 0 {
		return parseErr(fmt.Errorf("parsing YAML: line %d: {{ ... }} template directive, not plain YAML; render the template first (e.g. `helm template`) or query the chart's values.yaml", tl))
	}
	return parseErr(fmt.Errorf("parsing YAML: %w", err))
}
