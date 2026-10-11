package tty_test

import (
	"os"
	"testing"

	"agentbox/internal/tty"
)

func TestPipeIsNotTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if tty.IsTerminal(r.Fd()) {
		t.Error("a pipe must not be detected as a terminal")
	}
}

func TestDevNullIsNotTerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if tty.IsTerminal(f.Fd()) {
		t.Error("/dev/null must not be detected as a terminal")
	}
}
