package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/authstore"
	"agentbox/internal/run"
)

func imagerefHost(out, errb *bytes.Buffer, env map[string]string) run.Host {
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

func baseRunOpts(ws string) run.Options {
	return run.Options{
		Version: "0.1.0", Workspace: ws, Engine: "docker",
		DryRun: true, Net: "bridge", CPUs: "2", Memory: "4g", PidsLimit: 512,
	}
}

func TestImageRefDryRun(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-imageref"})
	o := baseRunOpts(ws)
	o.Agent = "image:alpine:3.20"
	o.Ephemeral = true
	o.AgentArgs = []string{"echo", "hi"}
	code := run.Run(o, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if strings.Contains(got, "--entrypoint") {
		t.Errorf("image:<ref> must not force the agentbox entrypoint:\n%s", got)
	}
	if !strings.Contains(got, "--user\n1000:1000") {
		t.Errorf("image:<ref> must pass --user host uid:gid (FR-9 P1):\n%s", got)
	}
	if !strings.Contains(got, "alpine:3.20\necho\nhi") {
		t.Errorf("image:<ref> cmd must be the user args:\n%s", got)
	}
	if strings.Contains(got, ":/state") {
		t.Errorf("--ephemeral image:<ref> must not mount state:\n%s", got)
	}
}

func TestImageRefRequiresEphemeralOrStateDir(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-imageref"})
	o := baseRunOpts(ws)
	o.Agent = "image:alpine:3.20"
	code := run.Run(o, host)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "--ephemeral") || !strings.Contains(errb.String(), "--state-dir") {
		t.Errorf("remedy must mention --ephemeral and --state-dir: %q", errb.String())
	}
}

func TestImageRefWithImageConflict(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-imageref"})
	o := baseRunOpts(ws)
	o.Agent = "image:alpine:3.20"
	o.Image = "other/img:1"
	o.Ephemeral = true
	code := run.Run(o, host)
	if code != 2 || !strings.Contains(errb.String(), "cannot be combined with --image") {
		t.Fatalf("exit = %d, stderr = %q", code, errb.String())
	}
}

func TestBuildWithImageConflict(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-buildimage"})
	e := &fakeEngine{responses: map[string]string{
		"~ps": "",
	}}
	o := baseRunOpts(ws)
	o.DryRun = false
	o.Build = true
	o.Image = "other/img:1"
	o.Ephemeral = true
	o.EngineInstance = e
	code := run.Run(o, host)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr = %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "--build cannot be combined with --image") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestUserDryRunKeepsEntrypoint(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-user"})
	o := baseRunOpts(ws)
	o.Ephemeral = true
	o.User = "1000:1000"
	code := run.Run(o, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "--user\n1000:1000") {
		t.Errorf("--user missing:\n%s", got)
	}
	if !strings.Contains(got, "--entrypoint\n/entrypoint.sh") {
		t.Errorf("built-in adapters keep the entrypoint so it can skip the drop:\n%s", got)
	}
}

func TestUserRootWarns(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-user"})
	o := baseRunOpts(ws)
	o.Ephemeral = true
	o.User = "0:0"
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errb.String(), "--user 0") {
		t.Errorf("--user 0 must warn: %q", errb.String())
	}
}

func TestStateDirMount(t *testing.T) {
	ws := t.TempDir()
	sd := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-statedir"})
	o := baseRunOpts(ws)
	o.StateDir = sd
	code := run.Run(o, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "-v\n"+sd+":/state") {
		t.Errorf("--state-dir must bind-mount the directory:\n%s", got)
	}
	if strings.Contains(got, "agentbox-state-") {
		t.Errorf("--state-dir must not use a named volume:\n%s", got)
	}
}

