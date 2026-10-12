package cli_test

import (
	"reflect"
	"strings"
	"testing"

	"agentbox/internal/cli"
)

func mustParse(t *testing.T, args ...string) *cli.Options {
	t.Helper()
	opts, err := cli.Parse(args)
	if err != nil {
		t.Fatalf("Parse(%q): unexpected error: %v", args, err)
	}
	return opts
}

func wantUsageError(t *testing.T, args ...string) *cli.UsageError {
	t.Helper()
	opts, err := cli.Parse(args)
	if err == nil {
		t.Fatalf("Parse(%q): expected usage error, got opts %+v", args, opts)
	}
	ue, ok := err.(*cli.UsageError)
	if !ok {
		t.Fatalf("Parse(%q): expected *cli.UsageError, got %T: %v", args, err, err)
	}
	return ue
}

func TestDefaults(t *testing.T) {
	o := mustParse(t)
	if o.Workspace != "" {
		t.Errorf("Workspace default = %q, want empty (= $PWD)", o.Workspace)
	}
	if o.Agent != "pi" {
		t.Errorf("Agent default = %q, want pi", o.Agent)
	}
	if o.Net != "bridge" {
		t.Errorf("Net default = %q, want bridge", o.Net)
	}
	if o.CPUs != "2" {
		t.Errorf("CPUs default = %q, want 2", o.CPUs)
	}
	if o.Memory != "4g" {
		t.Errorf("Memory default = %q, want 4g", o.Memory)
	}
	if o.PidsLimit != 512 {
		t.Errorf("PidsLimit default = %d, want 512", o.PidsLimit)
	}
	if o.DryRun || o.Quiet || o.ReadOnly || o.Force || o.Ephemeral || o.Keep || o.Build || o.NoCheck || o.NoTTY {
		t.Errorf("boolean defaults must be false: %+v", o)
	}
	if o.Engine != "" {
		t.Errorf("Engine default = %q, want \"\" (auto-detect)", o.Engine)
	}
	if len(o.AgentArgs) != 0 || len(o.Env) != 0 {
		t.Errorf("slice defaults must be empty: %+v", o)
	}
}

func TestPositionalWorkspace(t *testing.T) {
	o := mustParse(t, "~/src/app")
	if o.Workspace != "~/src/app" {
		t.Errorf("Workspace = %q", o.Workspace)
	}
	if o.Agent != "pi" {
		t.Errorf("Agent = %q", o.Agent)
	}
}

func TestInterspersedParsing(t *testing.T) {
	// Flags before and after the positional workspace.
	o := mustParse(t, "--ro", "/src/app", "--agent", "shell", "--net", "none")
	if o.Workspace != "/src/app" || o.Agent != "shell" || o.Net != "none" || !o.ReadOnly {
		t.Errorf("interspersed parse wrong: %+v", o)
	}
	// Short flags, after positional.
	o = mustParse(t, "/src/app", "-A", "claude", "-q")
	if o.Workspace != "/src/app" || o.Agent != "claude" || !o.Quiet {
		t.Errorf("short/interspersed parse wrong: %+v", o)
	}
}

func TestFlagEqualsForms(t *testing.T) {
	o := mustParse(t, "--agent=shell", "--net=none", "--cpus=4", "--memory=1g", "--pids-limit=64", "--name=mybox")
	if o.Agent != "shell" || o.Net != "none" || o.CPUs != "4" || o.Memory != "1g" || o.PidsLimit != 64 || o.Name != "mybox" {
		t.Errorf("equals form wrong: %+v", o)
	}
	// Attached short value.
	o = mustParse(t, "-Aopencode", "-eFOO")
	if o.Agent != "opencode" {
		t.Errorf("-Aopencode: Agent = %q", o.Agent)
	}
	if !reflect.DeepEqual(o.Env, []string{"FOO"}) {
		t.Errorf("-eFOO: Env = %v", o.Env)
	}
}

func TestAllP0Flags(t *testing.T) {
	o := mustParse(t,
		"--agent", "pi", "--image", "custom/img:1",
		"--dry-run", "--quiet", "--ro", "--force", "--ephemeral",
		"--env", "FOO", "--net", "host",
		"--cpus", "8", "--memory", "16g", "--pids-limit", "2048",
	)
	if o.Agent != "pi" || o.Image != "custom/img:1" || !o.DryRun || !o.Quiet || !o.ReadOnly ||
		!o.Force || !o.Ephemeral || o.Net != "host" || o.CPUs != "8" || o.Memory != "16g" || o.PidsLimit != 2048 {
		t.Errorf("P0 flags wrong: %+v", o)
	}
	if !reflect.DeepEqual(o.Env, []string{"FOO"}) {
		t.Errorf("Env = %v", o.Env)
	}
}

