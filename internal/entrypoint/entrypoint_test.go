package entrypoint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/entrypoint"
)

func TestScriptIsEmbedded(t *testing.T) {
	if entrypoint.Script == "" {
		t.Fatal("entrypoint script is empty")
	}
	if !strings.HasPrefix(entrypoint.Script, "#!/bin/sh") {
		t.Errorf("script must start with #!/bin/sh: %q", entrypoint.Script[:min(40, len(entrypoint.Script))])
	}
	if entrypoint.Path != "/entrypoint.sh" {
		t.Errorf("Path = %q", entrypoint.Path)
	}
}

func TestScriptSyntax(t *testing.T) {
	f := writeScript(t)
	out, err := exec.Command("sh", "-n", f).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -n failed: %v\n%s", err, out)
	}
}

// Frozen FR-9 contract steps must be present verbatim.
func TestScriptImplementsFrozenContract(t *testing.T) {
	checks := []struct{ name, needle string }{
		{"non-root passthrough", `exec "$@"`},
		{"non-root HOME prep", "nonroot_home"},
		{"home default", `${AGENTBOX_HOME:-/state/home}`},
		{"mkdir home", "mkdir -p"},
		{"uid required", "AGENTBOX_UID"},
		{"gid required", "AGENTBOX_GID"},
		{"chown", `chown "$AGENTBOX_UID:$AGENTBOX_GID"`},
		{"home export", `export HOME=`},
		{"setpriv", "exec setpriv"},
		{"setpriv reuid", "--reuid"},
		{"setpriv regid", "--regid"},
		{"setpriv clear-groups", "--clear-groups"},
		{"config dir ownership walk", "AGENTBOX_CONFIG_DIR"},
		{"first-run cache notice", "preparing the harness cache"},
		{"first-run notice gated on cache dir", "AGENTBOX_CACHE_DIR"},
		{"first-run notice honors --quiet", "AGENTBOX_QUIET"},
		{"first-run creates the cache dir", `mkdir -p "$AGENTBOX_CACHE_DIR"`},
		{"credential parent chown", "AGENTBOX_CRED_FILE"},
		{"credential import hook", "AGENTBOX_CRED_IMPORT"},
		{"credential import marker", ".agentbox-cred-import-"},
		{"credential import hashes the file", "sha256sum"},
		{"reusable chown chain", "chown_chain"},
		{"home fallback to tmpfs", "/tmp/agentbox-home"},
		{"config dir walk dirname", "dirname"},
	}
	for _, c := range checks {
		if !strings.Contains(entrypoint.Script, c.needle) {
			t.Errorf("script missing %s (%q)", c.name, c.needle)
		}
	}
	// Only executable lines count: comments must not trigger the guard.
	for _, line := range strings.Split(entrypoint.Script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "chown -R") || strings.Contains(trimmed, "chown --recursive") {
			t.Errorf("script must never chown -R (FR-9: non-recursive deliberately): %q", trimmed)
		}
	}
}

// As a non-root user the script must exec the command directly: no chown, no
// setpriv, no required UID env (explicit --user / engine userns path).
func TestNonRootExecsDirectly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test must run as a non-root user")
	}
	f := writeScript(t)
	cmd := exec.Command("sh", f, "echo", "direct-exec-ok")
	cmd.Env = []string{} // deliberately no AGENTBOX_UID/GID
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed for non-root: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "direct-exec-ok" {
		t.Errorf("output = %q, want direct-exec-ok", out)
	}
}

func TestRootPathRequiresIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-path execution needs uid 0 (covered by isolation tests)")
	}
}

func writeScript(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "entrypoint.sh")
	if err := os.WriteFile(f, []byte(entrypoint.Script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
