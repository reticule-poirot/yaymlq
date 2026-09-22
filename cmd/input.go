package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// defaultMaxBytes caps how much input yaymlq will buffer. It guards against
// memory exhaustion from an oversized or hostile file/stream. A value of 0 on
// the --max-bytes flag disables the cap.
const defaultMaxBytes int64 = 64 << 20 // 64 MiB

// errInputTooLarge is returned when the input exceeds the configured cap.
var errInputTooLarge = errors.New("input too large")

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
