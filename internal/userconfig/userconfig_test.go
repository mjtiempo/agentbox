package userconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/userconfig"
)

func mustAdapter(t *testing.T, name string) adapter.Adapter {
	t.Helper()
	a, err := adapter.Get(name, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func envFrom(m map[string]string) userconfig.Env {
	return func(k string) string { return m[k] }
}

func TestSourceDirDefaults(t *testing.T) {
	cases := []struct {
		harness string
		env     map[string]string
		want    string
	}{
		{"pi", map[string]string{"HOME": "/home/u"}, "/home/u/.pi/agent"},
		{"claude", map[string]string{"HOME": "/home/u"}, "/home/u/.claude"},
		{"opencode", map[string]string{"HOME": "/home/u"}, "/home/u/.config/opencode"},
		{"opencode", map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/xdg"}, "/xdg/opencode"},
	}
	for _, tc := range cases {
		got, err := userconfig.SourceDir(mustAdapter(t, tc.harness), "", envFrom(tc.env))
		if err != nil {
			t.Fatalf("%s: %v", tc.harness, err)
		}
		if got != tc.want {
			t.Errorf("%s SourceDir = %q, want %q", tc.harness, got, tc.want)
		}
	}
}

func TestSourceDirFromOverride(t *testing.T) {
	got, err := userconfig.SourceDir(mustAdapter(t, "pi"), "/custom/pi", envFrom(nil))
	if err != nil || got != "/custom/pi" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestSourceDirNoHome(t *testing.T) {
	if _, err := userconfig.SourceDir(mustAdapter(t, "pi"), "", envFrom(nil)); err == nil {
		t.Fatal("expected error without HOME")
	}
}

func TestLinkAndReset(t *testing.T) {
	src := t.TempDir()
	target := filepath.Join(t.TempDir(), "agents", "pi")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := userconfig.Link(target, src, false)
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if res.Action != "linked" {
		t.Errorf("Action = %q", res.Action)
	}
	st, err := os.Lstat(target)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("target is not a symlink: %v %v", st, err)
	}
	// Idempotent.
	res, err = userconfig.Link(target, src, false)
	if err != nil || res.Action != "unchanged" {
		t.Fatalf("second link: %+v %v", res, err)
	}
	// Reset removes the link but never the source.
	res, err = userconfig.Reset(target)
	if err != nil || res.Action != "reset" {
		t.Fatalf("Reset: %+v %v", res, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("target still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "settings.json")); err != nil {
		t.Fatalf("source was damaged: %v", err)
	}
	// Reset again is a no-op.
	res, err = userconfig.Reset(target)
	if err != nil || res.Action != "unchanged" {
		t.Fatalf("second reset: %+v %v", res, err)
	}
}

func TestLinkReplacesEmptyStandardDirs(t *testing.T) {
	src := t.TempDir()
	parent := t.TempDir()
	target := filepath.Join(parent, "agents", "pi")
	// Simulate a previous run's pre-created dirs.
	for _, d := range []string{"extensions", "skills", "themes"} {
		if err := os.MkdirAll(filepath.Join(target, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := userconfig.Link(target, src, false); err != nil {
		t.Fatalf("Link over empty dirs: %v", err)
	}
	st, _ := os.Lstat(target)
	if st.Mode()&os.ModeSymlink == 0 {
		t.Error("target should be a symlink now")
	}
}

func TestLinkRefusesExistingContent(t *testing.T) {
	src := t.TempDir()
	target := filepath.Join(t.TempDir(), "agents", "pi")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := userconfig.Link(target, src, false); err == nil {
		t.Fatal("expected refusal without --force")
	}
	if _, err := userconfig.Link(target, src, true); err != nil {
		t.Fatalf("force link: %v", err)
	}
}

func TestLinkMissingSource(t *testing.T) {
	if _, err := userconfig.Link(filepath.Join(t.TempDir(), "t"), filepath.Join(t.TempDir(), "nope"), false); err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCopySkipsExcludedAndExisting(t *testing.T) {
	src := t.TempDir()
	for _, d := range []string{"extensions", "skills", "sessions"} {
		if err := os.MkdirAll(filepath.Join(src, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "auth.json"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "SKILL.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sessions", "s.jsonl"), []byte("session"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "agents", "pi")

	res, err := userconfig.Copy(target, src, mustAdapter(t, "pi"), false)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "SKILL.md")); err != nil {
		t.Errorf("skills not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "extensions")); err != nil {
		t.Errorf("extensions not copied: %v", err)
	}
	for _, excluded := range []string{"sessions", "auth.json"} {
		if _, err := os.Lstat(filepath.Join(target, excluded)); !os.IsNotExist(err) {
			t.Errorf("%s must not be copied (sessions/auth stay per-project)", excluded)
		}
		if !contains(res.Skipped, excluded) {
			t.Errorf("%s should be reported as skipped: %v", excluded, res.Skipped)
		}
	}
	if len(res.Entries) != 2 {
		t.Errorf("Entries = %v", res.Entries)
	}
}

func TestCopySkipsExistingWithoutForce(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "settings.json"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "agents", "pi")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "settings.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := userconfig.Copy(target, src, mustAdapter(t, "pi"), false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(target, "settings.json"))
	if string(got) != "old" {
		t.Errorf("existing file overwritten without --force: %q", got)
	}
	if _, err := userconfig.Copy(target, src, mustAdapter(t, "pi"), true); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(target, "settings.json"))
	if string(got) != "new" {
		t.Errorf("--force did not overwrite: %q", got)
	}
}

func TestCopyDoesNotWriteThroughSymlink(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "settings.json"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "agents", "pi")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(target, "settings.json")
	if err := os.Symlink(sentinel, dst); err != nil {
		t.Fatal(err)
	}

	// Without force an existing destination (symlink included) is skipped.
	res, err := userconfig.Copy(target, src, mustAdapter(t, "pi"), false)
	if err != nil {
		t.Fatalf("Copy without force: %v", err)
	}
	if !contains(res.Skipped, "settings.json") {
		t.Errorf("Entries = %v Skipped = %v, want settings.json skipped", res.Entries, res.Skipped)
	}
	if st, err := os.Lstat(dst); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("non-force copy touched the destination symlink: %v %v", st, err)
	}

	// With force the symlink is replaced by a regular file holding the source
	// content, and the sentinel it pointed at is untouched.
	res, err = userconfig.Copy(target, src, mustAdapter(t, "pi"), true)
	if err != nil {
		t.Fatalf("Copy force: %v", err)
	}
	if !contains(res.Entries, "settings.json") {
		t.Errorf("Entries = %v, want settings.json", res.Entries)
	}
	st, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("destination missing after force copy: %v", err)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		t.Errorf("destination is still a symlink after force copy")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Errorf("destination content = %q err %v, want %q", got, err, "new")
	}
	sentinelGot, err := os.ReadFile(sentinel)
	if err != nil || string(sentinelGot) != "sentinel" {
		t.Errorf("sentinel changed to %q err %v, want %q", sentinelGot, err, "sentinel")
	}
}

func TestCopyMergesIntoPreCreatedDirs(t *testing.T) {
	// After a first run the standard dirs exist but are empty; config copy
	// must merge into them instead of skipping them.
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "linked", "SKILL.md"), []byte("linked"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "agents", "pi")
	for _, d := range []string{"extensions", "skills", "themes"} {
		if err := os.MkdirAll(filepath.Join(target, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	res, err := userconfig.Copy(target, src, mustAdapter(t, "pi"), false)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "skills", "linked", "SKILL.md"))
	if err != nil || string(got) != "linked" {
		t.Fatalf("merge did not copy into the pre-created dir: %q %v", got, err)
	}
	if !contains(res.Entries, "skills") {
		t.Errorf("Entries = %v, want skills", res.Entries)
	}
}

func TestResetRemovesCopiedDir(t *testing.T) {
	target := filepath.Join(t.TempDir(), "agents", "pi")
	if err := os.MkdirAll(filepath.Join(target, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "skills", "S.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := userconfig.Reset(target)
	if err != nil || res.Action != "reset" {
		t.Fatalf("Reset: %+v %v", res, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("target still exists: %v", err)
	}
}

func TestTargetDir(t *testing.T) {
	got := userconfig.TargetDir("/root", mustAdapter(t, "claude"))
	if got != "/root/agents/claude" {
		t.Errorf("TargetDir = %q", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s || strings.HasPrefix(x, s) {
			return true
		}
	}
	return false
}
