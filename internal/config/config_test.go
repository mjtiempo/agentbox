package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/config"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadUserConfigAllKeys(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "config.toml", `
agent = "claude"
cpus = "4"
memory = "8g"
pids-limit = 1024
ro = true
ephemeral = true
quiet = true
net = "none"
env = ["FOO", "BAR"]
env-file = ["a.env"]
state-dir = "/tmp/state"
credentials = ["/tmp/cred.json"]
user = "1000:1000"
name = "mybox"
keep = true
image = "custom/img:1"
`)
	v, err := config.LoadUser(p)
	if err != nil {
		t.Fatalf("LoadUser: %v", err)
	}
	if v.Agent == nil || *v.Agent != "claude" {
		t.Errorf("Agent = %v", v.Agent)
	}
	if v.CPUs == nil || *v.CPUs != "4" {
		t.Errorf("CPUs = %v", v.CPUs)
	}
	if v.Memory == nil || *v.Memory != "8g" {
		t.Errorf("Memory = %v", v.Memory)
	}
	if v.PidsLimit == nil || *v.PidsLimit != 1024 {
		t.Errorf("PidsLimit = %v", v.PidsLimit)
	}
	if v.RO == nil || !*v.RO || v.Ephemeral == nil || !*v.Ephemeral || v.Quiet == nil || !*v.Quiet {
		t.Errorf("bools = %+v", v)
	}
	if v.Net == nil || *v.Net != "none" {
		t.Errorf("Net = %v", v.Net)
	}
	if len(v.Env) != 2 || v.Env[0] != "FOO" {
		t.Errorf("Env = %v", v.Env)
	}
	if v.StateDir == nil || *v.StateDir != "/tmp/state" {
		t.Errorf("StateDir = %v", v.StateDir)
	}
	if len(v.Credentials) != 1 || v.User == nil || v.Name == nil || v.Keep == nil {
		t.Errorf("credentials/user/name/keep = %+v", v)
	}
	if v.Image == nil || *v.Image != "custom/img:1" {
		t.Errorf("Image = %v", v.Image)
	}
}

func TestProjectConfigAllowedKeys(t *testing.T) {
	ws := t.TempDir()
	p := writeFile(t, ws, "agentbox.toml", `
