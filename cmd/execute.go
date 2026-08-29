package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// silentExit makes the process exit with a specific code without printing an
// error message — used for the -e/--exit-status "no match" case.
type silentExit struct{ code int }

func (s silentExit) Error() string { return fmt.Sprintf("exit status %d", s.code) }

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	return exitCode(NewRootCommand().Execute(), os.Stderr)
}

// exitCode maps a command error to a process exit code, printing a message to
// errOut for anything that isn't a silent exit.
func exitCode(err error, errOut io.Writer) int {
	if err == nil {
		return 0
	}
	var se silentExit
	if errors.As(err, &se) {
		return se.code
	}
	_, _ = fmt.Fprintln(errOut, "Error:", err)
	return 1
}
