package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
)

func testAdapter(t *testing.T, name string) adapter.Adapter {
	t.Helper()
	a, err := adapter.Get(name, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func baseInput(t *testing.T) SpecInput {
	t.Helper()
	return SpecInput{
		Adapter:   testAdapter(t, "pi"),
		Image:     "agentbox-pi:0.1.0-pi1.1.0",
		Naming:    Naming{Name: "agentbox-testbox", Project: "app-1a2b3c4d", RunID: "run-1", Owner: "42@boot"},
		Workspace: "/home/me/src/app",
		State:     &engine.Mount{Host: "agentbox-state-1000-1a2b3c4d", Container: "/state"},
		Net:       "bridge",
		CPUs:      "2",
		Memory:    "4g",
		PidsLimit: 512,
		Workdir:   "/workspace",
		UID:       1000,
		Env:       []engine.EnvVar{{Name: "AGENTBOX_UID", Value: "1000", Set: true}},
	}
}

func TestAssembleDefaults(t *testing.T) {
	spec := Assemble(baseInput(t))
	if spec.Name != "agentbox-testbox" || spec.Image != "agentbox-pi:0.1.0-pi1.1.0" {
		t.Errorf("name/image: %+v", spec)
	}
	if len(spec.Cmd) != 1 || spec.Cmd[0] != "pi" {
		t.Errorf("Cmd = %v", spec.Cmd)
	}
	if spec.Entrypoint != "/entrypoint.sh" {
		t.Errorf("Entrypoint = %q", spec.Entrypoint)
	}
	if !spec.Rm || !spec.Init || !spec.NoNewPriv {
		t.Errorf("lifecycle/hardening: %+v", spec)
	}
	if len(spec.CapDrop) != 1 || spec.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v", spec.CapDrop)
	}
	if strings.Join(spec.CapAdd, ",") != "CHOWN,SETUID,SETGID" {
		t.Errorf("CapAdd = %v", spec.CapAdd)
	}
	if len(spec.Tmpfs) != 1 || spec.Tmpfs[0] != "/tmp:size=256m" {
		t.Errorf("Tmpfs = %v", spec.Tmpfs)
	}
	if spec.Workdir != "/workspace" {
		t.Errorf("Workdir = %q", spec.Workdir)
	}
	if spec.Labels["agentbox.run"] != "run-1" || spec.Labels["agentbox.uid"] != "1000" {
		t.Errorf("Labels = %v", spec.Labels)
	}
	if spec.State == nil || spec.State.Container != "/state" {
		t.Errorf("State = %+v", spec.State)
	}
	if spec.Workspace.ReadOnly {
		t.Error("workspace must be rw by default")
	}
	if spec.User != "" {
		t.Errorf("built-in adapters must not pass --user: %q", spec.User)
	}
}

func TestAssembleArgsComposition(t *testing.T) {
	in := baseInput(t)
	in.AgentArgs = []string{"--print", "hello"}
	in.Ephemeral = true
	spec := Assemble(in)
	want := []string{"pi", "--no-session", "--print", "hello"}
	if strings.Join(spec.Cmd, " ") != strings.Join(want, " ") {
		t.Errorf("Cmd = %v, want %v", spec.Cmd, want)
	}
	if spec.State != nil {
		t.Errorf("ephemeral must have no state mount: %+v", spec.State)
	}
}

func TestAssembleReadOnly(t *testing.T) {
	in := baseInput(t)
	in.ReadOnly = true
	spec := Assemble(in)
	if !spec.Workspace.ReadOnly {
		t.Error("--ro must set the mount read-only")
	}
	if spec.ReadOnly != true {
		t.Error("RunSpec.ReadOnly must be set")
	}
}

func TestAssembleImageRefAdapter(t *testing.T) {
	in := baseInput(t)
	in.Adapter = testAdapter(t, "image:alpine:3.20")
	in.Image = "alpine:3.20"
	in.AgentArgs = []string{"echo", "hi"}
	in.User = "1000:1000"
	spec := Assemble(in)
	if spec.Entrypoint != "" {
		t.Errorf("image:<ref> must not force the agentbox entrypoint: %q", spec.Entrypoint)
	}
	if strings.Join(spec.Cmd, " ") != "echo hi" {
		t.Errorf("Cmd = %v", spec.Cmd)
	}
	if spec.User != "1000:1000" {
		t.Errorf("User = %q", spec.User)
	}
}

func TestAssembleTrailerArgsAfterUserArgs(t *testing.T) {
	in := baseInput(t)
	in.Adapter = testAdapter(t, "opencode")
	in.AgentArgs = []string{"auth", "list"}
	spec := Assemble(in)
	want := []string{"opencode", "auth", "list", "--standalone"}
	if strings.Join(spec.Cmd, " ") != strings.Join(want, " ") {
		t.Errorf("Cmd = %v, want %v (trailer must come last)", spec.Cmd, want)
	}
}

func TestAssembleShell(t *testing.T) {
	in := baseInput(t)
	in.Adapter = testAdapter(t, "shell")
	in.AgentArgs = []string{"-c", "pwd"}
	spec := Assemble(in)
	if strings.Join(spec.Cmd, " ") != "bash -c pwd" {
		t.Errorf("Cmd = %v", spec.Cmd)
	}
}

func TestAssembleTTYAndKeep(t *testing.T) {
	in := baseInput(t)
	in.TTY = true
	in.Keep = true
	spec := Assemble(in)
	if !spec.TTY || !spec.Keep {
		t.Errorf("TTY/Keep: %+v", spec)
	}
}

func TestResolveImage(t *testing.T) {
	a := testAdapter(t, "pi")
	if got := ResolveImage("", a); got != a.Image {
		t.Errorf("ResolveImage(\"\") = %q, want %q", got, a.Image)
	}
	if got := ResolveImage("custom/img:1", a); got != "custom/img:1" {
		t.Errorf("ResolveImage override = %q", got)
	}
}

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"# comment",
		"",
		"PLAIN=value",
		"QUOTED=\"quoted value\"",
		"SINGLE='single value'",
		"export EXPORTED=yes",
		"EMPTY=",
		"NO_EQUALS",
		"WITH_EQUALS=a=b",
	}, "\n")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	kvs, err := ParseEnvFile(f)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range kvs {
		got[kv.Key] = kv.Value
	}
	want := map[string]string{
		"PLAIN": "value", "QUOTED": "quoted value", "SINGLE": "single value",
		"EXPORTED": "yes", "EMPTY": "", "WITH_EQUALS": "a=b",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	if _, ok := got["NO_EQUALS"]; ok {
		t.Errorf("malformed line must be skipped: %v", got)
	}
}

