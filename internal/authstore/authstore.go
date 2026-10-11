// Package authstore manages credentials adopted from the host
// (`agentbox auth copy <harness>`) so later runs use them automatically.
//
// Stored files live under <userRoot>/credentials/<harness>.json with 0600
// permissions and are mounted read-only at the adapter's credential target.
// Nothing is adopted implicitly: the user runs `auth copy` once.
package authstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"agentbox/internal/adapter"
)

// Env is the host environment lookup.
type Env func(string) string

// Runner executes a host command and returns its stdout (used for
// `opencode auth export`).
type Runner func(name string, args ...string) ([]byte, error)

// Dir returns the credentials directory for a user root.
func Dir(userRoot string) string { return filepath.Join(userRoot, "credentials") }

// Path returns the stored credential file for a harness.
func Path(userRoot, harness string) string {
	return filepath.Join(Dir(userRoot), harness+".json")
}

// Store writes credential bytes with restrictive permissions.
func Store(userRoot, harness string, data []byte) (string, error) {
	if userRoot == "" {
		return "", fmt.Errorf("cannot locate the agentbox user folder (HOME is unset)")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("refusing to store empty credentials")
	}
	if err := os.MkdirAll(Dir(userRoot), 0o700); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", Dir(userRoot), err)
	}
	p := Path(userRoot, harness)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", p, err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		return "", fmt.Errorf("cannot secure %s: %w", p, err)
	}
	return p, nil
}

// Load returns the stored credential for a harness. ok is false when absent.
func Load(userRoot, harness string) (data []byte, path string, ok bool, err error) {
	if userRoot == "" {
		return nil, "", false, nil
	}
	p := Path(userRoot, harness)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, p, false, nil
		}
		return nil, p, false, err
	}
	return b, p, true, nil
}

// Reset removes the stored credential. It reports whether one existed.
func Reset(userRoot, harness string) (bool, error) {
	if userRoot == "" {
		return false, fmt.Errorf("cannot locate the agentbox user folder (HOME is unset)")
	}
	p := Path(userRoot, harness)
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Source describes where a credential was adopted from.
type Source struct {
	Kind string // "file" | "command"
	Ref  string // host path or command line
}

// Adopt reads the harness's host credential: the host credential file, or the
// stdout of the harness's export command (opencode keeps credentials in its
// database, so it must be asked to export them).
func Adopt(ad adapter.Adapter, getenv Env, run Runner) ([]byte, Source, error) {
	if len(ad.HostCredExport) > 0 {
		if run == nil {
			return nil, Source{}, fmt.Errorf("cannot run %s on the host", ad.HostCredExport[0])
		}
		out, err := run(ad.HostCredExport[0], ad.HostCredExport[1:]...)
		if err != nil {
			return nil, Source{}, fmt.Errorf("%s failed: %w", ad.HostCredExport[0], err)
		}
		if len(out) == 0 {
			return nil, Source{}, fmt.Errorf("%s produced no credentials (log in on the host first)", ad.HostCredExport[0])
		}
		var probe any
		if err := json.Unmarshal(out, &probe); err != nil {
			return nil, Source{}, fmt.Errorf("%s did not produce JSON credentials", ad.HostCredExport[0])
		}
		return out, Source{Kind: "command", Ref: ad.HostCredExport[0]}, nil
	}
	if ad.HostCredFile == "" {
		return nil, Source{}, fmt.Errorf("harness %q has no host credential file to copy", ad.Name)
	}
	home := getenv("HOME")
	if home == "" {
		return nil, Source{}, fmt.Errorf("cannot locate the %s credential: HOME is unset", ad.Name)
	}
	p := ad.HostCredFile
	if !filepath.IsAbs(p) {
		p = filepath.Join(home, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Source{}, fmt.Errorf("no %s credential found at %s (log in on the host first)", ad.Name, p)
		}
		return nil, Source{}, fmt.Errorf("cannot read %s: %w", p, err)
	}
	return b, Source{Kind: "file", Ref: p}, nil
}
