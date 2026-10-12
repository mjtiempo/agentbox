package run

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// KV is one environment entry.
type KV struct {
	Key   string
	Value string
}

// reservedEnvPrefixes are key prefixes whose values would reconfigure or
// execute code through the container-engine CLI: env-file values are merged
// into the engine child process environment (ChildEnv -> Stdio.Env), so a
// DOCKER_*/LD_*/AGENTBOX_* key is an engine-process-env trust boundary, not a
// container variable.
var reservedEnvPrefixes = []string{
	"DOCKER_", "LD_", "DYLD_", "AGENTBOX_", "CONTAINERS_", "_CONTAINERS_", "PODMAN_", "BUILDAH_",
}

// reservedEnvNames are exact key names with the same engine-process-env trust
// boundary as reservedEnvPrefixes.
var reservedEnvNames = map[string]bool{
	"PATH": true, "HOME": true, "SHELL": true, "ENV": true, "BASH_ENV": true,
	"IFS": true, "CDPATH": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
	"XDG_RUNTIME_DIR": true, "REGISTRY_AUTH_FILE": true,
	"STORAGE_DRIVER": true, "STORAGE_OPTS": true,
}

// validEnvName reports whether key is a shell variable name ([A-Za-z_][A-Za-z0-9_]*).
func validEnvName(key string) bool {
	for i, r := range key {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return key != ""
}

// reservedEnvName reports whether key would reconfigure the container engine.
func reservedEnvName(key string) bool {
	for _, prefix := range reservedEnvPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return reservedEnvNames[key]
}

// ParseEnvFile reads KEY=value lines (FR-1 P1). Comments (#), blank lines, an
// optional `export ` prefix, and matching single/double quotes are handled;
// malformed lines without `=` are skipped. Keys are validated against the
// engine-process-env trust boundary; errors never echo a value.
func ParseEnvFile(path string) ([]KV, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read env file %q: %w", path, err)
	}
	defer f.Close()

	var kvs []KV
	sc := bufio.NewScanner(f)
	lineNumber := 0
	for sc.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && (strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")) {
			line = strings.TrimSpace(rest)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if !validEnvName(key) {
			return nil, fmt.Errorf("env file %q line %d: key %q is not a valid variable name", path, lineNumber, key)
		}
		if reservedEnvName(key) {
			return nil, fmt.Errorf("env file %q line %d: refusing reserved key %q (it would reconfigure the container engine)", path, lineNumber, key)
		}
		value = strings.TrimSpace(value)
		value = trimQuotes(value)
		kvs = append(kvs, KV{Key: key, Value: value})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("cannot read env file %q: %w", path, err)
	}
	return kvs, nil
}

func trimQuotes(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// ChildEnv merges env-file values over the host environment (env-file wins),
// preserving host entries that are not overridden.
func ChildEnv(base []string, kvs []KV) []string {
	out := append([]string{}, base...)
	index := map[string]int{}
	for i, e := range out {
		key, _, _ := strings.Cut(e, "=")
		index[key] = i
	}
	for _, kv := range kvs {
		entry := kv.Key + "=" + kv.Value
		if i, ok := index[kv.Key]; ok {
			out[i] = entry
			continue
		}
		index[kv.Key] = len(out)
		out = append(out, entry)
	}
	return out
}