func TestAllP1P2Flags(t *testing.T) {
	o := mustParse(t,
		"--state-dir", "/tmp/state",
		"--env-file", "a.env", "--env-file", "b.env",
		"--credentials", "/tmp/creds.json",
		"--user", "1000:1000",
		"--config", "/tmp/c.toml",
		"--no-check", "--no-tty",
		"--engine", "podman",
		"--name", "box1", "--keep", "--workdir-path", "/w", "--build",
	)
	if o.StateDir != "/tmp/state" || o.User != "1000:1000" || o.Config != "/tmp/c.toml" ||
		!o.NoCheck || !o.NoTTY || o.Engine != "podman" || o.Name != "box1" ||
		!o.Keep || o.WorkdirPath != "/w" || !o.Build {
		t.Errorf("P1/P2 flags wrong: %+v", o)
	}
	if !reflect.DeepEqual(o.EnvFiles, []string{"a.env", "b.env"}) {
		t.Errorf("EnvFiles = %v", o.EnvFiles)
	}
	if !reflect.DeepEqual(o.Credentials, []string{"/tmp/creds.json"}) {
		t.Errorf("Credentials = %v", o.Credentials)
	}
}

func TestRepeatableEnv(t *testing.T) {
	o := mustParse(t, "-e", "A", "--env", "B", "--env=C")
	if !reflect.DeepEqual(o.Env, []string{"A", "B", "C"}) {
		t.Errorf("Env = %v", o.Env)
	}
}

func TestDashDashBoundary(t *testing.T) {
	o := mustParse(t, "--agent", "shell", "--", "-c", "pwd", "--flag")
	if !reflect.DeepEqual(o.AgentArgs, []string{"-c", "pwd", "--flag"}) {
		t.Errorf("AgentArgs = %v", o.AgentArgs)
	}
	if o.Workspace != "" || o.Agent != "shell" {
		t.Errorf("boundary parse wrong: %+v", o)
	}
	// "--" itself is never forwarded.
	for _, a := range o.AgentArgs {
		if a == "--" {
			t.Errorf("`--` leaked into AgentArgs: %v", o.AgentArgs)
		}
	}
	// Args after -- are verbatim, even flag-looking ones.
	o = mustParse(t, "--", "--print", "explain this repo")
	if !reflect.DeepEqual(o.AgentArgs, []string{"--print", "explain this repo"}) {
		t.Errorf("AgentArgs = %v", o.AgentArgs)
	}
	// Empty tail after --.
	o = mustParse(t, "--")
	if len(o.AgentArgs) != 0 {
		t.Errorf("AgentArgs = %v, want empty", o.AgentArgs)
	}
}

func TestUnknownFlagErrorsWithHint(t *testing.T) {
	ue := wantUsageError(t, "--modell", "foo")
	if !strings.Contains(ue.Msg, `unknown flag "--modell"`) {
		t.Errorf("Msg = %q", ue.Msg)
	}
	if !strings.Contains(ue.Hint, `--`) {
		t.Errorf("Hint must suggest --: %q", ue.Hint)
	}
	// Unknown short flag too.
	ue = wantUsageError(t, "-Z")
	if !strings.Contains(ue.Msg, `unknown flag "-Z"`) {
		t.Errorf("Msg = %q", ue.Msg)
	}
}

func TestTooManyPositionals(t *testing.T) {
	ue := wantUsageError(t, "/a", "/b")
	if !strings.Contains(ue.Msg, "at most one workspace") {
		t.Errorf("Msg = %q", ue.Msg)
	}
}

func TestEnumValidation(t *testing.T) {
	ue := wantUsageError(t, "--net", "bogus")
	if !strings.Contains(ue.Msg, `--net`) || !strings.Contains(ue.Hint+ue.Msg, "bridge") {
		t.Errorf("net error must list valid values: msg=%q hint=%q", ue.Msg, ue.Hint)
	}
	ue = wantUsageError(t, "--engine", "rkt")
	if !strings.Contains(ue.Msg, "--engine") || !strings.Contains(ue.Hint+ue.Msg, "podman") {
		t.Errorf("engine error must list valid values: msg=%q hint=%q", ue.Msg, ue.Hint)
	}
}

