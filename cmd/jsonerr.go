package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/query"
	"github.com/spf13/cobra"
)

// jsonError is the shape written to stderr for a command failure under
// -o json: kind lines up with exitCode's taxonomy so a caller can branch on
// it the same way it would branch on the exit code. line and path are
// best-effort — populated only when the underlying error is known to carry
// that information, omitted (never guessed) otherwise.
type jsonError struct {
	Error string `json:"error"`
	Kind  string `json:"kind"`
	Line  int    `json:"line,omitempty"`
	Path  string `json:"path,omitempty"`
	// Available lists the keys of the mapping a key lookup missed, capped
	// at --max-suggestions; AvailableTotal appears only when that cap cut
	// the list, so a consumer can tell "these are all the keys" from
	// "these are the first ten". Suggestion is the nearest key when the
	// miss looks like a typo. All three are omitted for a miss that has no
	// keys to offer.
	Available      []string `json:"available,omitempty"`
	AvailableTotal int      `json:"availableTotal,omitempty"`
	Suggestion     string   `json:"suggestion,omitempty"`
}

// kindFor names an exit code from codeFor for -o json error output.
func kindFor(code int) string {
	switch code {
	case 2:
		return "parse"
	case 3:
		return "usage"
	case 4:
		return "io"
	default:
		return "no-match"
	}
}

// lineInMessage best-effort extracts a 1-indexed line number out of an error
// message that names one — yaml.v3's own parse errors always do ("yaml: line
// 3: did not find expected ...").
var lineInMessage = regexp.MustCompile(`line (\d+)`)

func lineInMessageOf(msg string) (int, bool) {
	m := lineInMessage.FindStringSubmatch(msg)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// handleErr centralizes error reporting for a command that supports -o json.
// A silentExit error — a deliberate, message-less exit like -e/-q's "no
// match" — passes through unchanged so its silence contract holds no matter
// the output format. Everything else renders as a JSON object on stderr when
// format is "json"; otherwise it's left as-is for exitCode's plain
// "Error: ..." line.
func handleErr(c *cobra.Command, err error, format string, maxSuggestions int) error {
	if err == nil {
		return nil
	}
	var se silentExit
	if errors.As(err, &se) {
		return err
	}
	if format != "json" {
		return annotateNotFound(err, maxSuggestions)
	}
	return silentExit{code: writeJSONError(c.ErrOrStderr(), err, maxSuggestions)}
}

// writeJSONError writes err's JSON representation to w and returns the exit
// code it maps to (the same one exitCode would have used for err).
func writeJSONError(w io.Writer, err error, maxSuggestions int) int {
	code := codeFor(err)
	je := jsonError{Error: err.Error(), Kind: kindFor(code)}
	if code == 2 {
		if n, ok := lineInMessageOf(err.Error()); ok {
			je.Line = n
		}
	}

	var nfe *query.NotFoundError
	if errors.As(err, &nfe) {
		je.Path = path.Format(nfe.Path)
	}
	if shown, all, target, ok := notFoundKeys(err, maxSuggestions); ok && len(all) > 0 {
		je.Available = shown
		if len(all) > len(shown) {
			je.AvailableTotal = len(all)
		}
		if s, found := nearest(target, all); found {
			je.Suggestion = s
		}
	}

	data, mErr := json.Marshal(je)
	if mErr != nil {
		// je is all strings/ints — Marshal cannot fail in practice. Fall
		// back to the plain message rather than silently losing it.
		_, _ = fmt.Fprintln(w, "Error:", err)
		return code
	}
	_, _ = fmt.Fprintln(w, string(data))
	return code
}
