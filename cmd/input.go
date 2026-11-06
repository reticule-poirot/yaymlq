package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
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

// bindReadFlags registers the flags every read-only verb shares (get, keys,
// len, type) — the read-side counterpart to bindEditFlags: one registration
// point, so none of them can end up offering a different subset or wording.
// --doc and --all-docs carry the same promise for all four, since none of
// them creates anything; that's what tells them apart from the edit verbs'
// bindEditFlags, which takes allDocsUsage as a parameter instead.
//
// defaultUsage is the one value the callers don't share verbatim: get's
// --default prints the value as given, while keys/len/type's skips the
// transform (inspecting a default wouldn't mean anything for len/type, and
// would just duplicate keys's own work for keys).
//
// --paths has no read-only counterpart to share and is registered by get on
// its own.
func bindReadFlags(cmd *cobra.Command, opts *readOpts, defaultUsage string) {
	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", opts.output, "output format: yaml|json|raw")
	f.BoolVarP(&opts.print0, "print0", "0", false, "NUL-separate multiple results instead of newline, for xargs -0; implies -o raw")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to query in a multi-doc stream")
	f.BoolVar(&opts.allDocs, "all-docs", false, "query every document in the stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited (bounds input size, not peak memory)")
	f.IntVar(&opts.maxSugg, "max-suggestions", opts.maxSugg, "max keys a \"path not found\" error lists; 0 = all of them")
	f.StringVar(&opts.defValue, "default", "", defaultUsage)
	f.BoolVarP(&opts.exitStatus, "exit-status", "e", false, "exit 1 (no output) when the path has no match")
	f.BoolVarP(&opts.quiet, "quiet", "q", false, "no output; exit 0 on a match, 1 otherwise (mirrors grep -q)")
	f.BoolVarP(&opts.asString, "string", "s", false, "treat --default as a string, not parsed YAML")
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

// decodeDocs decodes the YAML document stream in data, returning each
// document's decoded value alongside the *yaml.Node it came from — the
// node a caller needs to tell what, if anything, an unknown YAML tag cost
// the decode (see tagwarn.go).
//
// When all is false, decoding stops as soon as document index `want` has been
// read, so a huge trailing stream is never parsed just to reach an early
// document.
func decodeDocs(data []byte, want int, all bool) ([]any, []*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	var roots []*yaml.Node
	var lines []string
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, explainParseError(data, err)
		}
		// Before n.Decode(&doc): decoding a mapping or sequence used as a
		// mapping key into `any` fails with "cannot use '[]interface {}{...}'
		// as a map key" — accurate, but Go syntax a caller never typed. rejectMappingKeys gives the same refusal
		// decodeNodes already does on the edit side (#355), so both halves
		// of the CLI describe the same input the same way.
		if err := rejectMappingKeys(&n, data); err != nil {
			return nil, nil, err
		}
		if err := rejectSwallowedMarker(&n, data, &lines); err != nil {
			return nil, nil, err
		}
		if err := rejectNonConforming(&n, data, &lines); err != nil {
			return nil, nil, err
		}
		retagForRead(&n)
		// Through the node so the keys can be checked; Node.Decode runs
		// yaml.v4's own checks, so its errors still come first.
		// yaml.v4 itself rejects alias-expansion bombs with
		// "excessive aliasing"; surface that and any other parse error.
		var doc any
		if err := n.Decode(&doc); err != nil {
			return nil, nil, explainParseError(data, err)
		}
		if err := rejectEqualKeys(&n); err != nil {
			return nil, nil, err
		}
		// After rejectEqualKeys, not before: that check's identity is the
		// node's own tag and decoded value, and two keys that decode to the
		// same instant (`2024-06-01` beside `2024-6-1`) must still collide
		// there the way an exact repeat does. Retagging first would make
		// them two distinct strings and silently accept what get already
		// refuses.
		if retagTimestamps(&n) {
			doc = nil
			if err := n.Decode(&doc); err != nil {
				return nil, nil, explainParseError(data, err)
			}
		}
		docs = append(docs, doc)
		// n is the Decoder's DocumentNode wrapper, not the root value
		// itself — Content[0] is what a path actually walks (lookupNode,
		// tagwarn.go), and is nil for a document with nothing in it (a
		// stream holding only "---").
		var root *yaml.Node
		if len(n.Content) == 1 {
			root = n.Content[0]
		}
		roots = append(roots, root)
		if !all && want >= 0 && len(docs) > want {
			break
		}
	}
	return docs, roots, nil
}

// rejectEqualKeys refuses a mapping holding two keys that decode to the same
// value. yaml.v4 refuses a key written twice the same way, but compares only
// the text, so `7` beside `0o7`, `true` beside `True` or `null` beside `~`
// passed and decoded as one key, the later value silently replacing the
// earlier (found on #300). YAML 1.2 makes two keys with the same tag and
// canonical value duplicates, which is what is compared here: the tag and the
// decoded value, with Go's ==, so NaN matches nothing and int 1 never matches
// float 1.0. An alias key counts as the key it names. A merge key is skipped:
// a key written beside `<<` overrides the merged one by design.
func rejectEqualKeys(n *yaml.Node) error {
	type identity struct {
		tag   string
		value any
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[identity]*yaml.Node, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			written, k := n.Content[i], n.Content[i]
			if k.Kind == yaml.AliasNode && k.Alias != nil {
				k = k.Alias
			}
			if k.Kind != yaml.ScalarNode || k.ShortTag() == "!!merge" {
				continue
			}
			id := identity{tag: k.ShortTag(), value: k.Value}
			switch {
			case isNegativeZero(k):
				id = identity{tag: "!!int", value: 0}
			case id.tag != "!!str":
				var v any
				if k.Decode(&v) != nil {
					continue
				}
				id.value = v
			}
			if prev, dup := seen[id]; dup {
				return parseErr(fmt.Errorf("parsing YAML: line %d: mapping key %q already defined at line %d as %q", written.Line, k.Value, prev.Line, prevText(prev)))
			}
			seen[id] = written
		}
	}
	for _, c := range n.Content {
		if err := rejectEqualKeys(c); err != nil {
			return err
		}
	}
	return nil
}

