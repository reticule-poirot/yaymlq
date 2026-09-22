package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// flagConflicts are invocations that must be rejected as usage errors,
// expressed as "applies to any command offering every flag in needs" rather
// than as a list of command names. Driving the table off the live cobra tree
// is the point: a command that wires these flags up later is covered without
// anyone remembering to extend this file.
//
// Both bugs this guards against were of one shape — a validation that exists
// but doesn't fire. #123: the --raw alias assigned the output format before
// --print0's guard compared against it, so passing --raw silently disabled a
// check that worked without it — the alias was removed in #138, and the row
// covering it went with it, but --print0's own case below is the same shape.
// #118: --diff-format was accepted without --diff and then
// ignored, so `set -i --diff-format json` wrote the file instead of previewing
// it. Per-command tests missed both because each flag was covered alone.
var flagConflicts = []struct {
	name  string
	needs []string
	args  []string
}{
	{"print0 with a conflicting -o", []string{"print0", "output"}, []string{"-0", "-o", "json"}},
	{"diff-format without --diff", []string{"diff-format"}, []string{"--diff-format", "json"}},
	{"unknown diff-format value", []string{"diff-format", "diff"}, []string{"--diff", "--diff-format", "xml"}},
	{"diff with show-diff", []string{"diff", "show-diff"}, []string{"--diff", "--show-diff"}},
	{"unknown -o value", []string{"output"}, []string{"-o", "xml"}},
}

// minimalArgs returns the smallest positional arguments that carry a command
// as far as its flag validation, so a conflict case can be appended to them.
// This is the one hand-maintained piece here, and
// TestMinimalArgsCoversEveryCommand fails when a new command has no entry —
// same tripwire argRanges gets in schema_test.go.
func minimalArgs(t *testing.T, name string) ([]string, bool) {
	t.Helper()
	switch name {
	case "get", "delete":
		return []string{".a"}, true
	case "keys", "len", "type":
		return []string{"."}, true
	case "set", "append":
		return []string{".a", "9"}, true
	case "rename":
		return []string{".a", "z"}, true
	case "apply":
		// A real file, not "-": the document under edit already occupies
		// stdin in these tests.
		script := filepath.Join(t.TempDir(), "edits.txt")
		if err := os.WriteFile(script, []byte("set .a = 9\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{"-f", script}, true
	case "validate", "schema":
		return nil, true
	}
	return nil, false
}

// conflictCommands returns every command the manifest covers, root included.
func conflictCommands() []*cobra.Command {
	root := NewRootCommand()
	cmds := []*cobra.Command{root}
	for _, c := range root.Commands() {
		if excludedCommands[c.Name()] {
			continue
		}
		cmds = append(cmds, c)
	}
	return cmds
}

func offersAll(c *cobra.Command, names []string) bool {
	for _, n := range names {
		if c.Flags().Lookup(n) == nil {
			return false
		}
	}
	return true
}

// invocation prefixes a subcommand's name; root's own verb takes none.
func invocation(name string, parts ...[]string) []string {
	args := []string{}
	if name != "get" {
		args = append(args, name)
	}
	for _, p := range parts {
		args = append(args, p...)
	}
	return args
}

func TestFlagConflictsAreUsageErrors(t *testing.T) {
	for _, c := range conflictCommands() {
		name := commandName(c)
		for _, fc := range flagConflicts {
			if !offersAll(c, fc.needs) {
				continue
			}
			t.Run(name+"/"+fc.name, func(t *testing.T) {
				base, ok := minimalArgs(t, name)
				if !ok {
					t.Skip("no minimalArgs entry; TestMinimalArgsCoversEveryCommand reports this")
				}
				args := invocation(name, fc.args, base)
				if got := exitCode(mustErr(execute(t, "a: 1\n", args...)), io.Discard); got != 3 {
					t.Fatalf("%v: want exit 3 (usage), got %d", args, got)
				}
			})
		}
	}
}

// TestRejectedFlagConflictsNeverWrite is the assertion that matters most: an
// invocation the CLI rejects must leave the target file byte-identical. #118's
// exit code was never the harm — the silent in-place write was — and this
// catches "the validation is missing" and "the validation fires after the
// write" as one property, which an exit-code check alone does not.
func TestRejectedFlagConflictsNeverWrite(t *testing.T) {
	const original = "a: 1\nb: 2\n"
	for _, c := range conflictCommands() {
		name := commandName(c)
		if c.Flags().Lookup("in-place") == nil {
			continue
		}
		for _, fc := range flagConflicts {
			if !offersAll(c, fc.needs) {
				continue
			}
			t.Run(name+"/"+fc.name, func(t *testing.T) {
				base, ok := minimalArgs(t, name)
				if !ok {
					t.Skip("no minimalArgs entry; TestMinimalArgsCoversEveryCommand reports this")
				}
				doc := filepath.Join(t.TempDir(), "doc.yaml")
				if err := os.WriteFile(doc, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				args := invocation(name, []string{"-i"}, fc.args, base, []string{doc})
				if _, err := execute(t, "", args...); err == nil {
					t.Fatalf("%v: want a usage error, got nil", args)
				}
				got, err := os.ReadFile(doc)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != original {
					t.Fatalf("%v: rejected invocation wrote the file: %q", args, got)
				}
			})
		}
	}
}

// TestMinimalArgsCoversEveryCommand catches a command added later with no
// minimalArgs entry, which would otherwise silently skip every conflict case
// for that command instead of failing.
func TestMinimalArgsCoversEveryCommand(t *testing.T) {
	for _, c := range conflictCommands() {
		name := commandName(c)
		if _, ok := minimalArgs(t, name); !ok {
			t.Errorf("minimalArgs has no entry for command %q", name)
		}
	}
}

// TestEveryFlagConflictMatchesSomeCommand catches the failure mode this table
// exists to prevent, turned on itself: renaming a flag would leave its case
// matching nothing, reducing that row to zero assertions while the suite
// stayed green.
func TestEveryFlagConflictMatchesSomeCommand(t *testing.T) {
	cmds := conflictCommands()
	for _, fc := range flagConflicts {
		matched := false
		for _, c := range cmds {
			if offersAll(c, fc.needs) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("flagConflicts case %q matches no command: flags %v", fc.name, fc.needs)
		}
	}
}

func mustErr(_ string, err error) error { return err }
