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
			return nil, parseErr(fmt.Errorf("parsing YAML: %w", err))
		}
		docs = append(docs, doc)
		if !all && want >= 0 && len(docs) > want {
			break
		}
	}
	return docs, nil
}
