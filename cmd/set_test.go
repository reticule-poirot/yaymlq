package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetToStdout(t *testing.T) {
	in := "svc:\n  image: old:1 # pinned\n  port: 80\n"
	got, err := execute(t, in, "set", ".svc.image", "new:2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "image: new:2") || !strings.Contains(got, "# pinned") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetInPlace(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(f, []byte("a:\n  b: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "", "set", "-i", ".a.b", "2", f); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "a:\n  b: 2" {
		t.Fatalf("file after edit:\n%s", out)
	}
}

func TestSetInPlacePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not represented on Windows")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "", "set", "-i", ".a", "2", f); err != nil {
		t.Fatalf("execute: %v", err)
	}

	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestSetInPlaceReplacesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	link := filepath.Join(dir, "link.yaml")
	if err := os.WriteFile(target, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not available: %v", err) // unprivileged Windows
	}

	if _, err := execute(t, "", "set", "-i", ".a", "2", link); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// The symlink target is left untouched...
	orig, _ := os.ReadFile(target)
	if strings.TrimSpace(string(orig)) != "a: 1" {
		t.Fatalf("symlink target was modified: %s", orig)
	}
	// ...and the link is now a regular file with the new content.
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("link is still a symlink; write went through it")
	}
	got, _ := os.ReadFile(link)
	if strings.TrimSpace(string(got)) != "a: 2" {
		t.Fatalf("link content = %q", got)
	}
}

func TestSetInPlaceNeedsFile(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "set", "-i", ".a", "2"); err == nil {
		t.Fatal("expected error: --in-place without a file")
	}
}

func TestSetString(t *testing.T) {
	got, err := execute(t, "x: 0\n", "set", "-s", ".x", "007")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != `x: "007"` {
		t.Fatalf("got %q", got)
	}
}

func TestSetWildcardRejected(t *testing.T) {
	if _, err := execute(t, "a: {b: 1}\n", "set", ".a.*", "2"); err == nil {
		t.Fatal("expected wildcard to be rejected by set")
	}
}

func TestSetErrorPaths(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"no documents", "", []string{"set", ".a", "1"}},
		{"doc index out of range", "a: 1\n", []string{"set", "--doc", "5", ".a", "1"}},
		{"bad path", "a: 1\n", []string{"set", "a[", "1"}},
		{"type mismatch", "a: 1\n", []string{"set", ".a.b", "1"}},
		{"file not found", "", []string{"set", ".a", "1", "/no/such/file.yaml"}},
		{"in-place into missing dir", "", []string{"set", "-i", ".a", "1", "/no/such/dir/f.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := execute(t, tc.stdin, tc.args...); err == nil {
				t.Fatalf("expected error for %v", tc.args)
			}
		})
	}
}

func TestSetSecondDoc(t *testing.T) {
	in := "name: one\n---\nname: two\n"
	got, err := execute(t, in, "set", "--doc", "1", ".name", "TWO")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "name: one") || !strings.Contains(got, "name: TWO") {
		t.Fatalf("got:\n%s", got)
	}
}
