// Package workspace canonicalizes the workspace path, enforces the FR-2
// guardrail, scans bounded notices, and probes SELinux relabel state.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Options controls validation.
type Options struct {
	Home  string // host home directory (guardrail comparisons)
	Force bool   // --force overrides the guardrail
}

// Error is a one-line error plus remedy, rendered in the PRD §8.2 style.
type Error struct {
	Msg    string
	Remedy string
}

func (e *Error) Error() string { return e.Msg }

// Render formats the error the way the CLI prints it.
func (e *Error) Render() string {
	if e.Remedy == "" {
		return "agentbox: " + e.Msg + "\n"
	}
	return "agentbox: " + e.Msg + "\n  \u2192 " + e.Remedy + "\n"
}

// Canonical returns the absolute, symlink-resolved path. A path that does not
// exist yields its cleaned absolute form so callers can render the
// "does not exist" error rather than a resolve failure.
func Canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return filepath.Clean(abs), nil
		}
		return filepath.Clean(abs), nil
	}
	return resolved, nil
}

// Validate checks existence, directory-ness, and the guardrail. It returns an
// *Error (never a bare sentinel) so the CLI can render the remedy.
func Validate(dir string, opts Options) error {
	canon, err := Canonical(dir)
	if err != nil {
		return &Error{Msg: fmt.Sprintf("cannot resolve workspace %q", dir), Remedy: "check the path"}
	}
	st, statErr := os.Stat(canon)
	if opts.Force {
		if statErr != nil {
			return statError(dir, statErr)
		}
		if !st.IsDir() {
			return notDirError(dir)
		}
		return nil
	}
	// Guardrail wins over existence: a sensitive target is never mountable,
	// even before the path is created.
	if reason, blocked := Guardrail(canon, opts.Home); blocked {
		return &Error{
			Msg:    fmt.Sprintf("refusing to mount %s — %s", canon, reason),
			Remedy: "cd into the project, or pass --force if you really mean it",
		}
	}
	if statErr != nil {
		return statError(dir, statErr)
	}
	if !st.IsDir() {
		return notDirError(dir)
	}
	return nil
}

func statError(dir string, statErr error) error {
	if os.IsNotExist(statErr) {
		return &Error{
			Msg:    fmt.Sprintf("workspace %q does not exist", dir),
			Remedy: "check the path, or run agentbox with no argument to use the current directory",
		}
	}
	return &Error{
		Msg:    fmt.Sprintf("cannot access workspace %q", dir),
		Remedy: "check permissions on the path",
	}
}

func notDirError(dir string) error {
	return &Error{
		Msg:    fmt.Sprintf("workspace %q is not a directory", dir),
		Remedy: "pass a directory, or run agentbox with no argument to use the current directory",
	}
}

// Guardrail reports whether canonical must be refused and why.
func Guardrail(canonical, home string) (reason string, blocked bool) {
	c := filepath.Clean(canonical)
	if c == string(filepath.Separator) {
		return "this is the whole filesystem, not a project", true
	}
	if home != "" {
		h := filepath.Clean(home)
		if c == h {
			return "this is your home directory, not a project", true
		}
		if isAncestor(c, h) {
			return "it contains your home directory", true
		}
		for _, name := range []string{".ssh", ".aws", ".config", ".gnupg", ".docker"} {
			if contains(filepath.Join(h, name), c) {
				return "this is a sensitive path (" + filepath.Join(h, name) + ")", true
			}
		}
	}
	for _, p := range []string{"/etc", "/var/run", "/run", "/var/lib/docker"} {
		if contains(p, c) {
			return "this is a sensitive path (" + p + ")", true
		}
	}
	return "", false
}

// isAncestor reports whether parent is an ancestor of child.
func isAncestor(parent, child string) bool {
	return strings.HasPrefix(child+string(filepath.Separator), parent+string(filepath.Separator))
}

// contains reports whether path equals base or lives beneath it.
func contains(base, path string) bool {
	return path == base || isAncestor(base, path)
}

// Hash8 is the first 8 hex characters of sha256(s); it names project volumes
// (FR-5) and project labels (FR-3).
func Hash8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// Scan returns at most one notice line for the workspace root plus
// .git/hooks/ (non-recursive, bounded per NFR-1). It never blocks.
func Scan(dir string) []string {
	var items []string
	for _, name := range []string{".env", ".envrc", ".netrc"} {
		if fileExists(filepath.Join(dir, name)) {
			items = append(items, name)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "credentials*.json")); len(matches) > 0 {
		for _, m := range matches {
			items = append(items, filepath.Base(m))
		}
	}
	if hasLifecycleScripts(filepath.Join(dir, "package.json")) {
		items = append(items, "package.json lifecycle scripts")
	}
	if hooks := executableHooks(filepath.Join(dir, ".git", "hooks")); len(hooks) > 0 {
		items = append(items, "executable .git/hooks ("+strings.Join(hooks, ", ")+")")
	}
	if len(items) == 0 {
		return nil
	}
	notice := fmt.Sprintf(
		"note: workspace contains %s — the agent can read them, and agent writes there run on the host later\n  \u2192 see docs/isolation.md; suppress with --quiet",
		strings.Join(items, ", "))
	return []string{notice}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func hasLifecycleScripts(pkgPath string) bool {
	b, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return false
	}
	for _, key := range []string{"preinstall", "install", "postinstall", "prepare"} {
		if strings.TrimSpace(pkg.Scripts[key]) != "" {
			return true
		}
	}
	return false
}

func executableHooks(hooksDir string) []string {
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".sample") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&0o111 != 0 {
			out = append(out, e.Name())
		}
	}
	return out
}

// SELinuxRelabel returns "z" when the kernel reports SELinux enforcing,
// otherwise "". The mount relabel keeps workspace/state usable on
// Fedora/RHEL without manual flags (FR-2).
func SELinuxRelabel() string { return selinuxRelabelFrom("/") }

func selinuxRelabelFrom(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "sys", "fs", "selinux", "enforce"))
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(b)) == "1" {
		return "z"
	}
	return ""
}
