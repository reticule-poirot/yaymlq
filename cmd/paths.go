package cmd

import (
	"encoding/json"
	"fmt"
	"io"
)

// pathJSON is one line of `--paths -o json`: a resolved path together with
// the document it is in.
//
// The document index is what makes this form worth having. A path alone is
// per-document, so a listing over a stream repeats the same path for each
// document that matches and nothing says which is which — a script built
// from it edits the first document over and over (#159). Doc is exactly the
// value to pass to --doc.
//
// One compact object per line, like jsonerr.go's error object rather than
// render.go's indented value: this is a stream of events to read a line at a
// time, not a document to look at.
type pathJSON struct {
	Doc  int    `json:"doc"`
	Path string `json:"path"`
}

// emitPathsJSON writes one pathJSON per resolved path. results holds the
// rendered path strings queryResults produced for document doc.
func emitPathsJSON(w io.Writer, doc int, results []any) error {
	for _, r := range results {
		p, ok := r.(string)
		if !ok {
			// queryResults only ever puts path.Format output in here under
			// --paths; anything else means the two have drifted apart.
			return fmt.Errorf("internal: --paths produced a %T, not a path string", r)
		}
		data, err := json.Marshal(pathJSON{Doc: doc, Path: p})
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, string(data)); err != nil {
			return err
		}
	}
	return nil
}
