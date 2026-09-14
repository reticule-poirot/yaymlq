package cmd

import (
	"errors"
	"testing"
)

func TestGetPrint0(t *testing.T) {
	in := "services:\n  web:\n    image: nginx\n  db:\n    image: postgres\n"
	got, err := execute(t, in, "-0", "services.*.image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "postgres\x00nginx"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetPrint0SingleResultHasNoTrailingSeparator(t *testing.T) {
	got, err := execute(t, "a: 1\n", "-0", ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "1" {
		t.Fatalf("got %q, want %q", got, "1")
	}
}

func TestGetPrint0ImpliesRaw(t *testing.T) {
	// No explicit -o; -0 should force raw without erroring.
	if _, err := execute(t, "a: 1\n", "-0", ".a"); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestGetPrint0ConflictsWithExplicitFormat(t *testing.T) {
	if _, err := execute(t, "a: 1\n", "-0", "-o", "json", ".a"); err == nil {
		t.Fatal("want error combining -0 with an explicit non-raw -o")
	}
}

func TestKeysPrint0(t *testing.T) {
	got, err := execute(t, "a:\n  x: 1\n  y: 2\n", "keys", "-0", ".a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "x\x00y"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetPrint0NoMatchesFlushesEmpty(t *testing.T) {
	got, err := execute(t, "a: 1\n", "-0", "-e", "b.*")
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
	if got != "" {
		t.Fatalf("want empty output, got %q", got)
	}
}