func TestParseEnvFileMissing(t *testing.T) {
	if _, err := ParseEnvFile("/nonexistent/env-file"); err == nil {
		t.Fatal("expected error for missing env file")
	}
}

func TestParseOwner(t *testing.T) {
	pid, boot, ok := ParseOwner("4242@boot-a1b2")
	if !ok || pid != 4242 || boot != "boot-a1b2" {
		t.Errorf("ParseOwner = %d %q %v", pid, boot, ok)
	}
	for _, bad := range []string{"", "4242", "@boot", "abc@boot", "42@"} {
		if _, _, ok := ParseOwner(bad); ok {
			t.Errorf("ParseOwner(%q) must fail", bad)
		}
	}
}

func TestOwnerAlive(t *testing.T) {
	procAlive := func(pid int) bool { return pid == 7 }
	cases := []struct {
		owner    string
		selfBoot string
		want     bool
	}{
		{"7@boot-a", "boot-a", true},  // same boot, pid alive
		{"8@boot-a", "boot-a", false}, // same boot, pid dead
		{"7@boot-b", "boot-a", false}, // different boot => dead
		{"7@", "boot-a", false},       // malformed => dead (safe to sweep)
		{"", "boot-a", false},         // malformed => dead
	}
	for _, tc := range cases {
		if got := OwnerAlive(tc.owner, tc.selfBoot, procAlive); got != tc.want {
			t.Errorf("OwnerAlive(%q, %q) = %v, want %v", tc.owner, tc.selfBoot, got, tc.want)
		}
	}
}

func TestParseContainerList(t *testing.T) {
	out := strings.Join([]string{
		"abc123\t7@boot-a",
		"def456\t8@boot-a",
		"",
		"ghi789\t",
	}, "\n")
	got := ParseContainerList(out)
	if len(got) != 3 {
		t.Fatalf("parsed %d, want 3: %+v", len(got), got)
	}
	if got[0].ID != "abc123" || got[0].Owner != "7@boot-a" {
		t.Errorf("got[0] = %+v", got[0])
	}
}

func TestBannerFormat(t *testing.T) {
	b := Banner("pi", "/workspace", "/home/me/src/app", "rw", "bridge", "1a2b3c4d")
	if !strings.HasPrefix(b, "agentbox: pi @ /workspace \u2190 /home/me/src/app (rw, net: bridge, state: 1a2b3c4d)") {
		t.Errorf("banner = %q", b)
	}
	if b2 := Banner("shell", "/workspace", "/x", "ro", "none", "ephemeral"); !strings.Contains(b2, "state: ephemeral") {
		t.Errorf("banner = %q", b2)
	}
}

func TestWorkspaceModeString(t *testing.T) {
	if got := workspaceMode(false); got != "rw" {
		t.Errorf("workspaceMode(false) = %q", got)
	}
	if got := workspaceMode(true); got != "ro" {
		t.Errorf("workspaceMode(true) = %q", got)
	}
}

func TestStateDescription(t *testing.T) {
	if got := stateDescription(false, "", "app-1a2b3c4d"); got != "1a2b3c4d" {
		t.Errorf("volume state = %q", got)
	}
	if got := stateDescription(true, "", "app-1a2b3c4d"); got != "ephemeral" {
		t.Errorf("ephemeral state = %q", got)
	}
	if got := stateDescription(false, "/host/state", "app-1a2b3c4d"); got != "/host/state" {
		t.Errorf("state-dir state = %q", got)
	}
	if got := stateDescription(false, "", "test-project"); got != "test-project" {
		t.Errorf("pinned test state = %q", got)
	}
	if got := stateDescription(false, "", "my-project-zzzzzzzz"); got != "my-project-zzzzzzzz" {
		t.Errorf("non-hex suffix = %q", got)
	}
}

func TestChildEnvMergesEnvFile(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/me"}
	kvs := []KV{{Key: "FOO", Value: "bar"}, {Key: "PATH", Value: "/custom"}}
	got := ChildEnv(base, kvs)
	seen := map[string]string{}
	for _, e := range got {
		parts := strings.SplitN(e, "=", 2)
		seen[parts[0]] = parts[1]
	}
	if seen["FOO"] != "bar" {
		t.Errorf("FOO missing: %v", got)
	}
	if seen["PATH"] != "/custom" {
		t.Errorf("env-file must override host: %v", got)
	}
	if seen["HOME"] != "/home/me" {
		t.Errorf("host env must be preserved: %v", got)
	}
}
