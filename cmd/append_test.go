package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendToStdout(t *testing.T) {
	in := "ports:\n  - 80 # http\n  - 443\nname: web\n"
	got, err := execute(t, in, "append", ".ports", "9090")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "- 9090") || !strings.Contains(got, "# http") || !strings.Contains(got, "name: web") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAppendInPlace(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(f, []byte("tags:\n  - a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "", "append", "-i", ".tags", "b", f); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "tags:\n  - a\n  - b" {
		t.Fatalf("file after edit:\n%s", out)
	}
}

func TestAppendString(t *testing.T) {
	got, err := execute(t, "l: [1, 2]\n", "append", "-s", ".l", "007")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != `l: [1, 2, "007"]` {
		t.Fatalf("got %q", got)
	}
}

func TestAppendSecondDoc(t *testing.T) {
	in := "a: [1]\n---\nb: [2]\n"
	got, err := execute(t, in, "append", "--doc", "1", ".b", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "b: [2, 3]") || !strings.Contains(got, "a: [1]") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAppendErrorPaths(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"no documents", "", []string{"append", ".a", "1"}},
		{"append into mapping", "a: {b: 1}\n", []string{"append", ".a", "1"}},
		{"append into scalar", "a: 1\n", []string{"append", ".a", "1"}},
		{"missing path", "a: [1]\n", []string{"append", ".nope", "1"}},
		{"wildcard", "a: [1]\n", []string{"append", ".a.*", "1"}},
		{"in-place without file", "a: [1]\n", []string{"append", "-i", ".a", "1"}},
		{"file not found", "", []string{"append", ".a", "1", "/no/such/file.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := execute(t, tc.stdin, tc.args...); err == nil {
				t.Fatalf("expected error for %v", tc.args)
			}
		})
	}
}
