package authstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/authstore"
)

func mustAdapter(t *testing.T, name string) adapter.Adapter {
	t.Helper()
	a, err := adapter.Get(name, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStoreLoadReset(t *testing.T) {
	root := t.TempDir()
	if _, _, ok, _ := authstore.Load(root, "pi"); ok {
		t.Fatal("nothing stored yet")
	}
	p, err := authstore.Store(root, "pi", []byte(`{"token":"x"}`))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("stored credential mode = %o, want 600", st.Mode().Perm())
	}
	data, _, ok, err := authstore.Load(root, "pi")
	if err != nil || !ok || string(data) != `{"token":"x"}` {
		t.Fatalf("Load = %q ok=%v err=%v", data, ok, err)
	}
	removed, err := authstore.Reset(root, "pi")
	if err != nil || !removed {
		t.Fatalf("Reset = %v %v", removed, err)
	}
	if removed, _ := authstore.Reset(root, "pi"); removed {
		t.Error("second reset should report nothing removed")
	}
}

func TestStoreRejectsEmpty(t *testing.T) {
	if _, err := authstore.Store(t.TempDir(), "pi", nil); err == nil {
		t.Fatal("empty credentials must be rejected")
	}
}

func TestAdoptFromFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"k":"v"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	data, src, err := authstore.Adopt(mustAdapter(t, "pi"), env, nil)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if string(data) != `{"k":"v"}` || src.Kind != "file" || !strings.Contains(src.Ref, ".pi/agent/auth.json") {
		t.Errorf("data=%q src=%+v", data, src)
	}
}

func TestAdoptMissingFile(t *testing.T) {
	env := func(k string) string {
		if k == "HOME" {
			return t.TempDir()
		}
		return ""
	}
	_, _, err := authstore.Adopt(mustAdapter(t, "pi"), env, nil)
	if err == nil || !strings.Contains(err.Error(), "log in on the host first") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdoptFromExportCommand(t *testing.T) {
	oc := mustAdapter(t, "opencode")
	var gotName string
	var gotArgs []string
	run := func(name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return []byte(`{"list":[{"id":"1"}]}`), nil
	}
	data, src, err := authstore.Adopt(oc, func(string) string { return "" }, run)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if gotName != "opencode" || strings.Join(gotArgs, " ") != "auth export" {
		t.Errorf("ran %s %v", gotName, gotArgs)
	}
	if src.Kind != "command" || !strings.Contains(string(data), `"list"`) {
		t.Errorf("src=%+v data=%q", src, data)
	}
}

func TestAdoptExportFailure(t *testing.T) {
	oc := mustAdapter(t, "opencode")
	run := func(string, ...string) ([]byte, error) { return nil, errors.New("exit 1") }
	if _, _, err := authstore.Adopt(oc, func(string) string { return "" }, run); err == nil {
		t.Fatal("expected error")
	}
	run = func(string, ...string) ([]byte, error) { return []byte("not json"), nil }
	if _, _, err := authstore.Adopt(oc, func(string) string { return "" }, run); err == nil {
		t.Fatal("expected non-JSON rejection")
	}
}

func TestAdoptShellHasNoCredential(t *testing.T) {
	if _, _, err := authstore.Adopt(mustAdapter(t, "shell"), func(string) string { return "" }, nil); err == nil {
		t.Fatal("shell has no credential source")
	}
}

func TestPathUsesHarnessName(t *testing.T) {
	if got := authstore.Path("/root", "claude"); got != "/root/credentials/claude.json" {
		t.Errorf("Path = %q", got)
	}
}
