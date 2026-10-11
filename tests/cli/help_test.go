package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"agentbox/internal/cli"
)

// Every flag documented in PRD FR-1 must appear in --help (AC 19).
var documentedFlags = []string{
	"--agent", "-A",
	"--image",
	"--dry-run",
	"--quiet", "-q",
	"--ro",
	"--force",
	"--ephemeral",
	"--state-dir",
	"--env", "-e",
	"--env-file",
	"--credentials",
	"--net",
	"--cpus",
	"--memory",
	"--pids-limit",
	"--user",
	"--engine",
	"--name",
	"--keep",
	"--workdir-path",
	"--config",
	"--build",
	"--no-check",
	"--no-tty",
	"--no-user-config",
	"--version",
	"--help", "-h",
	"--",
}

func TestHelpListsEveryDocumentedFlag(t *testing.T) {
	h := cli.Help()
	for _, f := range documentedFlags {
		if !strings.Contains(h, f) {
			t.Errorf("help output is missing %q", f)
		}
	}
	for _, cmd := range []string{"agents", "auth", "doctor", "state", "version"} {
		if !strings.Contains(h, cmd) {
			t.Errorf("help output is missing subcommand %q", cmd)
		}
	}
	if !strings.Contains(h, "workspace") {
		t.Error("help output is missing the workspace positional")
	}
}

func runMain(t *testing.T, args []string, deps cli.Deps) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Main(args, cli.IO{In: strings.NewReader(""), Out: &out, Err: &errb, IsTTY: false}, "1.2.3", deps)
	return code, out.String(), errb.String()
}

func TestMainHelpExitsZero(t *testing.T) {
	code, out, _ := runMain(t, []string{"--help"}, cli.Deps{})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "usage: agentbox") {
		t.Errorf("help must print usage, got %q", out)
	}
}

func TestMainUnknownFlagExitsTwo(t *testing.T) {
	code, _, errOut := runMain(t, []string{"--bogus"}, cli.Deps{})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, `unknown flag "--bogus"`) {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(errOut, `"--"`) {
		t.Errorf("stderr must suggest --: %q", errOut)
	}
	if !strings.HasPrefix(errOut, "agentbox: ") || !strings.Contains(errOut, "  \u2192 ") {
		t.Errorf("stderr must use one-line + remedy style: %q", errOut)
	}
}

func TestMainVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		code, out, _ := runMain(t, args, cli.Deps{})
		if code != 0 {
			t.Fatalf("args %v: exit = %d, want 0", args, code)
		}
		if !strings.Contains(out, "1.2.3") {
			t.Errorf("args %v: version output = %q", args, out)
		}
	}
}

func TestMainDispatchesRunWithParsedOptions(t *testing.T) {
	var got *cli.Options
	code, out, _ := runMain(t, []string{"--agent", "shell", "--ro", "/tmp/ws", "--", "-c", "pwd"}, cli.Deps{
		Run: func(o *cli.Options, io cli.IO) int {
			got = o
			return 7
		},
	})
	if code != 7 {
		t.Fatalf("exit = %d, want propagated 7", code)
	}
	if got == nil {
		t.Fatal("Run was not called")
	}
	if got.Agent != "shell" || !got.ReadOnly || got.Workspace != "/tmp/ws" || len(got.AgentArgs) != 2 {
		t.Errorf("Run got %+v", got)
	}
	if out != "" {
		t.Errorf("success must print nothing on stdout, got %q", out)
	}
}

func TestMainDispatchesSubcommands(t *testing.T) {
	called := ""
	deps := cli.Deps{
		Agents: func(o *cli.Options, io cli.IO) int { called = "agents"; return 0 },
		Auth:   func(o *cli.Options, io cli.IO) int { called = "auth:" + o.SubArgs[0]; return 0 },
		Doctor: func(o *cli.Options, io cli.IO) int { called = "doctor"; return 0 },
		State:  func(o *cli.Options, io cli.IO) int { called = "state:" + strings.Join(o.SubArgs, ","); return 0 },
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"agents"}, "agents"},
		{[]string{"auth", "claude"}, "auth:claude"},
		{[]string{"doctor"}, "doctor"},
		{[]string{"state", "ls"}, "state:ls"},
		{[]string{"state", "rm", "p1"}, "state:rm,p1"},
		{[]string{"state", "gc"}, "state:gc"},
	} {
		called = ""
		if code, _, _ := runMain(t, tc.args, deps); code != 0 {
			t.Fatalf("args %v: exit %d", tc.args, code)
		}
		if called != tc.want {
			t.Errorf("args %v: called %q, want %q", tc.args, called, tc.want)
		}
	}
}

func TestMainUsageErrorRender(t *testing.T) {
	code, _, errOut := runMain(t, []string{"--net", "bogus"}, cli.Deps{})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "bridge, none, host") {
		t.Errorf("stderr = %q", errOut)
	}
}
