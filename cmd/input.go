package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// defaultMaxBytes caps how much input yaymlq will buffer. It guards against
// memory exhaustion from an oversized or hostile file/stream. A value of 0 on
// the --max-bytes flag disables the cap.
const defaultMaxBytes int64 = 64 << 20 // 64 MiB

// errInputTooLarge is returned when the input exceeds the configured cap.
var errInputTooLarge = errors.New("input too large")

// readCapped reads all of r into memory, refusing to buffer more than limit
// bytes. limit <= 0 means unlimited.
func readCapped(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	// Read one byte past the limit so we can tell "exactly at the cap" from
	// "over the cap".
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: exceeds %d bytes; raise --max-bytes (0 = unlimited) to override", errInputTooLarge, limit)
	}
	return data, nil
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
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		docs = append(docs, doc)
		if !all && want >= 0 && len(docs) > want {
			break
		}
	}
	return docs, nil
}
