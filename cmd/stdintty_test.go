package cmd

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestRefuseInteractiveStdinSkipsNonFileReaders: execute's test double is a
// strings.Reader, never *os.File, so every existing test that reads from
// "stdin" keeps working untouched — the check only has an opinion about a
// real stdin.
func TestRefuseInteractiveStdinSkipsNonFileReaders(t *testing.T) {
	c := NewRootCommand()
	c.SetIn(strings.NewReader("a: 1\n"))
	if err := refuseInteractiveStdin(c); err != nil {
		t.Fatalf("a non-*os.File reader must never be refused: %v", err)
	}
}

// TestRefuseInteractiveStdinOnACharDevice: a character device standing in
// for a terminal (the portable part of "is this a TTY" the stdlib alone can
// answer, no x/term dependency needed) is refused rather than left to block
// forever on an input that will never arrive.
func TestRefuseInteractiveStdinOnACharDevice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("character-device detection for NUL is not exercised on windows here")
	}
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Skip("no /dev/null to stand in for a character device")
	}
	defer f.Close()

	c := NewRootCommand()
	c.SetIn(f)
	err = refuseInteractiveStdin(c)
	if err == nil {
		t.Fatal("want a refusal for a character-device stdin, got nil")
	}
	if codeFor(err) != 3 {
		t.Errorf("codeFor = %d, want 3 (usage)", codeFor(err))
	}
}

// TestGetOnCharDeviceStdinDoesNotHang: the end-to-end case A8 is about — a
// misspelled invocation that drops the file argument reads from stdin
// instead of erroring, and on a TTY that hangs forever rather than failing
// fast. A character device stands in for the terminal here too.
func TestGetOnCharDeviceStdinDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("character-device detection for NUL is not exercised on windows here")
	}
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Skip("no /dev/null to stand in for a character device")
	}
	defer f.Close()

	c := NewRootCommand()
	c.SetIn(f)
	c.SetArgs([]string{".a"})
	err = c.Execute()
	if err == nil {
		t.Fatal("want a refusal, got nil")
	}
	if codeFor(err) != 3 {
		t.Errorf("codeFor = %d, want 3 (usage): %v", codeFor(err), err)
	}
}
