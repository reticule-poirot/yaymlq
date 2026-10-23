package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shape #164 opens with: `tset` is a typo for `test`, and `set` creates
// it rather than failing, so nothing marks the mistake.
const typoSrc = "jobs:\n  test: {x: 1}\n"

// TestSetMustExistRejectsATypo guards #164. `set` creating what it can't find
// is the right default, but it leaves a typo'd path indistinguishable from an
// intentional new key — exit 0, no output. --must-exist is the way to say
// "this path already exists", and the error it produces is the same
// *ymledit.KeyError the other verbs give, so the recovery facts from #163
// arrive with it.
func TestSetMustExistRejectsATypo(t *testing.T) {
	out, err := execute(t, typoSrc, "set", "--must-exist", ".jobs.tset", "1")
	if err == nil {
		t.Fatalf("want an error for a path that doesn't exist, got output:\n%s", out)
	}
	if code := codeFor(err); code != 1 {
		t.Errorf("want exit 1, got %d", code)
	}
	msg := annotateNotFound(err, defaultMaxSuggestions).Error()
	if !strings.Contains(msg, "no such key") {
		t.Errorf("want a \"no such key\" message, got %q", msg)
	}
	// Inherited from #163 rather than written again here, which is the
	// reason for reusing KeyError instead of a new error type.
	if !strings.Contains(msg, `did you mean "test"`) {
		t.Errorf("want the suggestion carried over, got %q", msg)
	}
}

// TestSetMustExistRejectsAMissingIntermediate: inventing a parent mapping is
// the more obvious mistake of the two, and the decision on #164 was that one
// rule covers both rather than a flag that forbids only this.
func TestSetMustExistRejectsAMissingIntermediate(t *testing.T) {
	out, err := execute(t, typoSrc, "set", "--must-exist", ".jobs.tset.deep", "1")
	if err == nil {
		t.Fatalf("want an error for a missing intermediate, got:\n%s", out)
	}
	// Exit 1, not 3: a path that doesn't resolve, not a bad command line.
	// Asserted so this can't pass merely because the flag is unknown.
	if code := codeFor(err); code != 1 {
		t.Fatalf("want exit 1, got %d (%v)", code, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "no such key") {
		t.Errorf("want a \"no such key\" message, got %q", msg)
	}
}

// TestSetMustExistAcceptsAnExistingPath: the guard has to be invisible when
// the path resolves, which is most of what it does.
func TestSetMustExistAcceptsAnExistingPath(t *testing.T) {
	got, err := execute(t, typoSrc, "set", "--must-exist", ".jobs.test.x", "9")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "x: 9") {
		t.Fatalf("want the value replaced, got:\n%s", got)
	}
}

// TestSetWithoutMustExistStillCreates pins the default #164 is explicit about
// keeping.
func TestSetWithoutMustExistStillCreates(t *testing.T) {
	got, err := execute(t, typoSrc, "set", ".jobs.tset", "1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "tset: 1") {
		t.Fatalf("want the key created by default, got:\n%s", got)
	}
}

// TestSetMustExistWritesNothingInPlace: the guard fires before the write, so
// a refused edit leaves the file byte-identical. Same property
// cmd/flagconflict_test.go asserts for a rejected flag combination — a
// validation that fires after the write is worse than none.
func TestSetMustExistWritesNothingInPlace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(f, []byte(typoSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execute(t, "", "set", "-i", "--must-exist", ".jobs.tset", "1", f)
	if err == nil {
		t.Fatal("want an error")
	}
	if code := codeFor(err); code != 1 {
		t.Fatalf("want exit 1 from the guard, got %d (%v)", code, err)
	}
	data, readErr := os.ReadFile(f)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != typoSrc {
		t.Errorf("file changed by a refused edit:\nwant:\n%q\ngot:\n%q", typoSrc, data)
	}
}

// TestApplyMustExistRejectsATypo: apply runs set ops, so it inherits the
// flag. Per-invocation rather than per-op, per the decision on #164 — a
// per-op spelling can be added later without breaking this one.
func TestApplyMustExistRejectsATypo(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(f, []byte(typoSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, dir, "set .jobs.test.x = 2\nset .jobs.tset = 1\n")

	_, err := execute(t, "", "apply", "-i", "--must-exist", "-f", script, f)
	if err == nil {
		t.Fatal("want an error for the second op")
	}
	if code := codeFor(err); code != 1 {
		t.Fatalf("want exit 1 from the guard, got %d (%v)", code, err)
	}
	// Named by line, like every other apply op failure.
	if msg := err.Error(); !strings.Contains(msg, "line 2") {
		t.Errorf("want the failing line named, got %q", msg)
	}
	// The first op succeeded in memory; the batch still writes nothing,
	// which is apply's existing all-or-nothing contract.
	data, readErr := os.ReadFile(f)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != typoSrc {
		t.Errorf("a failed batch wrote to the file:\nwant:\n%q\ngot:\n%q", typoSrc, data)
	}
}

func TestApplyWithoutMustExistStillCreates(t *testing.T) {
	script := writeScript(t, t.TempDir(), "set .jobs.tset = 1\n")
	got, err := execute(t, typoSrc, "apply", "-f", script)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "tset: 1") {
		t.Fatalf("want the key created by default, got:\n%s", got)
	}
}

// TestMustExistIsNotOnTheVerbsThatAlreadyRequireIt: delete/rename/append fail
// on a missing key already, so the flag would be a no-op there — and an
// accepted no-op flag reads as a promise the command doesn't make.
func TestMustExistIsNotOnTheVerbsThatAlreadyRequireIt(t *testing.T) {
	for _, verb := range []string{"delete", "rename", "append"} {
		t.Run(verb, func(t *testing.T) {
			args := []string{verb, "--must-exist", ".jobs.test"}
			if verb != "delete" {
				args = append(args, "z")
			}
			_, err := execute(t, typoSrc, args...)
			if err == nil {
				t.Fatal("want --must-exist rejected as an unknown flag")
			}
			if code := codeFor(err); code != 3 {
				t.Errorf("want exit 3 for an unknown flag, got %d", code)
			}
		})
	}
}
