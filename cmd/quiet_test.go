package cmd

import (
	"errors"
	"testing"
)

func TestExecuteQuietHit(t *testing.T) {
	out, err := execute(t, doc, "-q", "meta.name")
	if err != nil {
		t.Fatalf("want nil error on match, got %v", err)
	}
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
}

func TestExecuteQuietMiss(t *testing.T) {
	out, err := execute(t, doc, "-q", "meta.missing")
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
	var se silentExit
	if !errors.As(err, &se) || se.code != 1 {
		t.Fatalf("want silentExit{1}, got %v", err)
	}
}

func TestExecuteQuietWildcardHit(t *testing.T) {
	// A wildcard path with at least one match counts as a hit even though
	// most branches miss.
	if _, err := execute(t, doc, "-q", "items[].id"); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
}

func TestExecuteQuietSuppressesOutputFormat(t *testing.T) {
	// -q wins over -o / --raw: no output either way.
	out, err := execute(t, doc, "-q", "-o", "json", "meta.name")
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if out != "" {
		t.Fatalf("want no output, got %q", out)
	}
}
