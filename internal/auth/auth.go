// Package auth detects which credential mechanism is available for a harness
// without ever printing secret values (FR-6 P1).
package auth

import (
	"fmt"
	"path/filepath"
	"strings"

	"agentbox/internal/adapter"
)

// Result is a credential detection outcome.
type Result struct {
	Mechanism string // "environment", "environment (oauth)", "credential file", "state volume", "none"
	Detail    string // the env var name or path; never a value
}

// Probes are the host-side checks (injectable for tests).
type Probes struct {
	Getenv         func(string) string
	CredFileExists func(path string) bool
	StateExists    func() bool
	// StoredCredential reports a credential adopted with
	// `agentbox auth copy <harness>` (path, true). Checked after env keys and
	// before the host credential file, because it is what runs actually use.
	StoredCredential func() (string, bool)
}

// Detect checks mechanisms in order: provider env key, OAuth env token,
// credential file, state volume, none.
func Detect(a adapter.Adapter, p Probes) Result {
	if p.Getenv != nil {
		for _, k := range a.CredEnv {
			if strings.TrimSpace(p.Getenv(k)) == "" {
				continue
			}
			if strings.Contains(k, "OAUTH") {
				return Result{Mechanism: "environment (oauth)", Detail: k}
			}
			if isProviderKey(k) {
				return Result{Mechanism: "environment", Detail: k}
			}
		}
	}
	if p.StoredCredential != nil {
		if path, ok := p.StoredCredential(); ok {
			return Result{Mechanism: "agentbox credential store", Detail: path}
		}
	}
	if credPath := hostCredPath(a, p); credPath != "" && p.CredFileExists != nil && p.CredFileExists(credPath) {
		return Result{Mechanism: "credential file", Detail: credPath}
	}
	if p.StateExists != nil && p.StateExists() {
		return Result{Mechanism: "state volume", Detail: "/state"}
	}
	return Result{Mechanism: "none"}
}

// hostCredPath resolves the adapter's host credential file (relative to HOME)
// to an absolute path. It is empty when the harness keeps credentials
// elsewhere (opencode stores them in its state database).
func hostCredPath(a adapter.Adapter, p Probes) string {
	if a.HostCredFile == "" {
		return ""
	}
	if filepath.IsAbs(a.HostCredFile) {
		return a.HostCredFile
	}
	if p.Getenv == nil {
		return ""
	}
	home := p.Getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, a.HostCredFile)
}

// Line renders the detection result for `agentbox auth <harness>`.
func (r Result) Line(harness string) string {
	if r.Mechanism == "none" {
		return fmt.Sprintf("harness: %s\nmechanism: none\n  \u2192 no env key, credential file, or state volume detected; export a provider key, use --credentials, or log in inside the container", harness)
	}
	return fmt.Sprintf("harness: %s\nmechanism: %s\nsource: %s", harness, r.Mechanism, r.Detail)
}

func isProviderKey(k string) bool {
	for _, prefix := range []string{"ANTHROPIC", "OPENAI", "OPENROUTER", "GEMINI", "GOOGLE"} {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// PrimaryProviderKeys returns up to two provider keys for the preflight notice.
func PrimaryProviderKeys(a adapter.Adapter) []string {
	var out []string
	for _, k := range a.CredEnv {
		if isProviderKey(k) {
			out = append(out, k)
			if len(out) == 2 {
				break
			}
		}
	}
	return out
}

// PreflightNotice is the §8.2-style notice printed when a harness has no
// credential mechanism detected. It never contains values.
func PreflightNotice(a adapter.Adapter) string {
	keys := PrimaryProviderKeys(a)
	if len(keys) >= 2 {
		return fmt.Sprintf("%s and %s are unset for harness %q\n  \u2192 export one, pass --env, or run: agentbox auth %s",
			keys[0], keys[1], a.Name, a.Name)
	}
	if len(keys) == 1 {
		return fmt.Sprintf("%s is unset for harness %q\n  \u2192 export it, pass --env, or run: agentbox auth %s",
			keys[0], a.Name, a.Name)
	}
	return fmt.Sprintf("no credentials detected for harness %q\n  \u2192 pass --env or --credentials, or run: agentbox auth %s",
		a.Name, a.Name)
}
