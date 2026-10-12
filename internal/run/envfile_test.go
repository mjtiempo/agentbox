package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseEnvFileValidNames(t *testing.T) {
	for _, key := range []string{"FOO", "_FOO", "FOO_BAR_123", "a", "_", "A1"} {
		path := writeEnvFile(t, key+"=value\n")
		kvs, err := ParseEnvFile(path)
		if err != nil {
			t.Errorf("ParseEnvFile(%q): %v", key, err)
			continue
		}
		if len(kvs) != 1 || kvs[0] != (KV{Key: key, Value: "value"}) {
			t.Errorf("ParseEnvFile(%q) = %v", key, kvs)
		}
	}
}

func TestParseEnvFileRejectsInvalidNames(t *testing.T) {
	for _, key := range []string{"1FOO", "-FOO", ".FOO", "FOO-BAR", "FOO.BAR", "FOO BAR"} {
		path := writeEnvFile(t, key+"=value\n")
		_, err := ParseEnvFile(path)
		if err == nil {
			t.Errorf("ParseEnvFile(%q) accepted invalid key", key)
			continue
		}
		if !strings.Contains(err.Error(), "is not a valid variable name") {
			t.Errorf("ParseEnvFile(%q) error = %v", key, err)
		}
		if strings.Contains(err.Error(), "value") {
			t.Errorf("ParseEnvFile(%q) leaked a value: %v", key, err)
		}
	}
}

func TestParseEnvFileStripsBOM(t *testing.T) {
	path := writeEnvFile(t, "\uFEFFFOO=bar\n")
	kvs, err := ParseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(kvs) != 1 || kvs[0] != (KV{Key: "FOO", Value: "bar"}) {
		t.Errorf("kvs = %v, want FOO=bar", kvs)
	}
}

func TestParseEnvFileExportSeparator(t *testing.T) {
	path := writeEnvFile(t, "export FOO=bar\nexport\tBAZ=qux\n")
	kvs, err := ParseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{{Key: "FOO", Value: "bar"}, {Key: "BAZ", Value: "qux"}}
	if len(kvs) != len(want) {
		t.Fatalf("kvs = %v, want %v", kvs, want)
	}
	for i := range want {
		if kvs[i] != want[i] {
			t.Errorf("kv[%d] = %v, want %v", i, kvs[i], want[i])
		}
	}
}

func TestParseEnvFileRejectsReservedKeys(t *testing.T) {
	reserved := []string{
		"DOCKER_HOST", "DOCKER_CONFIG", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES",
		"AGENTBOX_ENGINE", "CONTAINERS_CONF", "_CONTAINERS_USERNS_CONFIGURED",
		"PODMAN_USERNS", "BUILDAH_FORMAT", "STORAGE_DRIVER", "STORAGE_OPTS",
		"PATH", "HOME", "SHELL", "ENV", "BASH_ENV", "IFS", "CDPATH",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "REGISTRY_AUTH_FILE",
	}
	for _, key := range reserved {
		path := writeEnvFile(t, key+"=value\n")
		_, err := ParseEnvFile(path)
		if err == nil {
			t.Errorf("ParseEnvFile(%q) accepted reserved key", key)
			continue
		}
		if !strings.Contains(err.Error(), "refusing reserved key") {
			t.Errorf("ParseEnvFile(%q) error = %v", key, err)
		}
		if strings.Contains(err.Error(), "value") {
			t.Errorf("ParseEnvFile(%q) leaked a value: %v", key, err)
		}
	}
}

func TestParseEnvFileAllowsLowercaseReservedNames(t *testing.T) {
	for _, key := range []string{"path", "docker_host", "home"} {
		path := writeEnvFile(t, key+"=value\n")
		kvs, err := ParseEnvFile(path)
		if err != nil {
			t.Errorf("ParseEnvFile(%q): %v", key, err)
			continue
		}
		if len(kvs) != 1 || kvs[0].Key != key {
			t.Errorf("ParseEnvFile(%q) = %v", key, kvs)
		}
	}
}

func TestParseEnvFileErrorNamesPathAndLine(t *testing.T) {
	path := writeEnvFile(t, "# comment\n\nexport GOOD=1\nDOCKER_HOST=tcp://evil\n")
	_, err := ParseEnvFile(path)
	if err == nil {
		t.Fatal("expected reserved-key error")
	}
	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Errorf("error missing path %q: %v", path, err)
	}
	if !strings.Contains(msg, "line 4") {
		t.Errorf("error missing correct line number: %v", err)
	}
	if strings.Contains(msg, "tcp://evil") {
		t.Errorf("error leaked a value: %v", err)
	}
}

func TestParseEnvFileCommentsBlankExportQuotes(t *testing.T) {
	path := writeEnvFile(t, "# comment\n\nno equals here\nexport A=1\nB=\"two words\"\nC='three'\nD=pl'ain\n")
	kvs, err := ParseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{{Key: "A", Value: "1"}, {Key: "B", Value: "two words"}, {Key: "C", Value: "three"}, {Key: "D", Value: "pl'ain"}}
	if len(kvs) != len(want) {
		t.Fatalf("kvs = %v, want %v", kvs, want)
	}
	for i := range want {
		if kvs[i] != want[i] {
			t.Errorf("kv[%d] = %v, want %v", i, kvs[i], want[i])
		}
	}
}
