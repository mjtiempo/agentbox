package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/cli"
)

type runCall struct {
	harness  string
	args     []string
	stateDir string
}

func quickstartDeps(t *testing.T, home, userRoot string, isTTY bool, in string, run func(string, []string, string, cli.IO) int) (cli.QuickstartDeps, *[]runCall) {
	t.Helper()
	calls := &[]runCall{}
	if run == nil {
		run = func(string, []string, string, cli.IO) int { return 0 }
	}
	deps := cli.QuickstartDeps{
		Env: func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		},
		UserRoot: userRoot,
		Runner:   func(string, ...string) ([]byte, error) { return nil, errors.New("no host export") },
		IsTTY:    isTTY,
		RunHarness: func(harness string, args []string, stateDir string, io cli.IO) int {
			*calls = append(*calls, runCall{harness, args, stateDir})
			return run(harness, args, stateDir, io)
		},
		TempDir: func() (string, error) { return os.MkdirTemp(t.TempDir(), "qs-") },
	}
	return deps, calls
}

func seedHostPiAuth(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte(`{"token":"fixture-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runQuickstart(t *testing.T, args []string, deps cli.QuickstartDeps, in string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Quickstart(&cli.Options{SubArgs: args}, cli.IO{
		In: strings.NewReader(in), Out: &out, Err: &errb,
	}, "0.1.0", deps)
	return code, out.String(), errb.String()
}

func TestQuickstartAdoptsHostCredential(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	seedHostPiAuth(t, home)
	deps, calls := quickstartDeps(t, home, userRoot, false, "", nil)

	code, out, errOut := runQuickstart(t, nil, deps, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(userRoot, "credentials", "pi.json")); err != nil {
		t.Fatalf("credential not stored: %v", err)
	}
	if !strings.Contains(out, "auth: adopted credentials from") || !strings.Contains(out, "setup complete") {
		t.Errorf("output = %q", out)
	}
	if len(*calls) != 0 {
		t.Errorf("no login run expected, got %+v", *calls)
	}
	if strings.Contains(out, "fixture-secret") {
		t.Fatal("quickstart must never print credential values")
	}
}

func TestQuickstartCopiesHostConfig(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	seedHostPiAuth(t, home)
	skills := filepath.Join(home, ".pi", "agent", "skills", "probe")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "SKILL.md"), []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, _ := quickstartDeps(t, home, userRoot, false, "", nil)

	code, out, _ := runQuickstart(t, nil, deps, "")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(userRoot, "agents", "pi", "skills", "probe", "SKILL.md")); err != nil {
		t.Errorf("config not copied: %v", err)
	}
	if !strings.Contains(out, "config: copied") {
		t.Errorf("output = %q", out)
	}
}

func TestQuickstartInteractiveLoginAdoptsCredential(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	run := func(harness string, args []string, stateDir string, _ cli.IO) int {
		// Simulate pi's TUI /login writing its credential file.
		dir := filepath.Join(stateDir, "pi")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"token":"from-login"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return 0
	}
	deps, calls := quickstartDeps(t, home, userRoot, true, "y\n", run)

	code, out, errOut := runQuickstart(t, nil, deps, "y\n")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if len(*calls) != 1 || (*calls)[0].harness != "pi" || (*calls)[0].stateDir == "" {
		t.Fatalf("login run = %+v", *calls)
	}
	stored := filepath.Join(userRoot, "credentials", "pi.json")
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatalf("credential not adopted: %v", err)
	}
	if string(data) != `{"token":"from-login"}` {
		t.Errorf("stored = %q", data)
	}
	if !strings.Contains(out, "adopted the credential created at") {
		t.Errorf("output = %q", out)
	}
	// The scratch state dir is cleaned up.
	if _, err := os.Stat((*calls)[0].stateDir); !os.IsNotExist(err) {
		t.Errorf("scratch state dir left behind: %v", err)
	}
}

func TestQuickstartDeclinesLogin(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	deps, calls := quickstartDeps(t, home, userRoot, true, "n\n", nil)

	code, out, _ := runQuickstart(t, nil, deps, "n\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(*calls) != 0 {
		t.Errorf("declining must not run the harness: %+v", *calls)
	}
	if !strings.Contains(out, "auth: skipped") {
		t.Errorf("output = %q", out)
	}
}

func TestQuickstartNonTTYDoesNotPrompt(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	deps, calls := quickstartDeps(t, home, userRoot, false, "", nil)

	code, out, _ := runQuickstart(t, nil, deps, "")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(*calls) != 0 {
		t.Errorf("non-TTY must not run an interactive login: %+v", *calls)
	}
	if !strings.Contains(out, "re-run `agentbox quickstart pi` in a terminal") {
		t.Errorf("output = %q", out)
	}
}

func TestQuickstartOpencodeExportsAfterLogin(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	run := func(harness string, args []string, _ string, io cli.IO) int {
		if strings.Join(args, " ") == "auth export" {
			io.Out.Write([]byte(`{"list":[{"id":"1"}]}`))
		}
		return 0
	}
	deps, calls := quickstartDeps(t, home, userRoot, true, "y\n", run)

	code, out, errOut := runQuickstart(t, []string{"opencode"}, deps, "y\n")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected login + export runs, got %+v", *calls)
	}
	if strings.Join((*calls)[0].args, " ") != "auth login" {
		t.Errorf("first run args = %v", (*calls)[0].args)
	}
	if strings.Join((*calls)[1].args, " ") != "auth export" {
		t.Errorf("second run args = %v", (*calls)[1].args)
	}
	data, err := os.ReadFile(filepath.Join(userRoot, "credentials", "opencode.json"))
	if err != nil {
		t.Fatalf("exported credential not stored: %v", err)
	}
	if !strings.Contains(string(data), `"list"`) {
		t.Errorf("stored = %q", data)
	}
	if !strings.Contains(out, "exported and adopted") {
		t.Errorf("output = %q", out)
	}
}

func TestQuickstartSkipsCopyWhenLinked(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	hostCfg := filepath.Join(home, ".pi", "agent")
	seedHostPiAuth(t, home)
	if err := os.MkdirAll(filepath.Join(userRoot, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hostCfg, filepath.Join(userRoot, "agents", "pi")); err != nil {
		t.Fatal(err)
	}
	deps, _ := quickstartDeps(t, home, userRoot, false, "", nil)

	code, out, _ := runQuickstart(t, nil, deps, "")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "is linked to") {
		t.Errorf("output = %q", out)
	}
	if strings.Contains(out, "config: copied") {
		t.Errorf("linked config must not be copied onto: %q", out)
	}
	// The host directory is untouched (no copies created inside it).
	entries, _ := os.ReadDir(hostCfg)
	if len(entries) != 1 { // just auth.json
		t.Errorf("host config gained entries: %v", entries)
	}
}

func TestQuickstartShellNeedsNoSetup(t *testing.T) {
	deps, _ := quickstartDeps(t, t.TempDir(), t.TempDir(), true, "y\n", nil)
	code, out, _ := runQuickstart(t, []string{"shell"}, deps, "y\n")
	if code != 0 || !strings.Contains(out, "needs no setup") {
		t.Fatalf("exit = %d output = %q", code, out)
	}
}

func TestQuickstartUnknownHarness(t *testing.T) {
	deps, _ := quickstartDeps(t, t.TempDir(), t.TempDir(), false, "", nil)
	code, _, errOut := runQuickstart(t, []string{"bogus"}, deps, "")
	if code != 2 || !strings.Contains(errOut, "pi") {
		t.Fatalf("exit = %d stderr = %q", code, errOut)
	}
}
