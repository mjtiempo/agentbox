package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/auth"
	"agentbox/internal/authstore"
	"agentbox/internal/cli"
	"agentbox/internal/engine"
)

type fakeEngine struct {
	responses map[string]string
	calls     []string
}

func (f *fakeEngine) Name() string { return "docker" }
func (f *fakeEngine) RunArgv(spec engine.RunSpec) []string {
	return append([]string{"docker", "run", spec.Image}, spec.Cmd...)
}
func (f *fakeEngine) DryRunArgv(engine.RunSpec, func(string) string) []string { return nil }
func (f *fakeEngine) Run(context.Context, []string) (int, error)              { return 0, nil }

func (f *fakeEngine) RunStream(_ context.Context, argv []string, stdio engine.Stdio) (int, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	out, ok := f.responses[key]
	if !ok {
		// "~substr" keys match any argv containing the substring.
		for pat, resp := range f.responses {
			if strings.HasPrefix(pat, "~") && strings.Contains(key, strings.TrimPrefix(pat, "~")) {
				out, ok = resp, true
				break
			}
		}
	}
	if !ok {
		return 1, nil
	}
	if w, ok := stdio.Out.(io.Writer); ok && w != nil {
		io.WriteString(w, out)
	}
	return 0, nil
}

func TestAuthSubcommandEnvDetection(t *testing.T) {
	var out, errb bytes.Buffer
	probes := auth.Probes{
		Getenv: func(k string) string {
			if k == "ANTHROPIC_API_KEY" {
				return "sk-secret-value"
			}
			return ""
		},
	}
	code := cli.Auth(&cli.Options{SubArgs: []string{"claude"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", cli.AuthDeps{Probes: probes})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "mechanism: environment") || !strings.Contains(got, "ANTHROPIC_API_KEY") {
		t.Errorf("output = %q", got)
	}
	if strings.Contains(got, "sk-secret-value") {
		t.Fatal("auth output must never contain secret values")
	}
}

func TestAuthSubcommandNone(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.Auth(&cli.Options{SubArgs: []string{"pi"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0",
		cli.AuthDeps{Probes: auth.Probes{Getenv: func(string) string { return "" }}})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "mechanism: none") {
		t.Errorf("output = %q", out.String())
	}
}

func TestAuthSubcommandUnknownHarness(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.Auth(&cli.Options{SubArgs: []string{"bogus"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", cli.AuthDeps{Probes: auth.Probes{}})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "pi") {
		t.Errorf("error should list valid harnesses: %q", errb.String())
	}
}

func TestStateLsSubcommand(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "agentbox-state-1000-aaaa1111\t1000\tapp-aaaa1111\t/home/me/src/app\n",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"ls"}}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{"agentbox-state-1000-aaaa1111", "app-aaaa1111", "/home/me/src/app"} {
		if !strings.Contains(got, want) {
			t.Errorf("ls output missing %q:\n%s", want, got)
		}
	}
}

func TestStateRmRequiresConfirmationInScripts(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "agentbox-state-1000-aaaa1111\t1000\tapp-aaaa1111\t/home/me/src/app\n",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"rm", "app-aaaa1111"}}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (confirmation required)", code)
	}
	if !strings.Contains(errb.String(), "--force") {
		t.Errorf("stderr must offer --force: %q", errb.String())
	}
	// No removal command must have been issued.
	for _, c := range e.calls {
		if strings.Contains(c, "volume rm") {
			t.Errorf("volume was removed without confirmation: %v", e.calls)
		}
	}
}

func TestStateRmForceRemoves(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "agentbox-state-1000-aaaa1111\t1000\tapp-aaaa1111\t/home/me/src/app\n",
		"docker volume rm agentbox-state-1000-aaaa1111": "agentbox-state-1000-aaaa1111\n",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"rm", "app-aaaa1111"}, Force: true}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "removed agentbox-state-1000-aaaa1111") {
		t.Errorf("output = %q", out.String())
	}
}

func TestStateRmUnknownProject(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"rm", "nope"}, Force: true}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "state ls") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestStateGcListsBeforeRemoving(t *testing.T) {
	// Regression: gc must never remove volumes before confirmation.
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "agentbox-state-1000-orphan111\t1000\tgone-orphan111\t/tmp/definitely-missing-path-xyz\n",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"gc"}}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (confirmation required)", code)
	}
	for _, c := range e.calls {
		if strings.Contains(c, "volume rm") {
			t.Errorf("gc removed volumes before confirmation: %v", e.calls)
		}
	}
	if !strings.Contains(errb.String(), "would remove: agentbox-state-1000-orphan111") {
		t.Errorf("gc must list candidates: %q", errb.String())
	}
}

func TestStateGcForceRemovesOrphans(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "agentbox-state-1000-orphan111\t1000\tgone-orphan111\t/tmp/definitely-missing-path-xyz\n",
		"docker volume rm agentbox-state-1000-orphan111": "agentbox-state-1000-orphan111\n",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"gc"}, Force: true}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "removed agentbox-state-1000-orphan111") {
		t.Errorf("output = %q", out.String())
	}
}

