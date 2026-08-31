package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteToStdout(t *testing.T) {
	in := "svc:\n  image: old:1 # pinned\n  port: 80\n"
	got, err := execute(t, in, "delete", ".svc.port")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "port:") {
		t.Fatalf("port not removed:\n%s", got)
	}
	if !strings.Contains(got, "image: old:1") || !strings.Contains(got, "# pinned") {
		t.Fatalf("sibling key/comment lost:\n%s", got)
	}
}

func TestDeleteAlias(t *testing.T) {
	got, err := execute(t, "a:\n  b: 1\n  c: 2\n", "rm", ".a.b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(got, "b: 1") || !strings.Contains(got, "c: 2") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestDeleteInPlace(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(f, []byte("a:\n  b: 1\n  c: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "", "delete", "-i", ".a.b", f); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "a:\n  c: 2" {
		t.Fatalf("file after edit:\n%s", out)
	}
}

func TestDeleteInPlaceNeedsFile(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "delete", "-i", ".a"); err == nil {
		t.Fatal("expected error: --in-place without a file")
	}
}

func TestDeleteSecondDoc(t *testing.T) {
	in := "name: one\nkeep: yes\n---\nname: two\n"
	got, err := execute(t, in, "delete", "--doc", "1", ".name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "name: one") || strings.Contains(got, "name: two") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestDeleteMissingPathExits1(t *testing.T) {
	out, err := execute(t, "a: 1\n", "delete", ".nope")
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
	var se silentExit
	if errors.As(err, &se) {
		t.Fatalf("want a plain error, not silentExit, got %v", err)
	}
	if err == nil {
		t.Fatal("expected an error for a missing path")
	}
}

func TestDeleteErrorPaths(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"no documents", "", []string{"delete", ".a"}},
		{"doc index out of range", "a: 1\n", []string{"delete", "--doc", "5", ".a"}},
		{"bad path", "a: 1\n", []string{"delete", "a["}},
		{"wildcard", "a: {b: 1}\n", []string{"delete", ".a.*"}},
		{"type mismatch", "a: 1\n", []string{"delete", ".a.b"}},
		{"file not found", "", []string{"delete", ".a", "/no/such/file.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := execute(t, tc.stdin, tc.args...); err == nil {
				t.Fatalf("expected error for %v", tc.args)
			}
		})
	}
}
