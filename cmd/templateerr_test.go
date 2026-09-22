package cmd

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

const helmChart = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: {{ include \"app.fullname\" . }}\n"

// TestTemplateErrorNamesTheCause: a chart template is the most likely thing
// someone points this at by accident, and yaml.v3's own message for it is a
// %#v dump of a decoded Go value — accurate, and useless to whoever typed the
// command.
func TestTemplateErrorNamesTheCause(t *testing.T) {
	_, err := execute(t, helmChart, ".metadata.name")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "template") {
		t.Errorf("error should name the template as the cause, got %q", msg)
	}
	if strings.Contains(msg, "map[string]interface") {
		t.Errorf("error should not dump a Go type at the user, got %q", msg)
	}
}

// TestTemplateErrorCarriesTheLine: this is the one parse-error family that
// arrives without a line number, because yaml.v3 fails it at the "map used as
// a map key" check rather than as a syntax error with a position.
func TestTemplateErrorCarriesTheLine(t *testing.T) {
	out, err := execute(t, helmChart, "-o", "json", ".metadata.name")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var je struct {
		Error string `json:"error"`
		Kind  string `json:"kind"`
		Line  int    `json:"line"`
	}
	if uerr := json.Unmarshal([]byte(out), &je); uerr != nil {
		t.Fatalf("stderr is not the JSON error shape: %v\n%s", uerr, out)
	}
	if je.Kind != "parse" {
		t.Errorf("kind = %q, want parse", je.Kind)
	}
	if je.Line != 4 {
		t.Errorf("line = %d, want 4 (the first {{ )", je.Line)
	}
}

func TestTemplateErrorStaysExitTwo(t *testing.T) {
	_, err := execute(t, helmChart, ".metadata.name")
	if got := exitCode(err, io.Discard); got != 2 {
		t.Fatalf("templated YAML is still a parse failure: want exit 2, got %d", got)
	}
}

// TestTemplateErrorOnControlFlow covers the other lineless shape: a
// {{- if }} block fails with "did not find expected node content", a
// different underlying error from the value case above.
func TestTemplateErrorOnControlFlow(t *testing.T) {
	in := "{{- if .Values.enabled }}\na: 1\n{{- end }}\n"
	_, err := execute(t, in, ".a")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Errorf("a control-flow directive should be named too, got %q", err.Error())
	}
}

// TestQuotedTemplateIsNotHijacked: a quoted directive is perfectly valid
// YAML and must keep working — the detection must not fire on the mere
// presence of {{.
func TestQuotedTemplateIsNotHijacked(t *testing.T) {
	got, err := execute(t, "name: \"{{ .Values.name }}\"\n", "-o", "raw", ".name")
	if err != nil {
		t.Fatalf("a quoted template is valid YAML: %v", err)
	}
	if strings.TrimSpace(got) != "{{ .Values.name }}" {
		t.Fatalf("got %q", got)
	}
}

// TestUnrelatedParseErrorIsNotHijacked: a document that happens to contain a
// quoted {{ but breaks for an ordinary reason must keep its own message and
// its own line number, not be blamed on templating.
func TestUnrelatedParseErrorIsNotHijacked(t *testing.T) {
	in := "name: \"{{ x }}\"\nb: [1, 2\n"
	_, err := execute(t, in, ".name")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "template") {
		t.Errorf("an unrelated syntax error must not be blamed on templating, got %q", msg)
	}
	if !strings.Contains(msg, "line 1") {
		t.Errorf("the original line number must survive, got %q", msg)
	}
}

// TestTemplateErrorOnEditPath: the editing commands decode through
// decodeNodes rather than decodeDocs, so they need the same treatment.
func TestTemplateErrorOnEditPath(t *testing.T) {
	_, err := execute(t, helmChart, "set", ".metadata.name", "x")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Errorf("set should name the template too, got %q", err.Error())
	}
	if got := exitCode(err, io.Discard); got != 2 {
		t.Errorf("want exit 2, got %d", got)
	}
}