// retagTimestamps rewrites every scalar yaml.v4 would decode to time.Time
// (tag !!timestamp) to !!str, so decoding n gives back the source's own
// text instead of an expanded, re-formatted instant. time.Time is the only
// non-JSON-primitive concrete type yaml.v4 can hand back into an `any`
// (resolve.go's allowedTimestampFormats), and several distinct source
// spellings — `2024-06-01`, `2024-06-01 00:00:00`, `2024-6-1` — decode to
// the identical time.Time, so there is no recovering the original spelling
// after the fact; this has to happen before that decode, not after it.
//
// Applies to a mapping key as well as a value: an unretagged date-like key
// decodes into a time.Time key, which forces the whole mapping into
// map[any]any and makes keys/--paths print it as its Go-formatted instant —
// a spelling set on that path can't resolve, landing a second, bogus key
// beside the real one instead of editing it (found alongside #321).
//
// Reports whether it changed anything, so a caller already holding a
// decoded value can skip redecoding a document with no timestamp in it.
func retagTimestamps(n *yaml.Node) bool {
	changed := false
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!timestamp" {
		n.Tag = "!!str"
		changed = true
	}
	for _, c := range n.Content {
		if retagTimestamps(c) {
			changed = true
		}
	}
	return changed
}

// prevText is the spelling of a key node as rejectEqualKeys reports it: an
// alias key by the key it names.
func prevText(k *yaml.Node) string {
	if k.Kind == yaml.AliasNode && k.Alias != nil {
		return k.Alias.Value
	}
	return k.Value
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
		if err := refuseInteractiveStdin(c); err != nil {
			return nil, nil, err
		}
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
func readDocs(input io.Reader, o readOpts) ([]any, []*yaml.Node, []int, error) {
	data, err := readCapped(input, o.maxBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	docs, roots, err := decodeDocs(data, o.docIdx, o.allDocs)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(docs) == 0 {
		// Unclassified on purpose, so it exits 1 rather than 2: the stream
		// parsed fine, it just holds nothing to query or edit (#209).
		return nil, nil, nil, fmt.Errorf("no YAML documents on input")
	}
	targets, err := docTargets(o.docIdx, o.allDocs, len(docs))
	if err != nil {
		return nil, nil, nil, err
	}
	return docs, roots, targets, nil
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
// yaml.v4: decoding into map[string]any trips the "as a map key" check and
// fails with a %#v dump that never mentions templating, while
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
// mapping key, directly or through an alias. yaml.v4 already rejects this when
// decoding into map[string]any
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
			k := n.Content[i]
			target := k
			if k.Kind == yaml.AliasNode && k.Alias != nil {
				target = k.Alias
			}
			if target.Kind == yaml.MappingNode || target.Kind == yaml.SequenceNode {
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

// yamlVersionDirective finds a "%YAML" directive naming a version other
// than "1.1" — the only one yaml.v4 reads — among the lines before the
// first document start. The YAML spec puts a directive at column 0, one
// per document, before its "---"; this only has to look as far as that
// marker, or the first non-directive, non-comment, non-blank line, since a
// directive can't appear after either.
func yamlVersionDirective(data []byte) (line int, version string, ok bool) {
	for i, l := range bytes.Split(data, []byte("\n")) {
		l = bytes.TrimRight(l, "\r")
		switch {
		case bytes.HasPrefix(l, []byte("%YAML")):
			v := bytes.TrimSpace(l[len("%YAML"):])
			if len(v) > 0 && string(v) != "1.1" {
				return i + 1, string(v), true
			}
			return 0, "", false
		case bytes.HasPrefix(l, []byte("%")), bytes.HasPrefix(l, []byte("#")), len(bytes.TrimSpace(l)) == 0:
			continue // another directive, a comment, or a blank line
		default:
			return 0, "", false // content, or the "---" marker: too late for a directive
		}
	}
	return 0, "", false
}

// explainParseError replaces yaml.v4's message when the real cause is a
// template directive or an unsupported "%YAML" version: yaml.v4's own text for
// the version ("found incompatible YAML document") doesn't say which directive
// it means, and a template fails at the map-key check or as a syntax error on
// the directive's own line. Gated on exactly those, because yaml.v4 puts a line
// on every error: an ordinary syntax error elsewhere in a file that also
// contains either keeps its own message and position.
func explainParseError(data []byte, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "found incompatible YAML document") {
		if line, version, ok := yamlVersionDirective(data); ok {
			return parseErr(fmt.Errorf("parsing YAML: line %d: %%YAML %s directive; yaymlq (via yaml.v4) only reads YAML 1.1 streams — remove the directive or change it to \"%%YAML 1.1\"", line, version))
		}
	}
	if tl := templateDirectiveLine(data); tl > 0 {
		line, _ := lineInMessageOf(msg)
		if line == tl || strings.Contains(msg, "as a map key") {
			return parseErr(fmt.Errorf("parsing YAML: line %d: {{ ... }} template directive, not plain YAML; render the template first (e.g. `helm template`) or query the chart's values.yaml", tl))
		}
	}
	return parseErr(fmt.Errorf("parsing YAML: %w", err))
}
