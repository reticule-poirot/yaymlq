package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// `make fuzz` is a hand-maintained list of `go test -fuzz` invocations, so a
// target added to a _test.go file is fuzzed only if someone also adds a line
// for it. Three targets went unwired that way (#177). These tests are the
// tripwire, in the same spirit as TestSchemaArgRangesCoversEveryCommand: the
// cost of a silent miss is a fuzzer that never sees a generated input, which
// looks exactly like one that passes.

var (
	fuzzFuncRe   = regexp.MustCompile(`(?m)^func (Fuzz\w+)\(`)
	fuzzInvokeRe = regexp.MustCompile(`-fuzz\s+(\S+)`)
	fuzzPkgRe    = regexp.MustCompile(`go test\s+(\./\S+)`)
)

// fuzzTarget is one `func FuzzX` and the package directory holding it.
type fuzzTarget struct {
	pkg  string // "internal/path", "cmd", ...
	name string
}

// fuzzInvocation is one `-fuzz` line of the Makefile's fuzz recipe.
type fuzzInvocation struct {
	pkg     string
	pattern *regexp.Regexp
	raw     string
}

// fuzzTargetsInTree finds every fuzz target in the repository.
func fuzzTargetsInTree(t *testing.T) []fuzzTarget {
	t.Helper()
	var targets []fuzzTarget
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata holds fixtures, not packages; .git holds copies of
			// everything.
			if d.Name() == "testdata" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(path))
		for _, m := range fuzzFuncRe.FindAllStringSubmatch(string(src), -1) {
			targets = append(targets, fuzzTarget{pkg: pkg, name: m[1]})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("found no fuzz targets at all, which means this test is not looking where it thinks it is")
	}
	return targets
}

// fuzzInvocations parses the Makefile's fuzz recipe.
func fuzzInvocations(t *testing.T) []fuzzInvocation {
	t.Helper()
	data, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}

	var out []fuzzInvocation
	inRecipe := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "fuzz:"):
			inRecipe = true
			continue
		case inRecipe && !strings.HasPrefix(line, "\t"):
			// A recipe ends at the first line that isn't a tab-indented
			// command.
			inRecipe = false
		}
		if !inRecipe || !strings.Contains(line, "-fuzz") {
			continue
		}

		pkg := fuzzPkgRe.FindStringSubmatch(line)
		pat := fuzzInvokeRe.FindStringSubmatch(line)
		if pkg == nil || pat == nil {
			t.Errorf("cannot parse fuzz recipe line: %s", strings.TrimSpace(line))
			continue
		}
		// `$$` is a Makefile-escaped `$`, which is what reaches the shell and
		// therefore what Go sees as the regexp anchor.
		raw := strings.ReplaceAll(strings.Trim(pat[1], `'"`), "$$", "$")
		re, err := regexp.Compile(raw)
		if err != nil {
			t.Errorf("fuzz pattern %q is not a valid regexp: %v", raw, err)
			continue
		}
		out = append(out, fuzzInvocation{
			pkg:     strings.TrimPrefix(filepath.ToSlash(pkg[1]), "./"),
			pattern: re,
			raw:     raw,
		})
	}
	if len(out) == 0 {
		t.Fatal("found no -fuzz lines in the Makefile's fuzz recipe")
	}
	return out
}

// TestMakeFuzzRunsEveryFuzzTarget asserts `make fuzz` reaches every fuzz
// target in the tree. An unwired target still passes `go test` — Go runs its
// seed corpus as ordinary subtests — so nothing else notices that it is never
// actually fuzzed.
func TestMakeFuzzRunsEveryFuzzTarget(t *testing.T) {
	invocations := fuzzInvocations(t)
	for _, target := range fuzzTargetsInTree(t) {
		covered := false
		for _, inv := range invocations {
			if inv.pkg == target.pkg && inv.pattern.MatchString(target.name) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("no `make fuzz` line runs %s in ./%s; add one to the fuzz recipe", target.name, target.pkg)
		}
	}
}

// TestMakeFuzzPatternsMatchExactlyOneTarget guards the other half: `-fuzz`
// takes a regexp and Go refuses a pattern matching more than one target, so an
// unanchored name silently breaks the moment a longer name is added beside it
// (what `-fuzz FuzzRun` did once FuzzRunMatchesPathsResolveBack existed).
func TestMakeFuzzPatternsMatchExactlyOneTarget(t *testing.T) {
	targets := fuzzTargetsInTree(t)
	for _, inv := range fuzzInvocations(t) {
		var matched []string
		for _, target := range targets {
			if target.pkg == inv.pkg && inv.pattern.MatchString(target.name) {
				matched = append(matched, target.name)
			}
		}
		switch len(matched) {
		case 1:
			// Good: exactly the one target this line means to run.
		case 0:
			t.Errorf("`make fuzz` pattern %q matches no target in ./%s", inv.raw, inv.pkg)
		default:
			t.Errorf("`make fuzz` pattern %q matches %d targets in ./%s (%s); go test rejects that — anchor it as '^Name$'",
				inv.raw, len(matched), inv.pkg, strings.Join(matched, ", "))
		}
	}
}
