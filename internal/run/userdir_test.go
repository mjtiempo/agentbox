package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/adapter"
)

func TestUserRootDefault(t *testing.T) {
	host := Host{Env: func(k string) string {
		if k == "HOME" {
			return "/home/user"
		}
		return ""
	}}
	if got := UserRoot(host); got != "/home/user/.agentbox" {
		t.Errorf("UserRoot = %q", got)
	}
}

func TestUserRootOverride(t *testing.T) {
	host := Host{Env: func(k string) string {
		switch k {
		case "HOME":
			return "/home/user"
		case "AGENTBOX_USER_DIR":
			return "/srv/agentbox"
		}
		return ""
	}}
	if got := UserRoot(host); got != "/srv/agentbox" {
		t.Errorf("UserRoot = %q", got)
	}
}

func TestUserRootNoHome(t *testing.T) {
	host := Host{Env: func(string) string { return "" }}
	if got := UserRoot(host); got != "" {
		t.Errorf("UserRoot = %q, want empty", got)
	}
	mounts, notice, err := userConfigMounts(mustAdapter(t, "pi"), host, "", false)
	if err != nil || mounts != nil {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
	if !strings.Contains(notice, "HOME is not set") {
		t.Errorf("notice = %q", notice)
	}
}

func TestUserConfigMountsCreatesAndMounts(t *testing.T) {
	root := t.TempDir()
	host := Host{Env: func(k string) string {
		if k == "AGENTBOX_USER_DIR" {
			return root
		}
		return ""
	}}
	pi := mustAdapter(t, "pi")

	// Seed an entry that is not a standard dir (dynamic mount).
	if err := os.MkdirAll(filepath.Join(root, "agents", "pi", "skills", "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "pi", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	mounts, notice, err := userConfigMounts(pi, host, "z", false)
	if err != nil {
		t.Fatalf("userConfigMounts: %v", err)
	}
	if notice != "" {
		t.Errorf("unexpected notice %q", notice)
	}
	// Standard dirs pre-created.
	for _, sub := range pi.UserDirs {
		if st, err := os.Stat(filepath.Join(root, "agents", "pi", sub)); err != nil || !st.IsDir() {
			t.Errorf("standard dir %q not created", sub)
		}
	}
	got := map[string]string{}
	for _, m := range mounts {
		got[m.Container] = m.Host
		if m.ReadOnly {
			t.Errorf("user folder mounts must be writable: %+v", m)
		}
		if m.Relabel != "z" {
			t.Errorf("relabel not propagated: %+v", m)
		}
	}
	if got["/state/pi/skills"] != filepath.Join(root, "agents", "pi", "skills") {
		t.Errorf("skills mount = %q", got["/state/pi/skills"])
	}
	if got["/state/pi/settings.json"] != filepath.Join(root, "agents", "pi", "settings.json") {
		t.Errorf("settings mount = %q", got["/state/pi/settings.json"])
	}
	if got["/state/pi/extensions"] == "" || got["/state/pi/themes"] == "" {
		t.Errorf("standard dirs must be mounted: %v", got)
	}
}

func TestUserConfigMountsDisabled(t *testing.T) {
	root := t.TempDir()
	host := Host{Env: func(k string) string {
		if k == "AGENTBOX_USER_DIR" {
			return root
		}
		return ""
	}}
	mounts, notice, err := userConfigMounts(mustAdapter(t, "pi"), host, "", true)
	if err != nil || mounts != nil || notice != "" {
		t.Fatalf("disabled: mounts=%v notice=%q err=%v", mounts, notice, err)
	}
}

func TestUserConfigMountsShellHasNone(t *testing.T) {
	root := t.TempDir()
	host := Host{Env: func(k string) string {
		if k == "AGENTBOX_USER_DIR" {
			return root
		}
		return ""
	}}
	mounts, _, err := userConfigMounts(mustAdapter(t, "shell"), host, "", false)
	if err != nil || mounts != nil {
		t.Fatalf("shell must have no user mounts: %v %v", mounts, err)
	}
	if _, err := os.Stat(filepath.Join(root, "agents", "shell")); err == nil {
		t.Error("shell must not create a user folder")
	}
}

func TestAdapterUserDirs(t *testing.T) {
	cases := map[string]struct {
		configDir string
		userDirs  []string
	}{
		"pi":       {"/state/pi", []string{"extensions", "skills", "themes"}},
		"claude":   {"/state/claude", []string{"plugins", "skills", "themes"}},
		"opencode": {"/state/opencode/config", []string{"plugins", "skills", "agents", "commands"}},
		"shell":    {"", nil},
	}
	for name, want := range cases {
		a := mustAdapter(t, name)
		if a.ConfigDir != want.configDir {
			t.Errorf("%s ConfigDir = %q, want %q", name, a.ConfigDir, want.configDir)
		}
		if strings.Join(a.UserDirs, ",") != strings.Join(want.userDirs, ",") {
			t.Errorf("%s UserDirs = %v, want %v", name, a.UserDirs, want.userDirs)
		}
	}
}

func mustAdapter(t *testing.T, name string) adapter.Adapter {
	t.Helper()
	a, err := adapter.Get(name, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
