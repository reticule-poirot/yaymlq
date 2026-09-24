package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// ciDoc mirrors the file #137 was measured on: a mapping of job names, where
// a one-character typo used to cost a whole second invocation to recover.
const ciDoc = `
jobs:
  changes: {}
  govulncheck: {}
  hygiene: {}
  lint: {}
  test: {}
`

// wideDoc has more keys than any error should print.
func wideDoc() string {
	var b strings.Builder
	b.WriteString("m:\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "  k%02d: %d\n", i, i)
	}
	return b.String()
}

// TestNotFoundSuggestsTheNearestKey is the whole point: the retry becomes a
// fix, without a second call to find out what was there.
func TestNotFoundSuggestsTheNearestKey(t *testing.T) {
	_, err := execute(t, ciDoc, ".jobs.tset.steps")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), `did you mean "test"?`) {
		t.Errorf("error does not suggest the nearest key: %v", err)
	}
}

// TestNotFoundListsKeysWhenNothingIsClose: with no plausible typo to point
// at, the keys themselves are still the answer to "what should I have asked
// for", and they are already in hand.
func TestNotFoundListsKeysWhenNothingIsClose(t *testing.T) {
	_, err := execute(t, ciDoc, ".jobs.replicas")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "available keys: changes, govulncheck, hygiene, lint, test") {
		t.Errorf("error does not list the available keys: %v", err)
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("suggested something for a key that isn't close: %v", err)
	}
}

// TestNotFoundCapsTheKeyList: a mapping with 500 keys must not dump 500 keys
// into an error — that inverts the saving this is meant to deliver.
func TestNotFoundCapsTheKeyList(t *testing.T) {
	_, err := execute(t, wideDoc(), ".m.nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "k10") || strings.Contains(msg, "k11") {
		t.Errorf("want the first 10 keys and no more: %v", err)
	}
	if !strings.Contains(msg, "+20 more") {
		t.Errorf("error does not say how many keys it left out: %v", err)
	}
}

// TestMaxSuggestionsControlsTheCap, including 0 for no cap at all — the same
// sentinel --max-bytes already uses.
func TestMaxSuggestionsControlsTheCap(t *testing.T) {
	_, err := execute(t, wideDoc(), "--max-suggestions", "3", ".m.nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if msg := err.Error(); !strings.Contains(msg, "k03") || strings.Contains(msg, "k04") {
		t.Errorf("--max-suggestions 3 did not cap at 3: %v", err)
	}
	if !strings.Contains(err.Error(), "+27 more") {
		t.Errorf("remainder count is wrong: %v", err)
	}

	_, err = execute(t, wideDoc(), "--max-suggestions", "0", ".m.nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "k30") {
		t.Errorf("--max-suggestions 0 should list every key: %v", err)
	}
	if strings.Contains(err.Error(), "more") {
		t.Errorf("uncapped list should not mention a remainder: %v", err)
	}
}

// TestNotFoundJSONCarriesAvailableAsData: the structured form gets the keys
// as an array, not a sentence — the same reason #35 put `path` in the object.
func TestNotFoundJSONCarriesAvailableAsData(t *testing.T) {
	got, err := execute(t, ciDoc, "-o", "json", ".jobs.tset")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var je struct {
		Error          string   `json:"error"`
		Kind           string   `json:"kind"`
		Path           string   `json:"path"`
		Available      []string `json:"available"`
		AvailableTotal int      `json:"availableTotal"`
		Suggestion     string   `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &je); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, got)
	}
	if je.Kind != "no-match" || je.Path != "jobs.tset" {
		t.Errorf("kind/path wrong: %+v", je)
	}
	if strings.Join(je.Available, ",") != "changes,govulncheck,hygiene,lint,test" {
		t.Errorf("available = %q", je.Available)
	}
	if je.Suggestion != "test" {
		t.Errorf("suggestion = %q, want %q", je.Suggestion, "test")
	}
	if je.AvailableTotal != 0 {
		t.Errorf("availableTotal = %d, want it omitted when nothing was cut", je.AvailableTotal)
	}
	// The array is the data; repeating it in the message would make a
	// consumer parse prose it already has structurally.
	if strings.Contains(je.Error, "did you mean") || strings.Contains(je.Error, "available") {
		t.Errorf("JSON error text was annotated: %q", je.Error)
	}
}

// TestNotFoundJSONReportsTheTotalWhenCapped so a consumer can tell "these
// are all the keys" from "these are the first ten".
func TestNotFoundJSONReportsTheTotalWhenCapped(t *testing.T) {
	got, err := execute(t, wideDoc(), "-o", "json", ".m.nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var je struct {
		Available      []string `json:"available"`
		AvailableTotal int      `json:"availableTotal"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &je); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, got)
	}
	if len(je.Available) != 10 || je.AvailableTotal != 30 {
		t.Errorf("available has %d of %d, want 10 of 30", len(je.Available), je.AvailableTotal)
	}
}

