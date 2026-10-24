package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const descendStream = "image: top\n" +
	"spec:\n" +
	"  image: mid\n" +
	"  containers:\n" +
	"    - name: web\n" +
	"      image: deep\n"

// TestGetDescend: `**` end to end through the CLI (#174).
func TestGetDescend(t *testing.T) {
	got, err := execute(t, descendStream, "-o", "raw", "**.image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "top\nmid\ndeep\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGetDescendInterior keeps the anchored form honest: the top-level image
// is out of scope under `spec.**`.
func TestGetDescendInterior(t *testing.T) {
	got, err := execute(t, descendStream, "-o", "raw", "spec.**.image")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := "mid\ndeep\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDescendPathsFeedApply is the pairing that makes descent more than a
// nicer grep: every editing verb refuses a fan-out path, so `--paths` is the
// only route from "find this key wherever it is" to "edit all of them".
func TestDescendPathsFeedApply(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "doc.yaml")
	if err := os.WriteFile(f, []byte(descendStream), 0o644); err != nil {
		t.Fatal(err)
	}
	listing, err := execute(t, "", "--paths", "**.image", f)
	if err != nil {
		t.Fatalf("--paths: %v", err)
	}
	want := "image\nspec.image\nspec.containers[0].image\n"
	if listing != want {
		t.Fatalf("paths = %q, want %q", listing, want)
	}
	var script strings.Builder
	for _, p := range strings.Fields(listing) {
		script.WriteString("set " + p + " = pinned\n")
	}
	s := writeScript(t, dir, script.String())
	if _, err := execute(t, "", "apply", "-f", s, "-i", f); err != nil {
		t.Fatalf("apply: %v", err)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "pinned"); n != 3 {
		t.Errorf("want all three edited, got %d:\n%s", n, data)
	}
}

// TestEditVerbsRefuseDescendFromTheCLI: the refusal reaches the user, with
// the exit code every other unresolved-edit failure uses.
func TestEditVerbsRefuseDescendFromTheCLI(t *testing.T) {
	for _, tc := range []struct {
		verb string
		args []string
	}{
		{"set", []string{"set", "**.image", "x"}},
		{"append", []string{"append", "**.image", "x"}},
		{"delete", []string{"delete", "**.image"}},
		{"rename", []string{"rename", "**.image", "other"}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			out, err := execute(t, descendStream, tc.args...)
			if err == nil {
				t.Fatalf("%s accepted a descent path:\n%s", tc.verb, out)
			}
			if msg := err.Error(); !strings.Contains(msg, "wildcard") {
				t.Errorf("want the refusal to name the wildcard, got %q", msg)
			}
			// Specifically not a "no such key" — before the refusal existed,
			// `set` would have created a key literally named **.
			if msg := err.Error(); strings.Contains(msg, "no such key") {
				t.Errorf("descent must be refused, not treated as a literal key: %q", msg)
			}
		})
	}
}

// TestDescendMatchingNothingIsNotedNotErrored: descent fans out, so it
// inherits the wildcard contract and #173's note rather than an error.
func TestDescendMatchingNothingIsNotedNotErrored(t *testing.T) {
	stdout, stderr, err := executeSplit(t, descendStream, "**.nope")
	if err != nil {
		t.Fatalf("want exit 0, got %v", err)
	}
	if stdout != "" {
		t.Errorf("stdout must stay empty, got %q", stdout)
	}
	if !strings.Contains(stderr, "matched nothing") {
		t.Errorf("want the no-match note, got %q", stderr)
	}
}

// TestDescendCannotHangOnACyclicAlias pins what the unbounded walk's safety
// rests on: yaml.v3 refuses a self-referential anchor while parsing, so a
// decoded document is a finite tree and descent always terminates. If that
// ever changed, this test is what would notice.
func TestDescendCannotHangOnACyclicAlias(t *testing.T) {
	_, err := execute(t, "a: &x\n  b: *x\n", "**.b")
	if err == nil {
		t.Fatal("want a parse error for a self-referential anchor")
	}
	if code := codeFor(err); code != 2 {
		t.Errorf("want exit 2 (parse), got %d", code)
	}
}

// TestLiteralDoubleStarKeyStaysUsableEndToEnd is the breaking half of #174.
// `**` is a valid, addressable key in YAML, and `--paths` used to render it
// bare — so the same rendered line now has to come back quoted, or a script
// built from a listing means "every depth" instead of "that key". Same seam
// as #157 and #158, one level up: not truncated, re-interpreted.
func TestLiteralDoubleStarKeyStaysUsableEndToEnd(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "odd.yaml")
	const in = "\"**\":\n  image: odd\nservices:\n  web:\n    image: nginx\n"
	if err := os.WriteFile(f, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	listing, err := execute(t, "", "--paths", "*.image", f)
	if err != nil {
		t.Fatalf("--paths: %v", err)
	}
	if strings.Contains(listing, "\n**.image") || strings.HasPrefix(listing, "**.image") {
		t.Fatalf("a literal ** key was rendered bare, so it reads as the token: %q", listing)
	}
	if !strings.Contains(listing, `"**".image`) {
		t.Fatalf("want the key quoted, got %q", listing)
	}
	// And the quoted form survives the script grammar, which is where #158
	// showed a rendered path can still be misread.
	s := writeScript(t, dir, "set "+strings.TrimSpace(listing)+" = changed\n")
	if _, err := execute(t, "", "apply", "-f", s, "-i", f); err != nil {
		t.Fatalf("apply: %v", err)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "image: changed") {
		t.Errorf("the edit did not land on the ** key:\n%s", data)
	}
	if !strings.Contains(string(data), "image: nginx") {
		t.Errorf("the edit touched an unrelated key:\n%s", data)
	}
}
