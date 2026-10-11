package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/cli"
)

func runWithConfig(t *testing.T, args []string, deps cli.Deps) (int, string, string, *cli.Options) {
	t.Helper()
	var out, errb bytes.Buffer
	var got *cli.Options
	if deps.Run == nil {
		deps.Run = func(o *cli.Options, _ cli.IO) int {
			got = o
			return 0
		}
	} else {
		inner := deps.Run
		deps.Run = func(o *cli.Options, io cli.IO) int {
			got = o
			return inner(o, io)
		}
	}
	code := cli.Main(args, cli.IO{In: strings.NewReader(""), Out: &out, Err: &errb}, "0.1.0", deps)
	return code, out.String(), errb.String(), got
}

func TestProjectConfigAppliesAndReportsProvenance(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "agentbox.toml"), []byte("cpus = \"4\"\nro = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut, got := runWithConfig(t, []string{ws, "--dry-run"}, cli.Deps{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if got == nil {
		t.Fatal("Run was not dispatched")
	}
	if got.CPUs != "4" {
		t.Errorf("CPUs = %q, want project value 4", got.CPUs)
	}
	if !got.ReadOnly {
		t.Error("ro = true from project config was not applied")
	}
	if !strings.Contains(errOut, "agentbox: config: "+filepath.Join(ws, "agentbox.toml")) {
		t.Errorf("provenance line missing: %q", errOut)
	}
}

func TestCLIWinsOverProjectConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "agentbox.toml"), []byte("cpus = \"4\"\n"), 0o644)
	code, _, _, got := runWithConfig(t, []string{ws, "--cpus", "8", "--dry-run"}, cli.Deps{})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got.CPUs != "8" {
		t.Errorf("CPUs = %q, CLI must win", got.CPUs)
	}
}

func TestProjectConfigPrecedenceOverUser(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	os.MkdirAll(filepath.Join(xdg, "agentbox"), 0o755)
	os.WriteFile(filepath.Join(xdg, "agentbox", "config.toml"), []byte("cpus = \"9\"\nmemory = \"1g\"\n"), 0o644)
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "agentbox.toml"), []byte("cpus = \"3\"\n"), 0o644)

	code, _, errOut, got := runWithConfig(t, []string{ws, "--dry-run"}, cli.Deps{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if got.CPUs != "3" {
		t.Errorf("CPUs = %q, project must beat user", got.CPUs)
	}
	if got.Memory != "1g" {
		t.Errorf("Memory = %q, user value must survive", got.Memory)
	}
	if !strings.Contains(errOut, "config: ") || strings.Count(errOut, "config: ") != 2 {
		t.Errorf("both provenance lines expected: %q", errOut)
	}
}

// AC 22: boundary-relevant project keys are rejected without being applied.
func TestProjectConfigRejectedKeysAC22(t *testing.T) {
	for _, key := range []string{"env", "image", "net", "state-dir", "credentials", "user", "name", "keep", "quiet"} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		ws := t.TempDir()
		var content string
		switch key {
		case "env", "credentials":
			content = key + ` = ["x"]` + "\n"
		case "keep", "quiet":
			content = key + " = true\n"
		default:
			content = key + ` = "x"` + "\n"
		}
		os.WriteFile(filepath.Join(ws, "agentbox.toml"), []byte(content), 0o644)

		code, _, errOut, got := runWithConfig(t, []string{ws, "--dry-run"}, cli.Deps{})
		if code == 0 {
			t.Errorf("key %q: expected nonzero exit", key)
		}
		if !strings.Contains(errOut, key) || !strings.Contains(errOut, "project config") {
			t.Errorf("key %q: stderr = %q", key, errOut)
		}
		if got != nil {
			t.Errorf("key %q: Run must not be dispatched", key)
		}
	}
}

func TestProjectConfigImageAgentRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "agentbox.toml"), []byte(`agent = "image:alpine"`+"\n"), 0o644)
	code, _, errOut, _ := runWithConfig(t, []string{ws, "--dry-run"}, cli.Deps{})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "image:<ref>") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestProvenancePrintedEvenWithQuiet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "agentbox.toml"), []byte("cpus = \"4\"\n"), 0o644)
	code, _, errOut, _ := runWithConfig(t, []string{ws, "--quiet", "--dry-run"}, cli.Deps{})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errOut, "agentbox: config: ") {
		t.Errorf("provenance must survive --quiet: %q", errOut)
	}
}

func TestExplicitConfigFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "custom.toml")
	os.WriteFile(cfg, []byte("agent = \"shell\"\n"), 0o644)
	code, _, errOut, got := runWithConfig(t, []string{ws, "--config", cfg, "--dry-run"}, cli.Deps{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if got.Agent != "shell" {
		t.Errorf("Agent = %q", got.Agent)
	}
	if !strings.Contains(errOut, "config: "+cfg) {
		t.Errorf("provenance = %q", errOut)
	}
}

func TestExplicitConfigMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	code, _, errOut, _ := runWithConfig(t, []string{ws, "--config", "/nope/missing.toml", "--dry-run"}, cli.Deps{})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "does not exist") {
		t.Errorf("stderr = %q", errOut)
	}
}
