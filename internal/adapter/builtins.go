package adapter

import (
	"fmt"
	"sort"
	"strings"

	"agentbox/internal/engine"
)

// Pinned harness versions (NFR-8). Keep in sync with the Dockerfiles, the
// Makefile image tags, and the release manifest.
//
// opencode v2 ships as @opencode/cli (the opencode-ai package stays on the
// 1.x line). The adapter tracks the current v2 CLI; v1 remains reachable with
// `--agent image:<old agentbox-opencode tag>` or a custom image.
const (
	PiVersion       = "1.1.0"
	ClaudeVersion   = "2.1.296"
	OpencodeVersion = "2.0.26"
)

// imageTag renders the FR-10 tag: agentbox-<harness>:<agentboxVer>-<harness><harnessVer>.
func imageTag(harness, agentboxVersion, harnessVersion string) string {
	if agentboxVersion == "" {
		agentboxVersion = "dev"
	}
	if harnessVersion == "" {
		return fmt.Sprintf("agentbox-%s:%s", harness, agentboxVersion)
	}
	return fmt.Sprintf("agentbox-%s:%s-%s%s", harness, agentboxVersion, harness, harnessVersion)
}

// Builtins returns the four built-in adapters for the given agentbox version.
func Builtins(agentboxVersion string) map[string]Adapter {
	return map[string]Adapter{
		"pi": {
			Name:  "pi",
			Image: imageTag("pi", agentboxVersion, PiVersion),
			Env: []engine.EnvVar{
				{Name: "PI_CODING_AGENT_DIR", Value: "/state/pi", Set: true},
				// pi's extension loader (jiti) caches transpiled packages under
				// $TMPDIR/jiti. The container's /tmp is a fresh tmpfs every run,
				// so without this every start re-transpiles all configured
				// packages (several seconds of CPU). Point TMPDIR at the
				// persistent per-project state so the cache survives.
				{Name: "TMPDIR", Value: "/state/home/.cache/tmp", Set: true},
				{Name: "JITI_RESPECT_TMPDIR_ENV", Value: "1", Set: true},
			},
			CredEnv:       piCredEnv(),
			CredFile:      "/state/pi/auth.json",
			HostCredFile:  ".pi/agent/auth.json",
			DefaultArgs:   []string{"pi"},
			EphemeralArgs: []string{"--no-session"},
			UseEntrypoint: true,
			ConfigDir:     "/state/pi",
			LoginHint:     "/login",
			UserDirs:      []string{"extensions", "skills", "themes"},
			UserExclude:   []string{"sessions", "auth.json", "run-history.jsonl", "crashes.json"},
			HostConfigDir: ".pi/agent",
			WarmupCache:   "/state/home/.cache/tmp",
		},
		"shell": {
			Name:          "shell",
			Image:         imageTag("base", agentboxVersion, ""),
			DefaultArgs:   []string{"bash"},
			UseEntrypoint: true,
		},
		"claude": {
			Name:          "claude",
			Image:         imageTag("claude", agentboxVersion, ClaudeVersion),
			Env:           []engine.EnvVar{{Name: "CLAUDE_CONFIG_DIR", Value: "/state/claude", Set: true}},
			CredEnv:       claudeCredEnv(),
			CredFile:      "/state/claude/.credentials.json",
			HostCredFile:  ".claude/.credentials.json",
			DefaultArgs:   []string{"claude"},
			UseEntrypoint: true,
			ConfigDir:     "/state/claude",
			LoginHint:     "/login",
			UserDirs:      []string{"plugins", "skills", "themes"},
			UserExclude: []string{
				"projects", "sessions", "history.jsonl", ".credentials.json",
				"stats-cache.json", "shell-snapshots", "todos", "debug", "ide",
				"backups", "cache",
			},
			HostConfigDir: ".claude",
		},
		"opencode": {
			Name:  "opencode",
			Image: imageTag("opencode", agentboxVersion, OpencodeVersion),
			Env: []engine.EnvVar{
				{Name: "XDG_CONFIG_HOME", Value: "/state/opencode/config", Set: true},
				{Name: "XDG_DATA_HOME", Value: "/state/opencode/data", Set: true},
				{Name: "XDG_STATE_HOME", Value: "/state/opencode/state", Set: true},
				{Name: "XDG_CACHE_HOME", Value: "/state/opencode/cache", Set: true},
				// Bun extracts a native render library and dlopens it from
				// $TMPDIR; the container's /tmp is a noexec tmpfs, so point the
				// temp dir at the exec-capable state volume instead of relaxing
				// the /tmp mount.
				{Name: "TMPDIR", Value: "/state/home/.cache/tmp", Set: true},
			},
			CredEnv: baseCredEnv(),
			// opencode v2 keeps credentials in its SQLite DB (per project).
			// --credentials mounts `opencode auth export` output here; import
			// it once per project with:
			//   agentbox --agent opencode --credentials <file> --
			//     auth import /state/opencode/credentials.json
			CredFile:       "/state/opencode/credentials.json",
			CredImportArgs: []string{"auth", "import"},
			HostCredExport: []string{"opencode", "auth", "export"},
			LoginArgs:      []string{"auth", "login"},
			CredExportArgs: []string{"auth", "export"},
			// service.json is opencode's local service state and carries a
			// generated password: never copy it into the user folder.
			UserExclude: []string{"service.json"},
			// --standalone runs a private server instead of spawning the
			// background service: in an ephemeral container the service is
			// pointless and costs ~5s on every warm start. opencode rejects the
			// flag before a subcommand, so it goes in TrailerArgs (after user
			// args), which works for both the TUI and subcommands.
			DefaultArgs:   []string{"opencode"},
			TrailerArgs:   []string{"--standalone"},
			UseEntrypoint: true,
			ConfigDir:     "/state/opencode/config",
			UserDirs:      []string{"plugins", "skills", "agents", "commands"},
			HostConfigXDG: "opencode",
		},
	}
}

// Names lists the built-in adapter names, sorted.
func Names() []string {
	names := []string{"pi", "shell", "claude", "opencode"}
	sort.Strings(names)
	return names
}

// Get resolves an adapter name, including the image:<ref> form (FR-4 P1).
func Get(name, agentboxVersion string) (Adapter, error) {
	if ref, ok := strings.CutPrefix(name, "image:"); ok {
		if strings.TrimSpace(ref) == "" {
			return Adapter{}, fmt.Errorf(`agent name %q is missing the image reference (use image:<ref>)`, name)
		}
		return Adapter{
			Name:          name,
			Image:         ref,
			UseEntrypoint: false, // the image's own entrypoint runs (FR-9 P1)
		}, nil
	}
	if a, ok := Builtins(agentboxVersion)[name]; ok {
		return a, nil
	}
	return Adapter{}, fmt.Errorf("unknown agent %q (valid: %s, image:<ref>)", name, strings.Join(Names(), ", "))
}
