package run

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeCredFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCredentialMountsSingleFile(t *testing.T) {
	ad := testAdapter(t, "pi")
	file := writeCredFile(t, "auth.json")
	mounts, err := credentialMounts(ad, []string{file})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts = %v, want one", mounts)
	}
	if mounts[0].Host != file || mounts[0].Container != ad.CredFile || !mounts[0].ReadOnly {
		t.Errorf("mount = %+v", mounts[0])
	}
}

func TestCredentialMountsSameFileTwiceIsOK(t *testing.T) {
	ad := testAdapter(t, "pi")
	file := writeCredFile(t, "auth.json")
	mounts, err := credentialMounts(ad, []string{file, file})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Errorf("mounts = %v, want one (duplicate host file deduped)", mounts)
	}
}

func TestCredentialMountsRejectsSecondDistinctFile(t *testing.T) {
	ad := testAdapter(t, "pi")
	first := writeCredFile(t, "first.json")
	second := writeCredFile(t, "second.json")
	_, err := credentialMounts(ad, []string{first, second})
	if err == nil {
		t.Fatal("expected an error for two distinct credential files")
	}
	want := fmt.Sprintf("only one credential file can be mounted at %s; pass a single --credentials file", ad.CredFile)
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
