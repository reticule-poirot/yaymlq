package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// manifest is the top-level shape of `yaymlq schema`'s JSON output: a
// machine-readable description of yaymlq's own command/flag/exit-code
// surface, so a script or LLM agent can consume it instead of parsing
// --help text or guessing exit-code meaning.
type manifest struct {
	ManifestVersion int               `json:"manifestVersion"`
	Version         string            `json:"version"`
	Commands        []commandManifest `json:"commands"`
	ExitCodes       []exitCodeInfo    `json:"exitCodes"`
}

type commandManifest struct {
	Name       string         `json:"name"`
	Aliases    []string       `json:"aliases,omitempty"`
	Short      string         `json:"short"`
	Long       string         `json:"long,omitempty"`
	Example    string         `json:"example,omitempty"`
	Args       argsSpec       `json:"args"`
	Flags      []flagManifest `json:"flags,omitempty"`
	JSONErrors bool           `json:"jsonErrors"`
}

// argsSpec mirrors the cobra.RangeArgs(min, max) (or ArbitraryArgs) call
// each command's own Args field already uses. Max is nil for a command
// with no upper bound (validate).
type argsSpec struct {
	Min int  `json:"min"`
	Max *int `json:"max,omitempty"`
}

type flagManifest struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

type exitCodeInfo struct {
	Code        int    `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// excludedCommands are cobra's own auto-injected commands — added because
// CompletionOptions is never customized, and because root.Version is set —
// not part of yaymlq's own hand-written surface.
var excludedCommands = map[string]bool{"completion": true, "help": true}

// jsonErrorCommands names the commands whose RunE runs its error through
// handleErr (jsonerr.go), emitting a structured {"error","kind",...} object
// on stderr under -o json instead of a plain "Error: ..." line. Nothing on
// *cobra.Command records "this RunE calls handleErr", so this is kept in
// sync by hand; TestSchemaJSONErrorsMatchesKnownCommands is a tripwire for
// drift, not a guarantee.
var jsonErrorCommands = map[string]bool{"get": true, "keys": true, "len": true, "type": true}

// argRanges mirrors each command's own cobra.RangeArgs(min, max) call —
// cobra doesn't expose a PositionalArgs closure's bounds back out, so this
// is kept in sync by hand. TestSchemaArgRangesCoversEveryCommand asserts
// every command in the real tree has an entry.
var argRanges = map[string]argsSpec{
	"get":      {Min: 0, Max: intPtr(2)},
	"set":      {Min: 2, Max: intPtr(3)},
	"append":   {Min: 2, Max: intPtr(3)},
	"delete":   {Min: 1, Max: intPtr(2)},
	"rename":   {Min: 2, Max: intPtr(3)},
	"apply":    {Min: 0, Max: intPtr(1)},
	"keys":     {Min: 1, Max: intPtr(2)},
	"len":      {Min: 1, Max: intPtr(2)},
	"type":     {Min: 1, Max: intPtr(2)},
	"validate": {Min: 0, Max: nil},
	"schema":   {Min: 0, Max: intPtr(0)},
}

func intPtr(n int) *int { return &n }

// exitCodeTable documents the scheme in execute.go's exitCode doc comment.
// Names reuse kindFor's (jsonerr.go) vocabulary so this table and -o json's
// "kind" field agree. Small and stable, so unlike the command/flag data
// above it isn't worth reflecting.
var exitCodeTable = []exitCodeInfo{
	{Code: 0, Name: "success", Description: "the command completed successfully"},
	{Code: 1, Name: "no-match", Description: "a deliberate -e/-q miss, a query or edit path that didn't resolve, or validate's aggregate failure"},
	{Code: 2, Name: "parse", Description: "the input YAML did not parse"},
	{Code: 3, Name: "usage", Description: "a bad flag, argument count, path expression, or value"},
	{Code: 4, Name: "io", Description: "a file or stream read or write failed"},
}

// newSchemaCommand builds the read-only `schema` verb: a JSON manifest of
// yaymlq's own command/flag/exit-code surface for a script or agent to
// introspect instead of parsing --help text.
func newSchemaCommand() *cobra.Command {
	var only []string
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print a JSON manifest of yaymlq's own commands, flags, and exit codes",
		Long: `schema introspects yaymlq's own command tree and prints a stable,
machine-readable JSON description of it -- every command's flags,
argument-count constraints, and the exit-code scheme -- so a script or LLM
agent can consume it instead of parsing --help text.`,
		Example:      "  yaymlq schema\n  yaymlq schema --command set\n  yaymlq schema | jq '.commands[].name'",
		Args:         usageArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			m, err := buildManifest(c.Root(), only)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return err // manifest is all structs/strings/ints; cannot fail in practice
			}
			_, err = fmt.Fprintln(c.OutOrStdout(), string(data))
			return ioErr(err)
		},
	}
	cmd.Flags().StringArrayVarP(&only, "command", "c", nil,
		"limit the manifest to this command (repeatable); default is every command")
	return cmd
}

// buildManifest walks root's command tree (root itself, plus every direct
// child not in excludedCommands) into a manifest. A non-empty only limits
// which commands are described; the surrounding fields (version, exit codes)
// are kept either way, since a caller asking about one verb still needs the
// exit-code table to interpret what that verb returns.
//
// Commands come out in tree order regardless of the order names were given,
// so the output of a given --command set is byte-stable.
func buildManifest(root *cobra.Command, only []string) (manifest, error) {
	m := manifest{ManifestVersion: 1, Version: version, ExitCodes: exitCodeTable}

	all := []*cobra.Command{root}
	for _, c := range root.Commands() {
		if excludedCommands[c.Name()] {
			continue
		}
		all = append(all, c)
	}

	want := make(map[string]bool, len(only))
	for _, n := range only {
		want[n] = true
	}

	names := make([]string, 0, len(all))
	for _, c := range all {
		name := commandName(c)
		names = append(names, name)
		if len(only) == 0 || want[name] {
			m.Commands = append(m.Commands, commandManifestFor(c))
			delete(want, name)
		}
	}

	// The valid names are in hand at the moment this fails, so recovering
	// from a typo shouldn't cost a second invocation.
	if len(want) > 0 {
		unknown := make([]string, 0, len(want))
		for n := range want {
			unknown = append(unknown, n)
		}
		sort.Strings(unknown)
		return manifest{}, usageErr(fmt.Errorf("unknown --command %s (want one of: %s)",
			strings.Join(unknown, ", "), strings.Join(names, ", ")))
	}
	return m, nil
}

// commandName gives the root command's default query action the name "get"
// -- matching keys/len/type's verb naming -- since the root has no literal
// subcommand word of its own (it's invoked as `yaymlq <path> [file]`).
func commandName(c *cobra.Command) string {
	if c.Parent() == nil {
		return "get"
	}
	return c.Name()
}

func commandManifestFor(c *cobra.Command) commandManifest {
	name := commandName(c)
	spec, ok := argRanges[name]
	if !ok {
		panic("schema: no argRanges entry for command " + name)
	}
	cm := commandManifest{
		Name:       name,
		Aliases:    c.Aliases,
		Short:      c.Short,
		Long:       c.Long,
		Example:    c.Example,
		Args:       spec,
		JSONErrors: jsonErrorCommands[name],
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Name == "version" {
			return
		}
		cm.Flags = append(cm.Flags, flagManifest{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Type:        f.Value.Type(),
			Default:     f.DefValue,
			Description: f.Usage,
		})
	})
	return cm
}
