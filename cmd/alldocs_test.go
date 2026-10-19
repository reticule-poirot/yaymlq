package cmd

import (
	"strings"
	"testing"
)

// A ConfigMap, then a Deployment: the ordinary shape of a Kubernetes manifest,
// and the one --all-docs exists for. Only the second document has .spec.
const heterogeneousStream = "kind: ConfigMap\n" +
	"data: {}\n" +
	"---\n" +
	"kind: Deployment\n" +
	"spec:\n" +
	"  replicas: 2\n"

// Same two documents the other way round, which is what showed the old
// behavior was not merely strict but order-dependent: it printed document 0's
// match and *then* failed.
const heterogeneousStreamReversed = "kind: Deployment\n" +
	"spec:\n" +
	"  replicas: 2\n" +
	"---\n" +
	"kind: ConfigMap\n" +
	"data: {}\n"

// TestGetAllDocsCollectsMatchesAcrossAHeterogeneousStream guards #175:
// --all-docs selects documents, it does not assert that every one matches. A
// document without the path is a document without the path, not a failed
// query.
func TestGetAllDocsCollectsMatchesAcrossAHeterogeneousStream(t *testing.T) {
	got, err := execute(t, heterogeneousStream, "--all-docs", "-o", "raw", ".spec.replicas")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "2\n" {
		t.Fatalf("want the Deployment's replicas, got %q", got)
	}
}

// TestGetAllDocsIsOrderIndependent pins the part that made the old behavior
// indefensible either way: results already written to stdout plus a failure
// exit, with which documents made it out depending on stream order.
func TestGetAllDocsIsOrderIndependent(t *testing.T) {
	first, err := execute(t, heterogeneousStream, "--all-docs", "-o", "raw", ".spec.replicas")
	if err != nil {
		t.Fatalf("execute (ConfigMap first): %v", err)
	}
	second, err := execute(t, heterogeneousStreamReversed, "--all-docs", "-o", "raw", ".spec.replicas")
	if err != nil {
		t.Fatalf("execute (Deployment first): %v", err)
	}
	if first != second {
		t.Fatalf("output depends on document order: %q vs %q", first, second)
	}
}

// TestGetAllDocsFailsWhenNoDocumentMatches keeps the other half of the
// contract: --all-docs still fails when the path is nowhere in the stream, and
// the error keeps #137's recovery facts from the first document that missed.
func TestGetAllDocsFailsWhenNoDocumentMatches(t *testing.T) {
	out, err := execute(t, heterogeneousStream, "--all-docs", "-o", "raw", ".nope")
	if err == nil {
		t.Fatalf("want an error when no document matches, got output %q", out)
	}
	if code := codeFor(err); code != 1 {
		t.Errorf("want exit 1 for a stream-wide miss, got %d", code)
	}
	if msg := annotateNotFound(err, defaultMaxSuggestions).Error(); !strings.Contains(msg, "available keys") {
		t.Errorf("want the available keys on a stream-wide miss, got %q", msg)
	}
}

// TestGetAllDocsMatchesEveryDocumentThatHasThePath checks the collecting half
// with more than one match, so "collect" isn't satisfied by stopping at the
// first.
func TestGetAllDocsMatchesEveryDocumentThatHasThePath(t *testing.T) {
	in := "kind: A\n---\nkind: B\n---\nkind: C\n"
	got, err := execute(t, in, "--all-docs", "-o", "raw", ".kind")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "A\nB\nC\n" {
		t.Fatalf("want every document's kind, got %q", got)
	}
}

// TestGetAllDocsSkipsAWrongTypeInTheWay covers the rest of the not-found
// family: every resolution failure in query wraps ErrNotFound, including an
// index out of range and a scalar where a mapping was expected. On a
// heterogeneous stream all of them mean the same thing — this document doesn't
// have it.
func TestGetAllDocsSkipsAWrongTypeInTheWay(t *testing.T) {
	in := "spec: a-string\n---\nspec:\n  replicas: 2\n"
	got, err := execute(t, in, "--all-docs", "-o", "raw", ".spec.replicas")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "2\n" {
		t.Fatalf("want the one document that resolves, got %q", got)
	}
}

