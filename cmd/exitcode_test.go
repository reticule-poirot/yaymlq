package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/spf13/cobra"
)

// wantExit runs args against stdin and asserts the resulting process exit
// code, the same mapping Execute() applies via exitCode.
// wantExit asserts one invocation's exit code as its own subtest, so a table
// of them reports every mismatch rather than stopping at the first. These
// tables run 39 assertions between them and the biggest holds 15; aborting on
// the first failure meant a change that broke several exit codes surfaced as
// one, and the rest only appeared one re-run at a time.
func wantExit(t *testing.T, stdin string, want int, args ...string) {
	t.Helper()
	t.Run(strings.Join(args, " "), func(t *testing.T) {
		t.Helper()
		_, err := execute(t, stdin, args...)
		if got := exitCode(err, io.Discard); got != want {
			t.Fatalf("%v -> exit %d (%v), want %d", args, got, err, want)
		}
	})
}

func TestExitCodesGet(t *testing.T) {
	wantExit(t, doc, 0, "meta.name")
	wantExit(t, doc, 1, "meta.missing")
	wantExit(t, doc, 3, "--nope", "meta.name")            // unknown flag
	wantExit(t, doc, 3, "a", "b", "c")                    // too many args
	wantExit(t, doc, 3, "a[")                             // bad path syntax
	wantExit(t, doc, 3, "--doc", "5", "meta.name")        // doc index out of range
	wantExit(t, doc, 3, "--default", "[", "meta.missing") // bad --default value
	wantExit(t, doc, 3, "--print0", "-o", "json", "meta.name")
	wantExit(t, doc, 3, "-o", "xml", "meta.name")                // unknown output format
	wantExit(t, doc, 3, "-q", "-o", "xml", "meta.name")          // ...even under --quiet
	wantExit(t, doc, 3, "--doc", "1", "--all-docs", "meta.name") // --doc + --all-docs
	wantExit(t, doc, 3, "--doc", "-1", "meta.name")              // negative --doc
	wantExit(t, doc, 3, "--max-bytes", "-1", "meta.name")        // negative --max-bytes
	wantExit(t, "", 4, "meta.name", "/no/such/file.yaml")        // missing file
	wantExit(t, "a: [1, 2", 2, "meta.name")                      // malformed YAML
}

func TestExitCodesInspect(t *testing.T) {
	wantExit(t, doc, 0, "keys", "meta")
	wantExit(t, doc, 3, "keys", "a[")
	wantExit(t, doc, 3, "len")                                      // too few args
	wantExit(t, doc, 3, "keys", "-o", "xml", "meta")                // unknown output format
	wantExit(t, doc, 3, "keys", "--doc", "1", "--all-docs", "meta") // --doc + --all-docs
	wantExit(t, doc, 3, "keys", "--doc", "-1", "meta")              // negative --doc
	wantExit(t, "", 4, "keys", "meta", "/no/such/file.yaml")
	wantExit(t, "a: [1, 2", 2, "keys", "meta")
}

func TestExitCodesSet(t *testing.T) {
	wantExit(t, doc, 0, "set", ".meta.name", "x")
	wantExit(t, doc, 3, "set", "-i", ".meta.name", "x") // --in-place, no file
	wantExit(t, doc, 3, "set", "a[", "x")               // bad path syntax
	wantExit(t, doc, 3, "set", ".a", "[")               // bad value syntax
	wantExit(t, doc, 3, "set", "--indent", "0", ".a", "x")
	wantExit(t, "", 4, "set", ".a", "x", "/no/such/file.yaml")
	wantExit(t, "a: [1, 2", 2, "set", ".a", "x")
}

func TestExitCodesDeleteAppendRename(t *testing.T) {
	wantExit(t, doc, 3, "delete", "a[")
	wantExit(t, doc, 3, "append", "a[", "x")
	wantExit(t, doc, 3, "rename", "a[", "x")
	wantExit(t, "", 4, "delete", ".meta.name", "/no/such/file.yaml")
	wantExit(t, "a: [1, 2", 2, "rename", ".a", "b")
}

func TestExitCodesValidateStaysAggregate(t *testing.T) {
	// validate deliberately keeps its existing flat exit 1 on any failure
	// (parse or --require) rather than being split into the new taxonomy.
	wantExit(t, "a: [1, 2", 1, "validate")
	wantExit(t, doc, 1, "validate", "--require", ".nope")
	wantExit(t, doc, 0, "validate")
}

func TestExitCodeClassified(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"parse", parseErr(errors.New("bad yaml")), 2},
		{"usage", usageErr(errors.New("bad flag")), 3},
		{"io", ioErr(errors.New("no such file")), 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err, &bytes.Buffer{}); got != tc.want {
				t.Fatalf("%v -> %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyNilStaysNil(t *testing.T) {
	if parseErr(nil) != nil || usageErr(nil) != nil || ioErr(nil) != nil {
		t.Fatal("classifying a nil error must stay nil")
	}
}

func TestClassifiedErrorPreservesMessage(t *testing.T) {
	err := usageErr(errors.New("bad --indent"))
	if err.Error() != "bad --indent" {
		t.Fatalf("Error() = %q, want unchanged message", err.Error())
	}
}

func TestClassifiedErrorUnwraps(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := ioErr(sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatal("classified error should unwrap to the original")
	}
}

func TestPathErrClassifiesSyntaxErrors(t *testing.T) {
	_, perr := path.Parse("a[")
	if exitCode(pathErr(perr), io.Discard) != 3 {
		t.Fatalf("pathErr(syntax error) should classify as usage (exit 3)")
	}
}

func TestPathErrLeavesOtherErrorsUnclassified(t *testing.T) {
	plain := errors.New("path not found: a.b")
	got := pathErr(plain)
	if !errors.Is(got, plain) {
		t.Fatalf("pathErr should pass through a non-syntax error unchanged, got %v", got)
	}
	if exitCode(got, io.Discard) != 1 {
		t.Fatalf("a non-syntax path error should stay in the default exit-1 family")
	}
}

func TestUsageArgsClassifiesMismatch(t *testing.T) {
	fn := usageArgs(cobra.RangeArgs(1, 2))
	err := fn(&cobra.Command{}, nil)
	if exitCode(err, io.Discard) != 3 {
		t.Fatalf("usageArgs should classify a count mismatch as usage (exit 3)")
	}
}

func TestUsageArgsPassesOK(t *testing.T) {
	fn := usageArgs(cobra.RangeArgs(0, 2))
	if err := fn(&cobra.Command{}, []string{"a"}); err != nil {
		t.Fatalf("want nil for an in-range count, got %v", err)
	}
}