// TestNotFoundSaysNothingWhenThereIsNothingToSay: an out-of-range index, a
// key into a list and a scalar in the way all already explain themselves.
func TestNotFoundSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	for _, expr := range []string{".jobs[0]", ".jobs.test.steps"} {
		_, err := execute(t, ciDoc, expr)
		if err == nil {
			t.Errorf("%s: want an error, got nil", expr)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "available keys") || strings.Contains(msg, "did you mean") {
			t.Errorf("%s: unexpected suggestion: %v", expr, err)
		}
	}
}

// TestSoftMissesStaySilent: -e/-q/--default treat a miss as control flow, and
// a suggestion must not leak into them.
func TestSoftMissesStaySilent(t *testing.T) {
	cases := [][]string{
		{"-e", ".jobs.tset"},
		{"-q", ".jobs.tset"},
		{"--default", "x", ".jobs.tset"},
		{"-o", "json", "-e", ".jobs.tset"},
	}
	for _, args := range cases {
		got, err := execute(t, ciDoc, args...)
		if strings.Contains(got, "did you mean") || strings.Contains(got, "available") {
			t.Errorf("%v leaked a suggestion into the output: %q", args, got)
		}
		if err != nil && (strings.Contains(err.Error(), "did you mean") || strings.Contains(err.Error(), "available")) {
			t.Errorf("%v leaked a suggestion into the error: %v", args, err)
		}
	}
}

// TestNotFoundSuggestionsOnInspectCommands: keys/len/type resolve a path the
// same way and fail the same way, so they recover the same way.
func TestNotFoundSuggestionsOnInspectCommands(t *testing.T) {
	for _, verb := range []string{"keys", "len", "type"} {
		_, err := execute(t, ciDoc, verb, ".jobs.tset")
		if err == nil {
			t.Errorf("%s: want an error, got nil", verb)
			continue
		}
		if !strings.Contains(err.Error(), `did you mean "test"?`) {
			t.Errorf("%s: no suggestion: %v", verb, err)
		}
	}
}

// TestNotFoundOnAnEmptyMappingSaysSo rather than printing "available keys: ".
func TestNotFoundOnAnEmptyMappingSaysSo(t *testing.T) {
	_, err := execute(t, "m: {}\n", ".m.nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "the mapping has no keys") {
		t.Errorf("unhelpful error for an empty mapping: %v", err)
	}
}