agent = "shell"
cpus = "1"
memory = "2g"
pids-limit = 128
ro = true
ephemeral = true
`)
	v, err := config.LoadProject(p, ws)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if v.Agent == nil || *v.Agent != "shell" || v.CPUs == nil || *v.CPUs != "1" {
		t.Errorf("v = %+v", v)
	}
	if v.RO == nil || !*v.RO || v.Ephemeral == nil || !*v.Ephemeral {
		t.Errorf("v = %+v", v)
	}
}

func TestProjectConfigRejectsBoundaryKeys(t *testing.T) {
	ws := t.TempDir()
	for _, key := range []string{"image", "net", "env", "env-file", "state-dir", "credentials", "user", "name", "keep"} {
		var content string
		switch key {
		case "env", "env-file", "credentials":
			content = key + ` = ["x"]` + "\n"
		case "keep":
			content = key + " = true\n"
		default:
			content = key + ` = "x"` + "\n"
		}
		p := writeFile(t, ws, "agentbox.toml", content)
		_, err := config.LoadProject(p, ws)
		if err == nil {
			t.Errorf("key %q must be rejected", key)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, key) {
			t.Errorf("key %q: error must name the key: %q", key, msg)
		}
		if !strings.Contains(msg, "project config") {
			t.Errorf("key %q: error must mention project config: %q", key, msg)
		}
	}
}

func TestProjectConfigRejectsQuiet(t *testing.T) {
	ws := t.TempDir()
	p := writeFile(t, ws, "agentbox.toml", "quiet = true\n")
	_, err := config.LoadProject(p, ws)
	if err == nil || !strings.Contains(err.Error(), "quiet") {
		t.Errorf("err = %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "provenance") {
		t.Errorf("quiet error should explain why: %v", err)
	}
}

func TestProjectConfigRejectsImageAgent(t *testing.T) {
	ws := t.TempDir()
	p := writeFile(t, ws, "agentbox.toml", `agent = "image:alpine"`+"\n")
	_, err := config.LoadProject(p, ws)
	if err == nil || !strings.Contains(err.Error(), "image:<ref>") {
		t.Errorf("err = %v", err)
	}
}

func TestProjectConfigRejectsUnknownKey(t *testing.T) {
	ws := t.TempDir()
	p := writeFile(t, ws, "agentbox.toml", "wat = true\n")
	_, err := config.LoadProject(p, ws)
	if err == nil || !strings.Contains(err.Error(), "wat") {
		t.Errorf("err = %v", err)
	}
}

func TestUserConfigRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "config.toml", "wat = true\n")
	_, err := config.LoadUser(p)
	if err == nil || !strings.Contains(err.Error(), "wat") {
		t.Errorf("err = %v", err)
	}
}

func TestParseErrorDoesNotEchoContents(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "config.toml", "secret-looking-value = [unclosed\n")
	_, err := config.LoadUser(p)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(err.Error(), "unclosed") || strings.Contains(err.Error(), "secret-looking-value") {
		t.Errorf("parse error must not echo file contents: %v", err)
	}
}

func TestProjectConfigSymlinkEscapeRejected(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	target := writeFile(t, outside, "evil.toml", "cpus = \"1\"\n")
	link := filepath.Join(ws, "agentbox.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadProject(link, ws)
	if err == nil || !strings.Contains(err.Error(), "outside the workspace") {
		t.Errorf("symlink escape must be rejected: %v", err)
	}
}

func TestMergePrecedence(t *testing.T) {
	s := func(v string) *string { return &v }
	b := func(v bool) *bool { return &v }
	i := func(v int) *int { return &v }
	user := &config.Values{Agent: s("pi"), CPUs: s("2"), RO: b(false)}
	project := &config.Values{CPUs: s("4"), Ephemeral: b(true)}
	cli := &config.Values{RO: b(true), PidsLimit: i(64)}

	merged := config.Merge(user, project, cli)
	if merged.Agent == nil || *merged.Agent != "pi" {
		t.Errorf("Agent = %v (user value must survive)", merged.Agent)
	}
	if merged.CPUs == nil || *merged.CPUs != "4" {
		t.Errorf("CPUs = %v (project beats user)", merged.CPUs)
	}
	if merged.RO == nil || !*merged.RO {
		t.Errorf("RO = %v (cli beats all)", merged.RO)
	}
	if merged.Ephemeral == nil || !*merged.Ephemeral {
		t.Errorf("Ephemeral = %v", merged.Ephemeral)
	}
	if merged.PidsLimit == nil || *merged.PidsLimit != 64 {
		t.Errorf("PidsLimit = %v", merged.PidsLimit)
	}
}

func TestResolveDiscovery(t *testing.T) {
	ws := t.TempDir()
	home := t.TempDir()
	writeFile(t, ws, "agentbox.toml", "cpus = \"3\"\n")
	xdg := filepath.Join(home, ".config")
	os.MkdirAll(filepath.Join(xdg, "agentbox"), 0o755)
	writeFile(t, filepath.Join(xdg, "agentbox"), "config.toml", "cpus = \"9\"\nmemory = \"1g\"\n")

	env := func(k string) string {
		switch k {
		case "XDG_CONFIG_HOME":
			return xdg
		case "HOME":
			return home
		}
		return ""
	}
	loaded, err := config.Resolve("", ws, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(loaded.Provenance) != 2 {
		t.Fatalf("provenance = %v", loaded.Provenance)
	}
	if loaded.Values.CPUs == nil || *loaded.Values.CPUs != "3" {
		t.Errorf("project must beat user: %v", loaded.Values.CPUs)
	}
	if loaded.Values.Memory == nil || *loaded.Values.Memory != "1g" {
		t.Errorf("user memory must survive: %v", loaded.Values.Memory)
	}
}

func TestResolveMissingExplicitConfigFails(t *testing.T) {
	ws := t.TempDir()
	_, err := config.Resolve("/nonexistent/config.toml", ws, func(string) string { return "" })
	if err == nil {
		t.Fatal("explicit missing config must fail")
	}
}

func TestResolveNoConfig(t *testing.T) {
	ws := t.TempDir()
	loaded, err := config.Resolve("", ws, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(loaded.Provenance) != 0 {
		t.Errorf("provenance = %v, want none", loaded.Provenance)
	}
	if loaded.Values == nil {
		t.Error("Values must never be nil")
	}
}
