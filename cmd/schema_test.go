package cmd

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func schemaManifest(t *testing.T) manifest {
	t.Helper()
	got, err := execute(t, "", "schema")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var m manifest
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("schema output is not valid JSON: %v\noutput: %s", err, got)
	}
	return m
}

func TestSchemaIsValidJSON(t *testing.T) {
	schemaManifest(t) // fails the test itself on any parse error
}

func TestSchemaManifestVersionPositive(t *testing.T) {
	m := schemaManifest(t)
	if m.ManifestVersion <= 0 {
		t.Fatalf("manifestVersion = %d, want > 0", m.ManifestVersion)
	}
}

func TestSchemaExcludesCompletionAndHelp(t *testing.T) {
	m := schemaManifest(t)
	for _, c := range m.Commands {
		if c.Name == "completion" || c.Name == "help" {
			t.Fatalf("manifest includes cobra's own %q command", c.Name)
		}
	}
}

func findCommand(t *testing.T, m manifest, name string) commandManifest {
	t.Helper()
	for _, c := range m.Commands {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no command named %q in manifest", name)
	return commandManifest{}
}

func findFlag(t *testing.T, c commandManifest, name string) flagManifest {
	t.Helper()
	for _, f := range c.Flags {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("command %q has no flag named %q", c.Name, name)
	return flagManifest{}
}

func TestSchemaSetHasStringFlag(t *testing.T) {
	m := schemaManifest(t)
	f := findFlag(t, findCommand(t, m, "set"), "string")
	if f.Shorthand != "s" || f.Type != "bool" || f.Default != "false" {
		t.Fatalf("set's string flag = %+v, want shorthand=s type=bool default=false", f)
	}
}

func TestSchemaOutputFlagDefaultsDiffer(t *testing.T) {
	m := schemaManifest(t)
	if got := findFlag(t, findCommand(t, m, "get"), "output").Default; got != "yaml" {
		t.Fatalf("get's output flag default = %q, want yaml", got)
	}
	if got := findFlag(t, findCommand(t, m, "keys"), "output").Default; got != "raw" {
		t.Fatalf("keys's output flag default = %q, want raw", got)
	}
}

func TestSchemaExitCodesExactlyFive(t *testing.T) {
	m := schemaManifest(t)
	if len(m.ExitCodes) != 5 {
		t.Fatalf("len(exitCodes) = %d, want 5", len(m.ExitCodes))
	}
	seen := map[int]bool{}
	for _, ec := range m.ExitCodes {
		seen[ec.Code] = true
	}
	for code := 0; code <= 4; code++ {
		if !seen[code] {
			t.Fatalf("exitCodes missing code %d", code)
		}
	}
}

func TestSchemaJSONErrorsMatchesKnownCommands(t *testing.T) {
	m := schemaManifest(t)
	want := map[string]bool{"get": true, "keys": true, "len": true, "type": true}
	for _, c := range m.Commands {
		if c.JSONErrors != want[c.Name] {
			t.Errorf("command %q: jsonErrors = %v, want %v", c.Name, c.JSONErrors, want[c.Name])
		}
	}
}

func TestSchemaValidateArgsUnbounded(t *testing.T) {
	m := schemaManifest(t)
	v := findCommand(t, m, "validate")
	if v.Args.Min != 0 || v.Args.Max != nil {
		t.Fatalf("validate's args = %+v, want min=0 max=nil", v.Args)
	}
}

// TestSchemaArgRangesCoversEveryCommand catches a new command added later
// without a matching argRanges entry: buildManifest would otherwise only
// discover the gap by panicking at runtime.
func TestSchemaArgRangesCoversEveryCommand(t *testing.T) {
	root := NewRootCommand()
	names := []string{commandName(root)}
	for _, c := range root.Commands() {
		if excludedCommands[c.Name()] {
			continue
		}
		names = append(names, commandName(c))
	}
	for _, name := range names {
		if _, ok := argRanges[name]; !ok {
			t.Errorf("argRanges has no entry for command %q", name)
		}
	}
}

// schemaManifestFor runs `schema` with extra arguments and decodes the result.
func schemaManifestFor(t *testing.T, args ...string) manifest {
	t.Helper()
	got, err := execute(t, "", append([]string{"schema"}, args...)...)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	var m manifest
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("schema %v output is not valid JSON: %v\noutput: %s", args, err, got)
	}
	return m
}

func commandNames(m manifest) []string {
	names := make([]string, 0, len(m.Commands))
	for _, c := range m.Commands {
		names = append(names, c.Name)
	}
	return names
}

func TestSchemaCommandFilterNarrowsToOne(t *testing.T) {
	m := schemaManifestFor(t, "--command", "set")
	if got := commandNames(m); len(got) != 1 || got[0] != "set" {
		t.Fatalf("--command set: got commands %v, want [set]", got)
	}
}

func TestSchemaCommandFilterIsRepeatable(t *testing.T) {
	m := schemaManifestFor(t, "--command", "set", "--command", "get")
	got := commandNames(m)
	// Tree order, not the order the flags were given, so output is stable.
	if len(got) != 2 || got[0] != "get" || got[1] != "set" {
		t.Fatalf("got commands %v, want [get set] in tree order", got)
	}
}

// TestSchemaCommandFilterKeepsManifestSelfDescribing: narrowing drops
// commands, not the surrounding contract. A caller that asked about one verb
// still needs the exit-code table to interpret what that verb returns.
func TestSchemaCommandFilterKeepsManifestSelfDescribing(t *testing.T) {
	m := schemaManifestFor(t, "--command", "set")
	if m.ManifestVersion <= 0 || m.Version == "" {
		t.Errorf("narrowed manifest lost its version fields: %+v", m)
	}
	if len(m.ExitCodes) != len(exitCodeTable) {
		t.Errorf("narrowed manifest has %d exit codes, want all %d", len(m.ExitCodes), len(exitCodeTable))
	}
}

// TestSchemaCommandFilterMatchesFullManifest is the anti-drift guard: a
// narrowed entry must be the same entry the full manifest carries, not a
// separately-built one that can diverge.
func TestSchemaCommandFilterMatchesFullManifest(t *testing.T) {
	full := schemaManifest(t)
	for _, want := range full.Commands {
		t.Run(want.Name, func(t *testing.T) {
			m := schemaManifestFor(t, "--command", want.Name)
			if len(m.Commands) != 1 {
				t.Fatalf("got %d commands, want 1", len(m.Commands))
			}
			gotJSON, _ := json.Marshal(m.Commands[0])
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("narrowed entry differs from the full manifest's:\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestSchemaWithoutFilterListsEveryCommand(t *testing.T) {
	full := schemaManifest(t)
	if len(full.Commands) < 2 {
		t.Fatalf("unfiltered schema should list every command, got %v", commandNames(full))
	}
}

func TestSchemaCommandFilterUnknownNameIsUsageError(t *testing.T) {
	_, err := execute(t, "", "schema", "--command", "nope")
	if got := exitCode(err, io.Discard); got != 3 {
		t.Fatalf("want exit 3 for an unknown --command name, got %d (err %v)", got, err)
	}
}

// TestSchemaCommandFilterErrorNamesValidCommands: the error has the valid
// names in hand at the moment it fails, so recovering shouldn't cost a second
// invocation.
func TestSchemaCommandFilterErrorNamesValidCommands(t *testing.T) {
	_, err := execute(t, "", "schema", "--command", "nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	for _, want := range []string{"set", "get", "validate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name the valid commands, %q missing from: %v", want, err)
		}
	}
}

// TestSchemaCommandFilterIsMuchSmaller is the point of the flag: a targeted
// lookup shouldn't cost the whole manifest.
func TestSchemaCommandFilterIsMuchSmaller(t *testing.T) {
	full, err := execute(t, "", "schema")
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := execute(t, "", "schema", "--command", "set")
	if err != nil {
		t.Fatal(err)
	}
	if len(narrow)*3 > len(full) {
		t.Fatalf("--command set is %d bytes against a full manifest of %d; expected a far bigger saving", len(narrow), len(full))
	}
}
