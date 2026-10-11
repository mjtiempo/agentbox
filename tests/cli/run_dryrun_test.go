package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"agentbox/internal/run"
	"agentbox/internal/tty"
)

func stubHost(out, errb *bytes.Buffer, env map[string]string) run.Host {
	return run.Host{
		Env:      func(k string) string { return env[k] },
		Environ:  []string{},
		UID:      1000,
		GID:      1000,
		PID:      4242,
		BootID:   "boot-test",
		LookPath: func(string) (string, error) { return "", errors.New("no engine in unit tests") },
		IsTTY:    false,
		In:       strings.NewReader(""),
		Out:      out,
		Err:      errb,
	}
}

func TestRunDryRunDockerGrammar(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-testbox"})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{
		"docker\nrun\n", "--rm", "--init", "--name\ncli-testbox",
		"--label\nagentbox.run=agentbox-test-run",
		"--cap-drop\nALL", "--cap-add\nCHOWN", "--security-opt\nno-new-privileges",
		"-v\n" + ws + ":/workspace:rw", "-v\nagentbox-state-1000-",
		"-e\nAGENTBOX_UID=1000", "--entrypoint\n/entrypoint.sh",
		"agentbox-pi:0.1.0-pi1.1.0", "pi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "--userns") {
		t.Errorf("docker must not pass --userns:\n%s", got)
	}
	if strings.Contains(got, "-it") {
		t.Errorf("non-TTY dry-run must omit -it:\n%s", got)
	}
	if !strings.Contains(errb.String(), "agentbox: pi @ /workspace") {
		t.Errorf("banner missing: %q", errb.String())
	}
}

func TestRunDryRunPodmanUserns(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-testbox"})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "podman",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "podman\nrun\n") {
		t.Errorf("podman grammar expected:\n%s", got)
	}
	if !strings.Contains(got, "--userns=keep-id") {
		t.Errorf("podman must pass --userns=keep-id (FR-9 P2):\n%s", got)
	}
}

func TestRunDryRunSecretsRedacted(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{
		"AGENTBOX_TEST_NAME": "cli-testbox",
		"ANTHROPIC_API_KEY":  "sk-super-secret",
	})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "ANTHROPIC_API_KEY=<set>") {
		t.Errorf("dry-run must show <set>:\n%s", got)
	}
	if strings.Contains(got, "sk-super-secret") {
		t.Fatalf("secret leaked into dry-run:\n%s", got)
	}
}

func TestRunDryRunEphemeralNoStateVolume(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-testbox"})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Ephemeral: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	if strings.Contains(got, ":/state") {
		t.Errorf("ephemeral dry-run must not mount /state:\n%s", got)
	}
	if !strings.Contains(got, "pi\n--no-session") {
		t.Errorf("ephemeral pi must get --no-session:\n%s", got)
	}
}

func TestRunDryRunForceEchoAlways(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-testbox"})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Force: true, Quiet: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errb.String(), "--force: workspace guardrail overridden") {
		t.Errorf("--force echo must survive --quiet: %q", errb.String())
	}
	if strings.Contains(errb.String(), "agentbox: pi @") {
		t.Errorf("--quiet must suppress the banner: %q", errb.String())
	}
}

func TestRunDryRunTTYIncludesIt(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-tty"})
	host.IsTTY = true
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "--rm\n--init\n-it\n") {
		t.Errorf("TTY run must pass -it right after --init:\n%s", got)
	}
}

func TestRunDryRunNoTTYFlagOmitsIt(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-tty"})
	host.IsTTY = true // terminal present, but --no-tty forces non-TTY
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, NoTTY: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out.String(), "-it") {
		t.Errorf("--no-tty must omit -it:\n%s", out.String())
	}
}

func TestRunTTYPassesTerm(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{
		"AGENTBOX_TEST_NAME": "cli-tty",
		"TERM":               "xterm-256color",
		"COLORTERM":          "truecolor",
	})
	host.IsTTY = true
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	for _, want := range []string{"-e\nTERM", "-e\nCOLORTERM"} {
		if !strings.Contains(got, want) {
			t.Errorf("TTY run must pass %q (bare, value from env):\n%s", want, got)
		}
	}

	// Non-TTY: no terminal vars.
	var out2, errb2 bytes.Buffer
	host2 := stubHost(&out2, &errb2, map[string]string{
		"AGENTBOX_TEST_NAME": "cli-tty",
		"TERM":               "xterm-256color",
	})
	if code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host2); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out2.String(), "-e\nTERM") {
		t.Errorf("non-TTY run must not pass TERM:\n%s", out2.String())
	}
}

