package state_test

import (
	"strings"
	"testing"

	"agentbox/internal/state"
	"agentbox/internal/workspace"
)

func TestVolumeNameDeterministic(t *testing.T) {
	a := state.VolumeName(1000, "/home/me/src/app")
	b := state.VolumeName(1000, "/home/me/src/app")
	if a != b {
		t.Errorf("same path must give same volume: %q vs %q", a, b)
	}
	want := "agentbox-state-1000-" + workspace.Hash8("/home/me/src/app")
	if a != want {
		t.Errorf("VolumeName = %q, want %q", a, want)
	}
	if c := state.VolumeName(1000, "/home/me/src/other"); c == a {
		t.Errorf("different paths must differ: %q", c)
	}
	if d := state.VolumeName(1001, "/home/me/src/app"); d == a {
		t.Errorf("different uids must differ: %q", d)
	}
	if !strings.HasPrefix(a, "agentbox-state-1000-") {
		t.Errorf("prefix wrong: %q", a)
	}
}

func TestMountSpec(t *testing.T) {
	m := state.VolumeMount(1000, "/home/me/src/app", "")
	if m.Container != "/state" {
		t.Errorf("Container = %q", m.Container)
	}
	if m.Host != state.VolumeName(1000, "/home/me/src/app") {
		t.Errorf("Host = %q", m.Host)
	}
	if m.ReadOnly {
		t.Error("state must be writable")
	}
	// SELinux relabel is threaded through.
	m = state.VolumeMount(1000, "/home/me/src/app", "z")
	if m.Relabel != "z" {
		t.Errorf("Relabel = %q", m.Relabel)
	}
}

func TestDirMount(t *testing.T) {
	m := state.DirMount("/host/state", "z")
	if m.Host != "/host/state" || m.Container != "/state" || m.Relabel != "z" {
		t.Errorf("DirMount = %+v", m)
	}
}

func TestConcurrentWarning(t *testing.T) {
	w := state.ConcurrentWarning("app-1a2b3c4d")
	for _, want := range []string{"app-1a2b3c4d", "--ephemeral", "--state-dir"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q missing %q", w, want)
		}
	}
}

func TestVolumeLabels(t *testing.T) {
	l := state.VolumeLabels(1000, "app-1a2b3c4d", "/home/me/src/app")
	if l["agentbox"] != "1" || l["agentbox.uid"] != "1000" ||
		l["agentbox.project"] != "app-1a2b3c4d" || l["agentbox.path"] != "/home/me/src/app" {
		t.Errorf("labels = %v", l)
	}
}

func TestParseVolumeList(t *testing.T) {
	out := strings.Join([]string{
		`agentbox-state-1000-aaaa1111	1000	app-aaaa1111	/home/me/src/app	123456`,
		`agentbox-state-1000-bbbb2222	1000	other-bbbb2222	/home/me/src/other	42`,
		`unrelated-volume		`,
	}, "\n")
	vols := state.ParseVolumeList(out)
	if len(vols) != 2 {
		t.Fatalf("parsed %d volumes, want 2: %+v", len(vols), vols)
	}
	if vols[0].Name != "agentbox-state-1000-aaaa1111" || vols[0].Path != "/home/me/src/app" || vols[0].SizeBytes != 123456 {
		t.Errorf("vols[0] = %+v", vols[0])
	}
	if vols[1].Project != "other-bbbb2222" {
		t.Errorf("vols[1] = %+v", vols[1])
	}
}