func TestCredentialsMountDryRun(t *testing.T) {
	ws := t.TempDir()
	cred := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(cred, []byte(`{"token":"super-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-cred"})
	o := baseRunOpts(ws)
	o.Agent = "pi"
	o.Credentials = []string{cred}
	code := run.Run(o, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "-v\n"+cred+":/state/pi/auth.json:ro") {
		t.Errorf("credentials must mount read-only at the adapter target:\n%s", got)
	}
	if !strings.Contains(got, "AGENTBOX_CRED_FILE=/state/pi/auth.json") {
		t.Errorf("credentials must tell the entrypoint the target for the chown:\n%s", got)
	}
	if strings.Contains(got, "super-secret") {
		t.Fatalf("credential contents must never appear in dry-run:\n%s", got)
	}
}

func TestOpencodeCredentialsAutoImportEnv(t *testing.T) {
	ws := t.TempDir()
	cred := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(cred, []byte(`{"list":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-occred"})
	o := baseRunOpts(ws)
	o.Agent = "opencode"
	o.Credentials = []string{cred}
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "AGENTBOX_CRED_IMPORT=auth import /state/opencode/credentials.json --standalone") {
		t.Errorf("opencode must pass the import argv to the entrypoint:\n%s", got)
	}
	if strings.Contains(got, "AGENTBOX_CRED_IMPORT") == false {
		t.Errorf("missing import env:\n%s", got)
	}
	// pi reads the file directly: no import step.
	var out2, errb2 bytes.Buffer
	host2 := imagerefHost(&out2, &errb2, map[string]string{"AGENTBOX_TEST_NAME": "iso-occred"})
	o2 := baseRunOpts(ws)
	o2.Agent = "pi"
	o2.Credentials = []string{cred}
	if code := run.Run(o2, host2); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out2.String(), "AGENTBOX_CRED_IMPORT") {
		t.Errorf("pi must not use the import hook:\n%s", out2.String())
	}
}

func TestStoredCredentialAutoMount(t *testing.T) {
	ws := t.TempDir()
	userRoot := t.TempDir()
	if _, err := authstore.Store(userRoot, "pi", []byte(`{"token":"fixture"}`)); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{
		"AGENTBOX_TEST_NAME": "iso-stored",
		"AGENTBOX_USER_DIR":  userRoot,
	})
	o := baseRunOpts(ws)
	o.Agent = "pi"
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	got := out.String()
	stored := filepath.Join(userRoot, "credentials", "pi.json")
	if !strings.Contains(got, "-v\n"+stored+":/state/pi/auth.json:ro") {
		t.Errorf("stored credential must be mounted read-only:\n%s", got)
	}
	if !strings.Contains(errb.String(), "using credentials copied with `agentbox auth copy pi`") {
		t.Errorf("notice missing: %q", errb.String())
	}
	// Explicit --credentials wins over the store.
	explicit := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(explicit, []byte(`{"token":"explicit"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out2, errb2 bytes.Buffer
	host2 := imagerefHost(&out2, &errb2, map[string]string{
		"AGENTBOX_TEST_NAME": "iso-stored",
		"AGENTBOX_USER_DIR":  userRoot,
	})
	o2 := baseRunOpts(ws)
	o2.Agent = "pi"
	o2.Credentials = []string{explicit}
	if code := run.Run(o2, host2); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out2.String(), explicit+":/state/pi/auth.json:ro") {
		t.Errorf("explicit --credentials must win:\n%s", out2.String())
	}
	if strings.Contains(out2.String(), stored) {
		t.Errorf("stored credential must not be mounted when --credentials is given:\n%s", out2.String())
	}
}

// Podman's `volume create` fails when the volume exists; an existing volume
// must not abort the run (Docker's create is idempotent).
func TestExistingStateVolumeIsTolerated(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-podvol"})
	e := &fakeEngine{responses: map[string]string{
		"~info":           "29.8.2",
		"~image inspect":  "sha256:abc",
		"~volume inspect": "", // the volume already exists
		"~ps":             "",
		"~run":            "",
	}}
	// No "~volume create" response: the fake fails it, like podman does.
	o := baseRunOpts(ws)
	o.DryRun = false
	o.EngineInstance = e
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	for _, c := range e.calls {
		if strings.Contains(c, "volume inspect") {
			return // exercised the fallback
		}
	}
	t.Errorf("expected a volume inspect fallback, calls = %v", e.calls)
}

func TestCredentialsMissingFile(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-cred"})
	o := baseRunOpts(ws)
	o.Credentials = []string{"/nope/missing.json"}
	code := run.Run(o, host)
	if code != 1 || !strings.Contains(errb.String(), "does not exist") {
		t.Fatalf("exit = %d, stderr = %q", code, errb.String())
	}
}

func TestCredentialsUnsupportedAdapter(t *testing.T) {
	ws := t.TempDir()
	cred := filepath.Join(t.TempDir(), "auth.json")
	os.WriteFile(cred, []byte("{}"), 0o600)
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-cred"})
	o := baseRunOpts(ws)
	o.Agent = "shell"
	o.Credentials = []string{cred}
	code := run.Run(o, host)
	if code != 1 || !strings.Contains(errb.String(), "no credential file target") {
		t.Fatalf("exit = %d, stderr = %q", code, errb.String())
	}
}

func TestPullOnFirstRunOnly(t *testing.T) {
	ws := t.TempDir()

	// Image missing: a pull happens once, with a one-line notice.
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-pull"})
	e := &fakeEngine{responses: map[string]string{
		"~info":          "29.8.2",
		"~volume create": "",
		"~pull":          "",
		"~ps":            "",
		"~run":           "",
	}}
	o := baseRunOpts(ws)
	o.DryRun = false
	o.EngineInstance = e
	code := run.Run(o, host)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "pulling image") {
		t.Errorf("first run must print a pull notice: %q", errb.String())
	}
	pulls := 0
	for _, c := range e.calls {
		if strings.HasPrefix(c, "docker pull") {
			pulls++
		}
	}
	if pulls != 1 {
		t.Errorf("pull count = %d, calls = %v", pulls, e.calls)
	}

	// Image present: no pull, no notice.
	var out2, errb2 bytes.Buffer
	host2 := imagerefHost(&out2, &errb2, map[string]string{"AGENTBOX_TEST_NAME": "iso-pull"})
	e2 := &fakeEngine{responses: map[string]string{
		"~info":          "29.8.2",
		"~volume create": "",
		"~image inspect": "sha256:abc",
		"~ps":            "",
		"~run":           "",
	}}
	o2 := baseRunOpts(ws)
	o2.DryRun = false
	o2.EngineInstance = e2
	if code := run.Run(o2, host2); code != 0 {
		t.Fatalf("second run exit = %d, stderr = %s", code, errb2.String())
	}
	if strings.Contains(errb2.String(), "pulling image") {
		t.Errorf("subsequent runs must be silent: %q", errb2.String())
	}
	for _, c := range e2.calls {
		if strings.HasPrefix(c, "docker pull") {
			t.Errorf("no pull expected: %v", e2.calls)
		}
	}
}

func TestCredentialPreflightNotice(t *testing.T) {
	ws := t.TempDir()
	// Fresh volume (inspect fails) + no env key -> notice fires.
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-preflight"})
	e := &fakeEngine{responses: map[string]string{
		"~info":          "29.8.2",
		"~volume create": "",
		"~image inspect": "sha256:abc",
		"~ps":            "",
		"~run":           "",
	}}
	o := baseRunOpts(ws)
	o.DryRun = false
	o.Agent = "claude"
	o.EngineInstance = e
	if code := run.Run(o, host); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "ANTHROPIC_API_KEY and ANTHROPIC_OAUTH_TOKEN are unset") {
		t.Errorf("preflight notice missing: %q", errb.String())
	}

	// Env key present -> no notice.
	var out2, errb2 bytes.Buffer
	host2 := imagerefHost(&out2, &errb2, map[string]string{
		"AGENTBOX_TEST_NAME": "iso-preflight",
		"ANTHROPIC_API_KEY":  "fixture-not-a-real-key",
	})
	o2 := baseRunOpts(ws)
	o2.DryRun = false
	o2.Agent = "claude"
	o2.EngineInstance = e
	if code := run.Run(o2, host2); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(errb2.String(), "are unset for harness") {
		t.Errorf("notice must not fire when a key is set: %q", errb2.String())
	}
}

func TestEphemeralWithStateDirConflict(t *testing.T) {
	ws := t.TempDir()
	var out, errb bytes.Buffer
	host := imagerefHost(&out, &errb, map[string]string{"AGENTBOX_TEST_NAME": "iso-conflict"})
	o := baseRunOpts(ws)
	o.Ephemeral = true
	o.StateDir = t.TempDir()
	code := run.Run(o, host)
	if code != 2 || !strings.Contains(errb.String(), "--ephemeral cannot be combined with --state-dir") {
		t.Fatalf("exit = %d, stderr = %q", code, errb.String())
	}
}