func TestIntegerValidation(t *testing.T) {
	for _, args := range [][]string{{"--pids-limit", "abc"}, {"--pids-limit", "0"}, {"--pids-limit=-1"}} {
		ue := wantUsageError(t, args...)
		if !strings.Contains(ue.Msg, "--pids-limit") {
			t.Errorf("args %v: Msg = %q", args, ue.Msg)
		}
	}
}

func TestMissingFlagValue(t *testing.T) {
	for _, args := range [][]string{{"--agent"}, {"--net"}, {"-A"}, {"--env"}} {
		ue := wantUsageError(t, args...)
		if !strings.Contains(ue.Msg, "requires a value") && !strings.Contains(ue.Msg, "needs a value") {
			t.Errorf("args %v: Msg = %q", args, ue.Msg)
		}
	}
}

func TestHelpAndVersionFlags(t *testing.T) {
	o := mustParse(t, "--help")
	if !o.ShowHelp {
		t.Error("--help did not set ShowHelp")
	}
	o = mustParse(t, "-h")
	if !o.ShowHelp {
		t.Error("-h did not set ShowHelp")
	}
	o = mustParse(t, "--version")
	if !o.ShowVersion {
		t.Error("--version did not set ShowVersion")
	}
}

func TestSubcommands(t *testing.T) {
	cases := []struct {
		args    []string
		command string
		sub     []string
	}{
		{[]string{"agents"}, "agents", nil},
		{[]string{"auth", "claude"}, "auth", []string{"claude"}},
		{[]string{"doctor"}, "doctor", nil},
		{[]string{"doctor", "--no-check"}, "doctor", nil},
		{[]string{"state", "ls"}, "state", []string{"ls"}},
		{[]string{"state", "rm", "myproject"}, "state", []string{"rm", "myproject"}},
		{[]string{"state", "gc"}, "state", []string{"gc"}},
		{[]string{"state", "gc", "--force"}, "state", []string{"gc"}},
		{[]string{"version"}, "version", nil},
		{[]string{"config", "link"}, "config", []string{"link"}},
		{[]string{"config", "copy", "pi"}, "config", []string{"copy", "pi"}},
		{[]string{"config", "reset", "claude"}, "config", []string{"reset", "claude"}},
		{[]string{"config", "link", "--from", "/tmp/cfg"}, "config", []string{"link"}},
		{[]string{"auth", "copy", "pi"}, "auth", []string{"copy", "pi"}},
		{[]string{"auth", "reset", "opencode"}, "auth", []string{"reset", "opencode"}},
		{[]string{"quickstart"}, "quickstart", nil},
		{[]string{"quickstart", "claude"}, "quickstart", []string{"claude"}},
	}
	for _, tc := range cases {
		o := mustParse(t, tc.args...)
		if o.Command != tc.command {
			t.Errorf("args %v: Command = %q, want %q", tc.args, o.Command, tc.command)
		}
		if !reflect.DeepEqual(o.SubArgs, tc.sub) {
			t.Errorf("args %v: SubArgs = %v, want %v", tc.args, o.SubArgs, tc.sub)
		}
	}
}

