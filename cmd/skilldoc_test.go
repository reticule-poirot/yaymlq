package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/editscript"
)

// skillPath is the distributable copy other repos' agents read. It is not
// auto-loaded here, so nothing prompts anyone to open it when the CLI changes
// (#155) — which is how it came to document the apply script format without
// its verb (#247).
const skillPath = "../contrib/claude-skill/SKILL.md"

func skillText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// splitArgs splits a documented command line the way a shell would, honouring
// single and double quotes. It handles what SKILL.md actually contains —
// quoted path expressions and values — and deliberately not pipes,
// redirection or substitution, which a documented example should not need.
func splitArgs(t *testing.T, line string) []string {
	t.Helper()
	var args []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		t.Fatalf("unbalanced quote in documented command: %s", line)
	}
	if started {
		args = append(args, cur.String())
	}
	return args
}

// skillCommands returns every yaymlq invocation SKILL.md shows, from its table
// and from any fenced block.
func skillCommands(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(skillText(t), "\n") {
		for _, chunk := range strings.Split(line, "`") {
			chunk = strings.TrimSpace(chunk)
			if !strings.HasPrefix(chunk, "yaymlq ") {
				continue
			}
			// A documented example may pipe into something else; the part
			// this file is responsible for is the invocation up to the pipe,
			// and the downstream command is the shell's business.
			if i := strings.IndexByte(chunk, '|'); i >= 0 {
				chunk = strings.TrimSpace(chunk[:i])
			}
			out = append(out, chunk)
		}
	}
	return out
}

// skillFixtures writes the files SKILL.md's examples name, so a documented
// command can actually run. The contents only have to carry the shapes the
// examples reach for.
func skillFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"compose.yml": "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"80:80\"\n    environment:\n      DEBUG: \"1\"\n",
		"deploy.yaml": "spec:\n  replicas: 3\n  containers:\n    - name: web\n",
		"manifests.yaml": "kind: Deployment\nmetadata:\n  labels:\n    team: core\nspec:\n  replicas: 1\n" +
			"---\nkind: Service\nmetadata:\n  labels:\n    team: core\nspec:\n  replicas: 2\n",
		"config.yaml": "a: 1\nb: 2\n",
		"edits.txt":   "set .services.web.image = nginx:1.28\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestSkillCommandsAreNotUsageErrors is #247's tripwire. Every invocation
// SKILL.md shows must at least be well formed: exit 3 is precisely what a
// renamed flag, a dropped argument or a bad grammar produces, so it is the
// signal that the file has drifted from the CLI.
//
// Deliberately not asserting exit 0. Some documented commands legitimately
// miss against a small fixture, and demanding a specific result would make
// this a test of the fixtures rather than of the documentation.
func TestSkillCommandsAreNotUsageErrors(t *testing.T) {
	dir := skillFixtures(t)
	cmds := skillCommands(t)
	if len(cmds) < 15 {
		t.Fatalf("found only %d commands in %s; the extraction or the file changed shape", len(cmds), skillPath)
	}
	for _, c := range cmds {
		t.Run(c, func(t *testing.T) {
			args := splitArgs(t, strings.TrimPrefix(c, "yaymlq "))
			for i, a := range args {
				// Point a documented filename at the fixture of that name.
				if strings.Contains(a, ".yml") || strings.Contains(a, ".yaml") || strings.Contains(a, ".txt") {
					if _, err := os.Stat(filepath.Join(dir, a)); err == nil {
						args[i] = filepath.Join(dir, a)
					}
				}
			}
			_, err := execute(t, "a: 1\n", args...)
			if code := exitCode(err, io.Discard); code == 3 {
				t.Errorf("documented command is a usage error: yaymlq %s\n  %v", strings.Join(args, " "), err)
			}
		})
	}
}

// TestSkillApplyScriptParses binds the claim that drifted. The format is prose
// in SKILL.md, and nothing runs prose — so the file now carries a real script
// in a fenced block, and this parses it with the same parser apply uses.
func TestSkillApplyScriptParses(t *testing.T) {
	text := skillText(t)
	_, rest, ok := strings.Cut(text, "```apply")
	if !ok {
		t.Fatalf("%s has no ```apply block; #247 added one so the script format is runnable rather than described", skillPath)
	}
	script, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("the apply block is unterminated")
	}
	script = strings.TrimSpace(script)
	if script == "" {
		t.Fatal("the apply block is empty")
	}

	ops, err := editscript.Parse(strings.NewReader(script))
	if err != nil {
		t.Fatalf("the documented apply script does not parse: %v\n%s", err, script)
	}
	if len(ops) == 0 {
		t.Fatal("the documented apply script has no operations")
	}
	// Every verb apply accepts should be exercised by the example, so the
	// block shows the shape of each rather than only the one anybody guesses.
	seen := map[editscript.Verb]bool{}
	for _, op := range ops {
		seen[op.Verb] = true
	}
	for _, want := range []editscript.Verb{"set", "delete"} {
		if !seen[want] {
			t.Errorf("the documented script never shows %q", want)
		}
	}
}
