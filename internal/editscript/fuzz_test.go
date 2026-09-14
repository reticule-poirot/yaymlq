package editscript_test

import (
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/editscript"
)

// FuzzParse checks that Parse never panics on arbitrary input, and that
// every Op it does return names a line number that was actually present in
// the input.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"# just a comment\n",
		"set .a = 1\n",
		"append .a = [1, 2]\n",
		"delete .a\n",
		"rename .a = b\n",
		"set .a=1\nset .b   =   2\n",
		`set .a = "x=y=z"`,
		"bogus\n",
		"set\n",
		"set .a\n",
		"set  = 1\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, script string) {
		ops, err := editscript.Parse(strings.NewReader(script))
		if err != nil {
			return
		}
		lines := len(strings.Split(script, "\n"))
		for _, op := range ops {
			if op.Line < 1 || op.Line > lines {
				t.Fatalf("op %+v has an out-of-range line number (script has %d lines)", op, lines)
			}
		}
	})
}
