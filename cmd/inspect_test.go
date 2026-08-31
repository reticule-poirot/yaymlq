package cmd

import (
	"strings"
	"testing"
)

const inspectDoc = `
name: demo
meta:
  labels: {tier: backend, app: api}
  ports: [80, 443, 8080]
enabled: true
replicas: 3
note: ~
`

func TestKeys(t *testing.T) {
	got, err := execute(t, inspectDoc, "keys", ".meta.labels")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "app\ntier\n" { // sorted
		t.Fatalf("got %q", got)
	}
}

func TestKeysOfList(t *testing.T) {
	got, err := execute(t, inspectDoc, "keys", ".meta.ports")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "0\n1\n2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestKeysJSON(t *testing.T) {
	got, err := execute(t, inspectDoc, "keys", "-o", "json", ".meta.labels")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Fields(got)[0] != `"app"` {
		t.Fatalf("got %q", got)
	}
}

func TestKeysOnScalarErrors(t *testing.T) {
	if _, err := execute(t, inspectDoc, "keys", ".name"); err == nil {
		t.Fatal("expected error: scalar has no keys")
	}
}

func TestLen(t *testing.T) {
	cases := map[string]string{
		".":           "5\n", // name, meta, enabled, replicas, note
		".meta":       "2\n", // labels, ports
		".meta.ports": "3\n", // 80, 443, 8080
		".name":       "4\n", // "demo"
		".note":       "0\n", // null
	}
	for path, want := range cases {
		got, err := execute(t, inspectDoc, "len", path)
		if err != nil {
			t.Fatalf("len %s: %v", path, err)
		}
		if got != want {
			t.Fatalf("len %s = %q, want %q", path, got, want)
		}
	}
}

func TestLenOnNumberErrors(t *testing.T) {
	if _, err := execute(t, inspectDoc, "len", ".replicas"); err == nil {
		t.Fatal("expected error: number has no length")
	}
}

func TestType(t *testing.T) {
	cases := map[string]string{
		".meta":       "object\n",
		".meta.ports": "array\n",
		".name":       "string\n",
		".replicas":   "number\n",
		".enabled":    "boolean\n",
		".note":       "null\n",
	}
	for path, want := range cases {
		got, err := execute(t, inspectDoc, "type", path)
		if err != nil {
			t.Fatalf("type %s: %v", path, err)
		}
		if got != want {
			t.Fatalf("type %s = %q, want %q", path, got, want)
		}
	}
}

func TestInspectWildcard(t *testing.T) {
	got, err := execute(t, inspectDoc, "type", ".meta.*")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "object\narray\n" {
		t.Fatalf("got %q", got)
	}
}

func TestInspectMissingPathErrors(t *testing.T) {
	if _, err := execute(t, inspectDoc, "keys", ".nope"); err == nil {
		t.Fatal("expected error for a missing path")
	}
}

func TestInspectSecondDoc(t *testing.T) {
	in := "a: [1, 2]\n---\nb: {x: 1, y: 2, z: 3}\n"
	got, err := execute(t, in, "len", "--doc", "1", ".b")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "3\n" {
		t.Fatalf("got %q", got)
	}
}
