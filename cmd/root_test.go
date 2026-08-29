package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const doc = `
meta:
  name: demo
  tags: [a, b, c]
items:
  - id: 1
  - id: 2
---
meta:
  name: second
`

func execute(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	c := NewRootCommand()
	var out bytes.Buffer
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestExecuteRaw(t *testing.T) {
	got, err := execute(t, doc, "-o", "raw", "meta.name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "demo" {
		t.Fatalf("got %q, want %q", got, "demo")
	}
}

func TestExecuteJSON(t *testing.T) {
	got, err := execute(t, doc, "-o", "json", "meta.tags[1]")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != `"b"` {
		t.Fatalf("got %q, want %q", got, `"b"`)
	}
}

func TestExecuteSecondDoc(t *testing.T) {
	got, err := execute(t, doc, "--doc", "1", "-o", "raw", "meta.name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "second" {
		t.Fatalf("got %q, want %q", got, "second")
	}
}

func TestExecuteMissingPath(t *testing.T) {
	if _, err := execute(t, doc, "meta.nope"); err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestExecuteMaxBytes(t *testing.T) {
	big := "data: " + strings.Repeat("x", 4096) + "\n"

	if _, err := execute(t, big, "--max-bytes", "128", "data"); err == nil {
		t.Fatal("expected error when input exceeds --max-bytes")
	}

	got, err := execute(t, big, "--max-bytes", "0", "-o", "raw", "data")
	if err != nil {
		t.Fatalf("execute with cap disabled: %v", err)
	}
	if len(strings.TrimSpace(got)) != 4096 {
		t.Fatalf("got %d chars, want 4096", len(strings.TrimSpace(got)))
	}
}

func TestDecodeDocsStopsEarly(t *testing.T) {
	// Second doc is malformed; asking for doc 0 must not parse far enough to
	// hit it.
	stream := "name: ok\n---\n: : bad\n"
	got, err := execute(t, stream, "-o", "raw", "name")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "ok" {
		t.Fatalf("got %q, want %q", got, "ok")
	}

	if _, err := execute(t, stream, "--doc", "1", "name"); err == nil {
		t.Fatal("expected parse error when reaching the malformed second doc")
	}
}

func TestExecuteWildcard(t *testing.T) {
	got, err := execute(t, doc, "-o", "raw", "items[].id")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "1\n2\n" {
		t.Fatalf("got %q, want %q", got, "1\n2\n")
	}
}

func TestExecuteWildcardNoMatchIsEmpty(t *testing.T) {
	got, err := execute(t, doc, "-o", "raw", "items.*.nope")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExecuteDefault(t *testing.T) {
	got, err := execute(t, doc, "-o", "raw", "--default", "N/A", "meta.missing")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(got) != "N/A" {
		t.Fatalf("got %q, want %q", got, "N/A")
	}
}

func TestExecuteExitStatusMiss(t *testing.T) {
	out, err := execute(t, doc, "-e", "meta.missing")
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
}

func TestExecuteExitStatusHit(t *testing.T) {
	if _, err := execute(t, doc, "-e", "meta.name"); err != nil {
		t.Fatalf("want nil error on match, got %v", err)
	}
}

func TestExecuteAliasBombRejected(t *testing.T) {
	bomb := `
a: &a ["x","x","x","x","x","x","x","x","x"]
b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]
e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]
f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]
g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f]
h: [*g,*g,*g,*g,*g,*g,*g,*g,*g]
`
	if _, err := execute(t, bomb, "--max-bytes", "0", "a"); err == nil {
		t.Fatal("expected yaml.v3 to reject the alias-expansion bomb")
	}
}
