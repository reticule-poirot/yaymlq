package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestInPlaceSymlinkPrintsNote checks that editing a symlinked path with
// --in-place prints a note explaining the link is being replaced, not
// written through — see warnIfSymlink and #86.
func TestInPlaceSymlinkPrintsNote(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	link := filepath.Join(dir, "link.yaml")
	if err := os.WriteFile(target, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not available: %v", err) // unprivileged Windows
	}

	got, err := execute(t, "", "set", "-i", ".a", "2", link)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "note:") || !strings.Contains(got, link) || !strings.Contains(got, target) {
		t.Fatalf("expected a symlink note mentioning %q and %q, got: %q", link, target, got)
	}
}

// TestInPlaceRegularFileNoNote checks the note is silent for the common
// case: editing a plain file in place prints nothing extra.
func TestInPlaceRegularFileNoNote(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := execute(t, "", "set", "-i", ".a", "2", f)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "" {
		t.Fatalf("expected no output for a regular-file in-place edit, got: %q", got)
	}
}

// TestWriteFileAtomicMissingTargetErrors checks that writeFileAtomic fails
// outright when its target no longer exists, rather than inventing a 0644
// fallback permission that would ignore the process umask. Every real
// caller has just successfully opened the file for reading, so reaching
// writeFileAtomic with the file gone means it was removed or replaced
// concurrently — worth failing on, not silently working around. See #87.
func TestWriteFileAtomicMissingTargetErrors(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "gone.yaml") // deliberately never created

	if err := writeFileAtomic(f, []byte("a: 1\n")); err == nil {
		t.Fatal("expected an error when the target doesn't exist")
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("writeFileAtomic should not have created a file at the missing target's path")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover temp files, found: %v", entries)
	}
}

// TestWriteFileAtomicChmodIsRaceSafe reproduces the TOCTOU window a symlink
// attacker would use against a chmod-by-path implementation: while
// writeFileAtomic is working, a racer goroutine watches the directory for
// the temp file writeFileAtomic just created and, the instant it appears,
// replaces it with a symlink to a victim file elsewhere. A chmod-by-path
// would follow that symlink and widen the victim's permissions; chmod-by-fd
// (what writeFileAtomic now does) cannot be redirected by a name swap, so
// the victim's mode must be untouched no matter how the race lands.
func TestWriteFileAtomicChmodIsRaceSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink-swap race doesn't apply on Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(target, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	prefix := "." + filepath.Base(target) + ".yaymlq-"
	raced := make(chan bool, 1)
	stop := make(chan struct{})
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-stop:
				raced <- false
				return
			default:
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), prefix) {
					tmpPath := filepath.Join(dir, e.Name())
					if os.Remove(tmpPath) == nil && os.Symlink(victim, tmpPath) == nil {
						raced <- true
						return
					}
				}
			}
		}
		raced <- false
	}()

	_, execErr := execute(t, "", "set", "-i", ".a", "2", target)
	close(stop)
	won := <-raced

	if execErr != nil && !won {
		t.Fatalf("execute: %v", execErr)
	}
	if !won {
		t.Skip("racer never won the window against writeFileAtomic; can't exercise the race on this run")
	}

	info, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("victim mode = %o, want 600 (chmod followed the swapped symlink)", got)
	}
	content, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "secret" {
		t.Fatalf("victim content changed: %q", content)
	}
}