// TestGetSingleDocMissIsStillAnError is the guard: without --all-docs nothing
// changes, since there is no other document the path could have been in.
func TestGetSingleDocMissIsStillAnError(t *testing.T) {
	if out, err := execute(t, heterogeneousStream, "-o", "raw", ".spec.replicas"); err == nil {
		t.Fatalf("want an error for a miss in the selected document, got %q", out)
	}
	if out, err := execute(t, heterogeneousStream, "--doc", "0", "-o", "raw", ".spec.replicas"); err == nil {
		t.Fatalf("want an error for a miss in --doc 0, got %q", out)
	}
}

// TestPathsAllDocsJSONOnAHeterogeneousStream is the recipe #159 shipped and
// #175 broke: a listing to feed into an apply script. It has to name only the
// documents that matched, with their real indices.
func TestPathsAllDocsJSONOnAHeterogeneousStream(t *testing.T) {
	got, err := execute(t, heterogeneousStream, "--paths", "--all-docs", "-o", "json", ".spec.replicas")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{"doc":1,"path":"spec.replicas"}` + "\n"
	if got != want {
		t.Fatalf("want only the matching document's path:\nwant: %q\ngot:  %q", want, got)
	}
}

// TestGetAllDocsWithDefaultStillFillsPerDocument keeps --default's existing
// meaning: it stands in for a missing value, per document, which is a
// different question from whether a miss is an error.
func TestGetAllDocsWithDefaultStillFillsPerDocument(t *testing.T) {
	got, err := execute(t, heterogeneousStream, "--all-docs", "-o", "raw", "--default", "none", ".spec.replicas")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "none\n2\n" {
		t.Fatalf("want the default for the ConfigMap and the real value for the Deployment, got %q", got)
	}
}

// TestKeysAllDocsCollectsAcrossAStream covers the inspect verbs, which had no
// notion of a soft miss at all.
func TestKeysAllDocsCollectsAcrossAStream(t *testing.T) {
	got, err := execute(t, heterogeneousStream, "keys", "--all-docs", ".spec")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "replicas\n" {
		t.Fatalf("want the Deployment's spec keys, got %q", got)
	}
}

func TestLenAndTypeAllDocsCollectAcrossAStream(t *testing.T) {
	for _, tc := range []struct{ verb, want string }{
		{"len", "1\n"},
		{"type", "object\n"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			got, err := execute(t, heterogeneousStream, tc.verb, "--all-docs", ".spec")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// TestInspectAllDocsFailsWhenNoDocumentMatches mirrors the get case: the
// inspect verbs still fail when nothing in the stream resolves.
func TestInspectAllDocsFailsWhenNoDocumentMatches(t *testing.T) {
	if out, err := execute(t, heterogeneousStream, "keys", "--all-docs", ".nope"); err == nil {
		t.Fatalf("want an error when no document matches, got %q", out)
	}
}

// TestLenAllDocsStillFailsOnATypeError draws the boundary the fix depends on:
// a path that *resolved* and then turned out to be the wrong kind of thing is
// not a missing path, and must stay an error even under --all-docs. Otherwise
// `len` over a stream would silently skip documents it genuinely can't answer
// for.
func TestLenAllDocsStillFailsOnATypeError(t *testing.T) {
	in := "kind: ConfigMap\n---\nkind: Deployment\nspec: 3\n"
	if out, err := execute(t, in, "len", "--all-docs", ".spec"); err == nil {
		t.Fatalf("want an error for len of a number, got %q", out)
	}
}

// TestGetAllDocsQuietAndExitStatusUnchanged checks the flags that were already
// soft still behave: -q and -e report on whether anything matched at all,
// which is now the same question --all-docs itself answers.
func TestGetAllDocsQuietAndExitStatusUnchanged(t *testing.T) {
	if _, err := execute(t, heterogeneousStream, "--all-docs", "-q", ".spec.replicas"); err != nil {
		t.Errorf("-q with a match in one document should exit 0, got %v", err)
	}
	if _, err := execute(t, heterogeneousStream, "--all-docs", "-q", ".nope"); err == nil {
		t.Error("-q with no match anywhere should exit 1")
	}
	if _, err := execute(t, heterogeneousStream, "--all-docs", "-e", ".spec.replicas"); err != nil {
		t.Errorf("-e with a match in one document should exit 0, got %v", err)
	}
}
