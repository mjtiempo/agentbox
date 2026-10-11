package adapter

import (
	"fmt"
	"strings"

	"agentbox/internal/engine"
)

// baseCredEnv is the FR-6 allowlist shared by the harness adapters: provider
// keys, proxies (both cases), and corporate-CA variables.
func baseCredEnv() []string {
	return []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_OAUTH_TOKEN",
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"OPENROUTER_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
		"SSL_CERT_FILE",
		"NODE_EXTRA_CA_CERTS",
	}
}

func piCredEnv() []string { return baseCredEnv() }

func claudeCredEnv() []string {
	return append(baseCredEnv(), "CLAUDE_CODE_OAUTH_TOKEN")
}

// pathValuedVars are allowlist entries whose value is a host path that may not
// exist inside the adapter image (FR-6: one-line warning).
func pathValuedVars(name string) bool {
	switch name {
	case "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS":
		return true
	}
	return false
}

// BuildEnv assembles the container environment:
//
//   - only the adapter's CredEnv allowlist is considered; the host env is
//     never inherited wholesale (FR-6)
//   - allowlist entries unset on the host are omitted entirely
//   - every value is passed as a bare `-e KEY` (Secret=true), so values never
//     appear in engine argv or host process listings
//   - `--env KEY` entries are appended; an unset KEY is skipped with a notice
//
// It returns the env vars plus one-line notices and warnings for the caller
// to print.
func BuildEnv(a Adapter, getenv func(string) string, extra []string) (envs []engine.EnvVar, notices, warns []string) {
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		envs = append(envs, engine.EnvVar{Name: name, Set: true, Secret: true})
		if pathValuedVars(name) {
			warns = append(warns, fmt.Sprintf(
				"%s is a host path that may not exist in the %s image — use --credentials or a custom image with the CA baked in",
				name, a.Name))
		}
	}
	for _, name := range a.CredEnv {
		if strings.TrimSpace(getenv(name)) != "" {
			add(name)
		}
	}
	for _, name := range extra {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(getenv(name)) == "" {
			notices = append(notices, fmt.Sprintf("%s is not set on the host; skipping --env %s", name, name))
			continue
		}
		add(name)
	}
	return envs, notices, warns
}
