package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/authstore"
	"agentbox/internal/run"
)

// envFileHost is imagerefHost with a pinned non-interactive test name.
func envFileHost(t *testing.T, out, errb *bytes.Buffer, extra map[string]string) run.Host {
	t.Helper()
	env := map[string]string{"AGENTBOX_TEST_NAME": t.Name()}
	for k, v := range extra {
		env[k] = v
	}
	return imagerefHost(out, errb, env)
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvFileValuesReachEngineEnv(t *testing.T) {
	ws := t.TempDir()
	envFile := writeEnvFile(t, "APP_SECRET=s3cr3t\n")
	var out, errb bytes.Buffer
	host := envFileHost(t, &out, &errb, nil)
	e := &fakeEngine{responses: map[string]string{
		"~ps":            "",
		"~image inspect": "sha256:abc",
		"~run":           "",
	}}
	o := baseRunOpts(ws)
	o.DryRun = false
	o.Ephemeral = true
	o.EngineInstance = e
	o.EnvFiles = []string{envFile}
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	found := false
	for _, kv := range e.env {
		if kv == "APP_SECRET=s3cr3t" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("engine child env missing APP_SECRET=s3cr3t: %v", e.env)
	}
}

func TestEnvFileReservedKeyRefusedBeforeRun(t *testing.T) {
	ws := t.TempDir()
	envFile := writeEnvFile(t, "DOCKER_HOST=tcp://evil\n")
	var out, errb bytes.Buffer
	host := envFileHost(t, &out, &errb, nil)
	e := &fakeEngine{responses: map[string]string{
		"~ps": "",
	}}
	o := baseRunOpts(ws)
	o.DryRun = false
	o.Ephemeral = true
	o.EngineInstance = e
	o.EnvFiles = []string{envFile}
	if code := run.Run(o, host); code != 1 {
		t.Fatalf("exit = %d, want 1; stderr = %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "refusing reserved key") {
		t.Errorf("stderr should surface the reserved-key refusal: %q", errb.String())
	}
	for _, call := range e.calls {
		if strings.Contains(call, "docker run") || strings.Contains(call, "~run") {
			t.Errorf("reserved env file must fail before any run call: %v", e.calls)
		}
	}
}

func TestStoredCredentialSuppressesPreflightNotice(t *testing.T) {
	ws := t.TempDir()
	userRoot := t.TempDir()
	if _, err := authstore.Store(userRoot, "claude", []byte(`{"token":"x"}`)); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	host := envFileHost(t, &out, &errb, map[string]string{"AGENTBOX_USER_DIR": userRoot})
	e := &fakeEngine{responses: map[string]string{
		"~ps":            "",
		"~image inspect": "sha256:abc",
		"~run":           "",
	}}
	o := baseRunOpts(ws)
	o.DryRun = false
	o.Agent = "claude"
	o.Ephemeral = true
	o.EngineInstance = e
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if strings.Contains(errb.String(), "unset for harness") {
		t.Errorf("stored credential must suppress the preflight notice: %q", errb.String())
	}
	if !strings.Contains(errb.String(), "using credentials copied with") {
		t.Errorf("stored credential notice missing: %q", errb.String())
	}
}