func TestSubcommandValidation(t *testing.T) {
	// auth needs exactly one harness.
	ue := wantUsageError(t, "auth")
	if !strings.Contains(ue.Msg, "auth") || !strings.Contains(ue.Msg, "harness") {
		t.Errorf("auth without harness: Msg = %q", ue.Msg)
	}
	// state needs a known subcommand.
	ue = wantUsageError(t, "state")
	if !strings.Contains(ue.Msg, "state") || !(strings.Contains(ue.Hint+ue.Msg, "ls") || strings.Contains(ue.Hint+ue.Msg, "gc")) {
		t.Errorf("state without subcommand: Msg = %q Hint = %q", ue.Msg, ue.Hint)
	}
	ue = wantUsageError(t, "state", "bogus")
	if !strings.Contains(ue.Msg, "bogus") {
		t.Errorf("state bogus: Msg = %q", ue.Msg)
	}
	// state rm needs a project.
	ue = wantUsageError(t, "state", "rm")
	if !strings.Contains(ue.Msg, "project") {
		t.Errorf("state rm without project: Msg = %q", ue.Msg)
	}
	// auth copy|reset <harness> is accepted; unknown subcommands are not.
	ue = wantUsageError(t, "auth", "bogus", "pi")
	if !strings.Contains(ue.Msg, "bogus") {
		t.Errorf("auth bogus: Msg = %q", ue.Msg)
	}
	ue = wantUsageError(t, "auth", "copy")
	if !strings.Contains(ue.Msg, "harness") {
		t.Errorf("auth copy without harness: Msg = %q", ue.Msg)
	}
	// quickstart takes at most one harness.
	ue = wantUsageError(t, "quickstart", "pi", "claude")
	if !strings.Contains(ue.Msg, "at most one harness") {
		t.Errorf("quickstart two harnesses: Msg = %q", ue.Msg)
	}
	// config needs a known subcommand and rejects --from on reset.
	ue = wantUsageError(t, "config")
	if !strings.Contains(ue.Msg, "config") || !strings.Contains(ue.Hint+ue.Msg, "link") {
		t.Errorf("config without subcommand: Msg = %q Hint = %q", ue.Msg, ue.Hint)
	}
	ue = wantUsageError(t, "config", "bogus")
	if !strings.Contains(ue.Msg, "bogus") {
		t.Errorf("config bogus: Msg = %q", ue.Msg)
	}
	ue = wantUsageError(t, "config", "reset", "pi", "--from", "/x")
	if !strings.Contains(ue.Msg, "--from") {
		t.Errorf("config reset --from: Msg = %q", ue.Msg)
	}
}

func TestSubcommandEngineFlag(t *testing.T) {
	// --engine is read by state/doctor, so it must be accepted after a
	// subcommand as well as before it.
	o := mustParse(t, "state", "ls", "--engine", "podman")
	if o.Engine != "podman" {
		t.Errorf("state ls --engine podman: Engine = %q", o.Engine)
	}
	if o.Command != "state" || !reflect.DeepEqual(o.SubArgs, []string{"ls"}) {
		t.Errorf("state ls parse wrong: %+v", o)
	}
	o = mustParse(t, "doctor", "--engine=podman")
	if o.Engine != "podman" || o.Command != "doctor" {
		t.Errorf("doctor --engine=podman parse wrong: %+v", o)
	}
	// The global form before the subcommand keeps working.
	o = mustParse(t, "--engine", "docker", "state", "ls")
	if o.Engine != "docker" || o.Command != "state" {
		t.Errorf("--engine docker state ls parse wrong: %+v", o)
	}
	// Invalid value is a usage error with the shared validation text.
	for _, args := range [][]string{{"state", "ls", "--engine", "rkt"}, {"doctor", "--engine=rkt"}} {
		ue := wantUsageError(t, args...)
		if !strings.Contains(ue.Msg, `invalid --engine value "rkt" (valid: docker, podman)`) {
			t.Errorf("args %v: Msg = %q", args, ue.Msg)
		}
	}
	// A value-less --engine errors like the top-level form.
	ue := wantUsageError(t, "state", "ls", "--engine")
	if !strings.Contains(ue.Msg, "requires a value") {
		t.Errorf("value-less --engine: Msg = %q", ue.Msg)
	}
}

func TestConfigFromFlag(t *testing.T) {
	o := mustParse(t, "config", "link", "--from", "/tmp/cfg")
	if o.From != "/tmp/cfg" {
		t.Errorf("From = %q", o.From)
	}
	o = mustParse(t, "config", "copy", "--from=/tmp/cfg2")
	if o.From != "/tmp/cfg2" {
		t.Errorf("From = %q", o.From)
	}
}

func TestSubcommandNameNotConfusedWithWorkspace(t *testing.T) {
	// A positional that is not in first position is a workspace.
	o := mustParse(t, "--agent", "shell", "doctor")
	if o.Command != "doctor" {
		t.Errorf("subcommand after flags should dispatch: Command = %q", o.Command)
	}
	// A directory literally named "agents" can still be chosen explicitly via ./agents.
	o = mustParse(t, "./agents")
	if o.Workspace != "./agents" || o.Command != "" {
		t.Errorf("./agents parse wrong: %+v", o)
	}
}

func TestDoubleDashThenBoundaryWithSubcommand(t *testing.T) {
	// The `--` boundary is for the agent run; a subcommand never consumes it.
	o := mustParse(t, "auth", "claude")
	if o.Command != "auth" || !reflect.DeepEqual(o.SubArgs, []string{"claude"}) {
		t.Errorf("auth parse wrong: %+v", o)
	}
}