// TestSuggestionSearchesEveryKeyNotJustTheListedOnes: the cap is about how
// much output an error is allowed to produce, not about how hard it looks.
// A typo of the 29th key of 30 still has an obvious answer.
func TestSuggestionSearchesEveryKeyNotJustTheListedOnes(t *testing.T) {
	_, err := execute(t, wideDoc(), ".m.k29x")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), `did you mean "k29"?`) {
		t.Errorf("suggestion missed a key past the display cap: %v", err)
	}

	got, err := execute(t, wideDoc(), "-o", "json", ".m.k29x")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var je struct {
		Available  []string `json:"available"`
		Suggestion string   `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &je); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, got)
	}
	if je.Suggestion != "k29" {
		t.Errorf("suggestion = %q, want %q", je.Suggestion, "k29")
	}
	if len(je.Available) != 10 {
		t.Errorf("available should still be capped, got %d keys", len(je.Available))
	}
}

// TestNegativeMaxSuggestionsIsUsageError, for the reason --max-bytes rejects
// one: silently reading a negative cap as "unlimited" hides whatever
// computed it.
func TestNegativeMaxSuggestionsIsUsageError(t *testing.T) {
	wantExit(t, ciDoc, 3, "--max-suggestions", "-1", ".jobs.test")
	wantExit(t, ciDoc, 3, "keys", "--max-suggestions", "-1", ".jobs")
	// It is checked even when the query would have succeeded, and even when
	// no error is ever produced for it to cap.
	wantExit(t, ciDoc, 3, "--max-suggestions", "-1", "-q", ".jobs.test")
}

// editMisses are the invocations that fail on a key the document doesn't
// have. set is absent on purpose: it creates the key instead of failing, so
// there is no error to annotate (#164).
var editMisses = [][]string{
	{"delete", ".jobs.tset"},
	{"rename", ".jobs.tset", "foo"},
	{"append", ".jobs.tset", "1"},
}

// TestEditingCommandsSuggestTheNearestKey: a read miss has told you what was
// there since #137, and an edit miss resolving the same path the same way
// should not be the one place you still have to go and look.
func TestEditingCommandsSuggestTheNearestKey(t *testing.T) {
	for _, args := range editMisses {
		_, err := execute(t, ciDoc, args...)
		if err == nil {
			t.Errorf("%v: want an error, got nil", args)
			continue
		}
		if !strings.Contains(err.Error(), `did you mean "test"?`) {
			t.Errorf("%v: no suggestion: %v", args, err)
		}
		if !strings.Contains(err.Error(), "no such key") {
			t.Errorf("%v: original message lost: %v", args, err)
		}
	}
}

// TestApplySuggestsTheNearestKey keeps the line prefix, which is how a
// caller finds the offending op in a batch.
func TestApplySuggestsTheNearestKey(t *testing.T) {
	f := writeScript(t, t.TempDir(), "delete .jobs.lint\ndelete .jobs.tset\n")
	_, err := execute(t, ciDoc, "apply", "-f", f)
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error no longer names the failing op's line: %v", err)
	}
	if !strings.Contains(err.Error(), `did you mean "test"?`) {
		t.Errorf("no suggestion: %v", err)
	}
}

// TestEditingCommandsListKeysWhenNothingIsClose mirrors get's behaviour.
func TestEditingCommandsListKeysWhenNothingIsClose(t *testing.T) {
	_, err := execute(t, ciDoc, "delete", ".jobs.replicas")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "available keys: changes, govulncheck, hygiene, lint, test") {
		t.Errorf("keys not listed: %v", err)
	}
}

// TestEditingCommandsHonourMaxSuggestions: one cap, one spelling, whichever
// half of the CLI produced the error.
func TestEditingCommandsHonourMaxSuggestions(t *testing.T) {
	_, err := execute(t, wideDoc(), "delete", "--max-suggestions", "3", ".m.nope")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if msg := err.Error(); !strings.Contains(msg, "k03") || strings.Contains(msg, "k04") {
		t.Errorf("--max-suggestions 3 did not cap at 3: %v", err)
	}
	if !strings.Contains(err.Error(), "+27 more") {
		t.Errorf("remainder count is wrong: %v", err)
	}
	wantExit(t, wideDoc(), 3, "delete", "--max-suggestions", "-1", ".m.nope")
}

// TestEditingCommandNonKeyErrorsAreUnchanged: an out-of-range index already
// says how long the list is.
func TestEditingCommandNonKeyErrorsAreUnchanged(t *testing.T) {
	_, err := execute(t, "list: [1, 2]\n", "delete", ".list[9]")
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if msg := err.Error(); strings.Contains(msg, "available keys") || strings.Contains(msg, "did you mean") {
		t.Errorf("unexpected suggestion: %v", err)
	}
}

// TestFailedEditWithASuggestionStillWritesNothing: the annotation happens on
// the way out of a failed mutate, which must not have changed the file.
func TestFailedEditWithASuggestionStillWritesNothing(t *testing.T) {
	const original = "jobs:\n  test: {x: 1}\n"
	f := writeTemp(t, original)
	_, err := execute(t, "", "delete", "-i", ".jobs.tset", f)
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	got, readErr := os.ReadFile(f)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("a failed edit rewrote the file: %q", got)
	}
}
