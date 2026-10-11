package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"agentbox/internal/cli"
)

func TestAgentsListsBuiltins(t *testing.T) {
	var out bytes.Buffer
	code := cli.Agents(&cli.Options{}, cli.IO{Out: &out, Err: &out}, "0.1.0")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	for _, want := range []string{
		"pi", "shell", "claude", "opencode",
		"agentbox-pi:0.1.0-pi1.1.0",
		"agentbox-base:0.1.0",
		"agentbox-claude:0.1.0-claude2.1.296",
		"agentbox-opencode:0.1.0-opencode2.0.26",
		"ANTHROPIC_API_KEY",
		"image:<ref>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("agents output missing %q:\n%s", want, got)
		}
	}
}
