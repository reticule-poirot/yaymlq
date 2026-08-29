package cmd

import (
	"bytes"
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
