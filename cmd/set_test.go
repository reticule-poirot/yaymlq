package cmd

import (
	"os"
	"path/filepath"
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