func TestDefaultHostWiresTTY(t *testing.T) {
	// Regression: DefaultHost once left IsTTY false, so -it was never passed
	// and TUI harnesses exited immediately.
	if got, want := run.DefaultHost().IsTTY, tty.StdinIsTerminal(); got != want {
		t.Errorf("DefaultHost().IsTTY = %v, want %v", got, want)
	}
}

func TestRunDryRunWarmupCacheEnv(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-cache"})
	if code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "AGENTBOX_CACHE_DIR=/state/home/.cache/tmp") {
		t.Errorf("pi must tell the entrypoint where the warm-up cache lives:\n%s", got)
	}
	if strings.Contains(got, "AGENTBOX_QUIET") {
		t.Errorf("no quiet env without --quiet:\n%s", got)
	}

	// --quiet is forwarded to the entrypoint so the notice is suppressed.
	var out2, errb2 bytes.Buffer
	host2 := stubHost(&out2, &errb2, map[string]string{"AGENTBOX_TEST_NAME": "cli-cache"})
	if code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Quiet: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host2); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out2.String(), "AGENTBOX_QUIET=1") {
		t.Errorf("--quiet must reach the entrypoint:\n%s", out2.String())
	}

	// shell has no warm-up cache.
	var out3, errb3 bytes.Buffer
	host3 := stubHost(&out3, &errb3, map[string]string{"AGENTBOX_TEST_NAME": "cli-cache"})
	if code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "shell", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host3); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out3.String(), "AGENTBOX_CACHE_DIR") {
		t.Errorf("shell must not emit a warm-up cache dir:\n%s", out3.String())
	}
}

func TestRunDryRunUserFolderMounts(t *testing.T) {
	ws := t.TempDir()
	userRoot := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{
		"AGENTBOX_TEST_NAME": "cli-userdir",
		"AGENTBOX_USER_DIR":  userRoot,
	})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{
		userRoot + "/agents/pi/skills:/state/pi/skills",
		userRoot + "/agents/pi/extensions:/state/pi/extensions",
		userRoot + "/agents/pi/themes:/state/pi/themes",
		"AGENTBOX_CONFIG_DIR=/state/pi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run missing %q:\n%s", want, got)
		}
	}

	// --no-user-config disables the mounts.
	var out2, errb2 bytes.Buffer
	host2 := stubHost(&out2, &errb2, map[string]string{
		"AGENTBOX_TEST_NAME": "cli-userdir",
		"AGENTBOX_USER_DIR":  userRoot,
	})
	if code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "pi", Engine: "docker",
		DryRun: true, NoUserConfig: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host2); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out2.String(), "/agents/pi/skills") {
		t.Errorf("--no-user-config must disable the user folder:\n%s", out2.String())
	}
}

func TestRunUsageConflicts(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		name string
		opts run.Options
		want string
	}{
		{"ephemeral+state-dir", run.Options{Ephemeral: true, StateDir: "/tmp/x"}, "--ephemeral cannot be combined with --state-dir"},
		{"imageref+image", run.Options{Agent: "image:alpine", Image: "other"}, "cannot be combined with --image"},
	}
	for _, tc := range cases {
		var out, errb bytes.Buffer
		host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-testbox"})
		o := tc.opts
		o.Version, o.Workspace, o.Engine = "0.1.0", ws, "docker"
		o.DryRun, o.Net, o.CPUs, o.Memory, o.PidsLimit = true, "bridge", "2", "4g", 512
		code := run.Run(o, host)
		if code != 2 {
			t.Errorf("%s: exit = %d, want 2", tc.name, code)
		}
		if !strings.Contains(errb.String(), tc.want) {
			t.Errorf("%s: stderr = %q", tc.name, errb.String())
		}
	}
}

func TestRunUnknownAgentListsNames(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := stubHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "cli-testbox"})
	code := run.Run(run.Options{
		Version: "0.1.0", Workspace: ws, Agent: "bogus", Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}, host)
	if code == 0 {
		t.Fatal("unknown agent must fail")
	}
	for _, name := range []string{"pi", "shell", "claude", "opencode"} {
		if !strings.Contains(errb.String(), name) {
			t.Errorf("error must list %q: %q", name, errb.String())
		}
	}
}
