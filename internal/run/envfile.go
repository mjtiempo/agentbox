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

// ParseEnvFile reads KEY=value lines (FR-1 P1). Comments (#), blank lines, an
// optional `export ` prefix, and matching single/double quotes are handled;
// malformed lines without `=` are skipped.
func ParseEnvFile(path string) ([]KV, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read env file %q: %w", path, err)
	}
	defer f.Close()

	var kvs []KV
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
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
