package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

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
