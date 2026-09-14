package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOK(t *testing.T) {
	out, err := execute(t, "a: 1\nb: [1, 2]\n", "validate")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "" {
		t.Fatalf("want no output on success, got %q", out)
	}
}

func TestValidateMultiDocOK(t *testing.T) {
	if _, err := execute(t, "a: 1\n---\nb: 2\n", "validate"); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestValidateBadYAMLFromStdin(t *testing.T) {
	out, err := execute(t, "a: [1, 2\n", "validate")
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, "stdin:") || !strings.Contains(out, "line") {
		t.Fatalf("want a labeled line/col error, got %q", out)
	}
}

func TestValidateFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "", "validate", f); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestValidateMultipleFilesOneBadKeepsCheckingAll(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(good, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("a: [1, 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "", "validate", good, bad)
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, filepath.Base(bad)) {
		t.Fatalf("want the bad file named in output, got %q", out)
	}
	if strings.Contains(out, filepath.Base(good)) {
		t.Fatalf("good file shouldn't be mentioned, got %q", out)
	}
}

func TestValidateMissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")

	out, err := execute(t, "", "validate", missing)
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if !strings.Contains(out, filepath.Base(missing)) {
		t.Fatalf("want the missing file named in output, got %q", out)
	}
}

func TestValidateMaxBytes(t *testing.T) {
	big := "data: " + strings.Repeat("x", 4096) + "\n"
	if _, err := execute(t, big, "validate", "--max-bytes", "128"); err == nil {
		t.Fatal("want error when input exceeds --max-bytes")
	}
}
