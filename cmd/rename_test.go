package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameToStdout(t *testing.T) {
	in := "svc:\n  image: nginx # pinned\n  port: 80\n"
	got, err := execute(t, in, "rename", ".svc.port", "containerPort")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "port:") {
		t.Fatalf("old key still present:\n%s", got)
	}
	if !strings.Contains(got, "containerPort: 80") || !strings.Contains(got, "# pinned") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenameInPlace(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(f, []byte("a:\n  b: 1\n  c: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "", "rename", "-i", ".a.b", "renamed", f); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "a:\n  renamed: 1\n  c: 2" {
		t.Fatalf("file after edit:\n%s", out)
	}
}

func TestRenameInPlaceNeedsFile(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "rename", "-i", ".a", "b"); err == nil {
		t.Fatal("expected error: --in-place without a file")
	}
}

func TestRenameSecondDoc(t *testing.T) {
	in := "name: one\n---\nname: two\n"
	got, err := execute(t, in, "rename", "--doc", "1", ".name", "title")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "name: one") || !strings.Contains(got, "title: two") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenameCollisionExits1(t *testing.T) {
	out, err := execute(t, "a: {b: 1, c: 2}\n", "rename", ".a.b", "c")
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
	if err == nil {
		t.Fatal("expected an error for a colliding rename")
	}
}

func TestRenameErrorPaths(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"no documents", "", []string{"rename", ".a", "b"}},
		{"doc index out of range", "a: 1\n", []string{"rename", "--doc", "5", ".a", "b"}},
		{"bad path", "a: 1\n", []string{"rename", "a[", "b"}},
		{"wildcard", "a: {b: 1}\n", []string{"rename", ".a.*", "c"}},
		{"list index", "a: [1, 2]\n", []string{"rename", "a[0]", "b"}},
		{"missing key", "a: 1\n", []string{"rename", ".b", "c"}},
		{"file not found", "", []string{"rename", ".a", "b", "/no/such/file.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := execute(t, tc.stdin, tc.args...); err == nil {
				t.Fatalf("expected error for %v", tc.args)
			}
		})
	}
}
