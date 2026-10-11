package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/cli"
)

func configEnv(home string) func(string) string {
	return func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
}

// seedHostConfig creates a fake harness host config dir.
func seedHostConfig(t *testing.T, dir string) {
	t.Helper()
	for _, d := range []string{"skills", "extensions", "sessions"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "SKILL.md"), []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigLinkCopyReset(t *testing.T) {
	home := t.TempDir()
	hostCfg := filepath.Join(home, ".pi", "agent")
	seedHostConfig(t, hostCfg)
	env := configEnv(home)
	target := filepath.Join(home, ".agentbox", "agents", "pi")

	// link
	var out, errb bytes.Buffer
	code := cli.Config(&cli.Options{SubArgs: []string{"link"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", env)
	if code != 0 {
		t.Fatalf("link exit = %d, stderr = %s", code, errb.String())
	}
	st, err := os.Lstat(target)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s: %v %v", target, st, err)
	}
	if !strings.Contains(out.String(), "linked") {
		t.Errorf("output = %q", out.String())
	}

	// reset (force: no TTY in tests)
	out.Reset()
	errb.Reset()
	code = cli.Config(&cli.Options{SubArgs: []string{"reset", "pi"}, Force: true}, cli.IO{Out: &out, Err: &errb}, "0.1.0", env)
	if code != 0 {
		t.Fatalf("reset exit = %d, stderr = %s", code, errb.String())
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("link not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hostCfg, "skills", "SKILL.md")); err != nil {
		t.Fatalf("source damaged by reset: %v", err)
	}

	// copy skips sessions/auth
	out.Reset()
	errb.Reset()
	code = cli.Config(&cli.Options{SubArgs: []string{"copy"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", env)
	if code != 0 {
		t.Fatalf("copy exit = %d, stderr = %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "SKILL.md")); err != nil {
		t.Errorf("skills not copied: %v", err)
	}
	for _, excluded := range []string{"sessions", "auth.json"} {
		if _, err := os.Lstat(filepath.Join(target, excluded)); !os.IsNotExist(err) {
			t.Errorf("%s must not be copied", excluded)
		}
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Errorf("copy output should mention skipped entries: %q", out.String())
	}
}

func TestConfigLinkFromOverride(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	seedHostConfig(t, src)
	var out, errb bytes.Buffer
	code := cli.Config(&cli.Options{SubArgs: []string{"link", "pi"}, From: src}, cli.IO{Out: &out, Err: &errb}, "0.1.0", configEnv(home))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	link, err := os.Readlink(filepath.Join(home, ".agentbox", "agents", "pi"))
	if err != nil || link != src {
		t.Errorf("link = %q err %v", link, err)
	}
}

func TestConfigResetNeedsConfirmation(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agentbox", "agents", "pi")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := cli.Config(&cli.Options{SubArgs: []string{"reset"}}, cli.IO{Out: &out, Err: &errb, IsTTY: false}, "0.1.0", configEnv(home))
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (confirmation required)", code)
	}
	if !strings.Contains(errb.String(), "--force") {
		t.Errorf("stderr must offer --force: %q", errb.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("reset must not run without confirmation: %v", err)
	}
}

func TestConfigUnknownHarness(t *testing.T) {
	home := t.TempDir()
	var out, errb bytes.Buffer
	code := cli.Config(&cli.Options{SubArgs: []string{"link", "bogus"}}, cli.IO{Out: &out, Err: &errb}, "0.1.0", configEnv(home))
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "pi") {
		t.Errorf("error should list valid harnesses: %q", errb.String())
	}
}