func TestStateGcNothingToCollect(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker volume ls --filter label=agentbox.uid=1000 --format {{.Name}}\t{{.Label \"agentbox.uid\"}}\t{{.Label \"agentbox.project\"}}\t{{.Label \"agentbox.path\"}}": "agentbox-state-1000-alive123\t1000\tapp-alive123\t/tmp\n",
	}}
	var out, errb bytes.Buffer
	code := cli.State(&cli.Options{SubArgs: []string{"gc"}}, cli.IO{Out: &out, Err: &errb}, e, 1000)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "nothing to collect") {
		t.Errorf("output = %q", out.String())
	}
}

func TestDoctorSubcommand(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{
		"docker version --format {{.Server.Version}}":    "29.8.2\n",
		"docker info --format {{json .SecurityOptions}}": `["name=seccomp","name=cgroupns"]`,
	}}
	var out, errb bytes.Buffer
	code := cli.Doctor(&cli.Options{NoCheck: true}, cli.IO{Out: &out, Err: &errb}, "0.1.0", e)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{"agentbox: 0.1.0", "engine:   docker 29.8.2", "update:   skipped (--no-check)", "image:"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorSubcommandNoEngine(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{}}
	var out, errb bytes.Buffer
	code := cli.Doctor(&cli.Options{NoCheck: true}, cli.IO{Out: &out, Err: &errb}, "0.1.0", e)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "unreachable") {
		t.Errorf("output = %q", out.String())
	}
}

func TestAuthCopyAndReset(t *testing.T) {
	home := t.TempDir()
	userRoot := t.TempDir()
	hostCred := filepath.Join(home, ".pi", "agent", "auth.json")
	if err := os.MkdirAll(filepath.Dir(hostCred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostCred, []byte(`{"token":"fixture-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	deps := cli.AuthDeps{
		UserRoot: userRoot,
		Env:      env,
		Probes: auth.Probes{
			Getenv:           env,
			StoredCredential: func() (string, bool) { _, p, ok, _ := authstore.Load(userRoot, "pi"); return p, ok },
		},
	}

	// copy
	var out, errb bytes.Buffer
	code := cli.Auth(&cli.Options{SubArgs: []string{"copy", "pi"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", deps)
	if code != 0 {
		t.Fatalf("copy exit = %d, stderr = %s", code, errb.String())
	}
	stored := filepath.Join(userRoot, "credentials", "pi.json")
	st, err := os.Stat(stored)
	if err != nil {
		t.Fatalf("stored credential missing: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("stored mode = %o, want 600", st.Mode().Perm())
	}
	if strings.Contains(out.String(), "fixture-secret") {
		t.Fatal("auth copy must never print credential values")
	}
	if !strings.Contains(out.String(), "auth reset pi") {
		t.Errorf("copy output should mention how to remove it: %q", out.String())
	}

	// detection now reports the store
	out.Reset()
	errb.Reset()
	code = cli.Auth(&cli.Options{SubArgs: []string{"pi"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", deps)
	if code != 0 || !strings.Contains(out.String(), "mechanism: agentbox credential store") {
		t.Fatalf("detection = %q (exit %d)", out.String(), code)
	}

	// reset needs confirmation without a TTY
	out.Reset()
	errb.Reset()
	code = cli.Auth(&cli.Options{SubArgs: []string{"reset", "pi"}}, cli.IO{Out: &out, Err: &errb, IsTTY: false}, "0.1.0", deps)
	if code != 2 || !strings.Contains(errb.String(), "--force") {
		t.Fatalf("reset without confirmation: exit %d, stderr %q", code, errb.String())
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("reset must not run without confirmation: %v", err)
	}

	// reset with --force
	out.Reset()
	errb.Reset()
	code = cli.Auth(&cli.Options{SubArgs: []string{"reset", "pi"}, Force: true}, cli.IO{Out: &out, Err: &errb}, "0.1.0", deps)
	if code != 0 {
		t.Fatalf("reset exit = %d, stderr = %s", code, errb.String())
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Errorf("stored credential still present: %v", err)
	}
}

func TestAuthCopyOpencodeUsesExport(t *testing.T) {
	userRoot := t.TempDir()
	var ran string
	deps := cli.AuthDeps{
		UserRoot: userRoot,
		Env:      func(string) string { return "" },
		Runner: func(name string, args ...string) ([]byte, error) {
			ran = name + " " + strings.Join(args, " ")
			return []byte(`{"list":[{"id":"1"}]}`), nil
		},
	}
	var out, errb bytes.Buffer
	code := cli.Auth(&cli.Options{SubArgs: []string{"copy", "opencode"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", deps)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if ran != "opencode auth export" {
		t.Errorf("ran %q, want `opencode auth export`", ran)
	}
	if _, err := os.Stat(filepath.Join(userRoot, "credentials", "opencode.json")); err != nil {
		t.Errorf("opencode credential not stored: %v", err)
	}
}

func TestAuthCopyMissingHostCredential(t *testing.T) {
	deps := cli.AuthDeps{
		UserRoot: t.TempDir(),
		Env: func(k string) string {
			if k == "HOME" {
				return t.TempDir()
			}
			return ""
		},
	}
	var out, errb bytes.Buffer
	code := cli.Auth(&cli.Options{SubArgs: []string{"copy", "pi"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", deps)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "log in on the host first") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestAuthCopyShellHasNoTarget(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.Auth(&cli.Options{SubArgs: []string{"copy", "shell"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", cli.AuthDeps{UserRoot: t.TempDir()})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "no credential target") {
		t.Errorf("stderr = %q", errb.String())
	}
}
