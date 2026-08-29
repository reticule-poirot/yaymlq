package cmd

import (
	"errors"
	"fmt"
	"os"
)

// silentExit makes the process exit with a specific code without printing an
// error message — used for the -e/--exit-status "no match" case.
type silentExit struct{ code int }

func (s silentExit) Error() string { return fmt.Sprintf("exit status %d", s.code) }

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	err := NewRootCommand().Execute()
	if err == nil {
		return 0
	}

	var se silentExit
	if errors.As(err, &se) {
		return se.code
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	return 1
}
