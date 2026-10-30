package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// heldReader yields src, then blocks every further Read until release is
// closed, and only then reports EOF. It stands in for the thing #233 is about:
// a producer that has written a complete document and has not closed the pipe
// — `kubectl get -w -o yaml`, `tail -f`, an interactive terminal.
type heldReader struct {
	src     *strings.Reader
	release chan struct{}

	mu      sync.Mutex
	drained bool // src was read to its end, so the document is available
}

func (r *heldReader) Read(p []byte) (int, error) {
	if r.src.Len() > 0 {
		return r.src.Read(p)
	}
	r.mu.Lock()
	r.drained = true
	r.mu.Unlock()
	<-r.release
	return 0, io.EOF
}

func (r *heldReader) sawWholeDocument() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drained
}

// syncBuffer is a bytes.Buffer the test goroutine can measure while the
// command writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestAReadProducesNothingBeforeEOF is #233's claim as a test rather than as a
// sentence: a read verb buffers its whole input before emitting anything, so a
// pipe that stays open produces no output even once a complete document has
// arrived.
//
// This asserts the shape the docs now promise, which means it is also the
// tripwire for changing it: per-document streaming for the read verbs would
// fail here, and the failure points at the three places that say input is read
// to EOF. That is the point — the behavior and the promise move together.
func TestAReadProducesNothingBeforeEOF(t *testing.T) {
	held := &heldReader{src: strings.NewReader("kind: A\n"), release: make(chan struct{})}
	var out syncBuffer

	c := NewRootCommand()
	c.SetIn(held)
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"-o", "raw", ".kind", "-"})

	done := make(chan error, 1)
	go func() { done <- c.Execute() }()

	// Wait for the command to have consumed the whole document and parked on
	// the next Read. Polling for that state rather than sleeping a fixed
	// duration is what keeps this from being a timing test: the assertion
	// below only runs once the input is provably available to the command.
	deadline := time.Now().Add(2 * time.Second)
	for !held.sawWholeDocument() {
		if time.Now().After(deadline) {
			close(held.release)
			t.Fatal("the command never read the document")
		}
		time.Sleep(time.Millisecond)
	}

	if got := out.String(); got != "" {
		t.Errorf("want no output while the stream is open, got %q", got)
	}

	close(held.release)
	if err := <-done; err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := out.String(); got != "A\n" {
		t.Errorf("after EOF, want %q, got %q", "A\n", got)
	}
}

// TestAllDocsEmitsNothingUntilTheStreamCloses is the same property where it
// surprises most. --all-docs reads like "handle each document as it comes",
// and a caller piping a live stream into it sees nothing at all rather than
// the first document's answer.
func TestAllDocsEmitsNothingUntilTheStreamCloses(t *testing.T) {
	held := &heldReader{
		src:     strings.NewReader("kind: A\n---\nkind: B\n"),
		release: make(chan struct{}),
	}
	var out syncBuffer

	c := NewRootCommand()
	c.SetIn(held)
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"-o", "raw", "--all-docs", ".kind", "-"})

	done := make(chan error, 1)
	go func() { done <- c.Execute() }()

	deadline := time.Now().Add(2 * time.Second)
	for !held.sawWholeDocument() {
		if time.Now().After(deadline) {
			close(held.release)
			t.Fatal("the command never read the documents")
		}
		time.Sleep(time.Millisecond)
	}

	if got := out.String(); got != "" {
		t.Errorf("want no output for document 0 while the stream is open, got %q", got)
	}

	close(held.release)
	if err := <-done; err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := out.String(); got != "A\nB\n" {
		t.Errorf("after EOF, want both documents, got %q", got)
	}
}

// TestEverySurfaceSaysTheReadWaitsForEOF covers the three places a caller can
// learn this from. #233 was not that the buffering is wrong — it is
// deliberate, and --max-bytes bounds it — but that nothing said so, and the
// way it failed was a hang rather than a message.
//
// All three are checked together because they serve different readers and are
// edited at different times: --help is what a person reaches for, the README
// is what a newcomer reads, and SKILL.md is what an agent in another repo is
// told to read *instead of* --help (#155, #226). A promise kept in two of the
// three is the state this issue was filed about.
func TestEverySurfaceSaysTheReadWaitsForEOF(t *testing.T) {
	for _, s := range []struct {
		name string
		text func(*testing.T) string
	}{
		{"--help", func(*testing.T) string { return NewRootCommand().Long }},
		{"README.md", func(t *testing.T) string { return readFileForTest(t, "../README.md") }},
		{"SKILL.md", func(t *testing.T) string {
			return readFileForTest(t, "../contrib/claude-skill/SKILL.md")
		}},
	} {
		t.Run(s.name, func(t *testing.T) {
			if !strings.Contains(s.text(t), "EOF") {
				t.Errorf("%s never says the input is read to EOF, so a caller piping a live stream has nothing to go on", s.name)
			}
		})
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
