package cli_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"agentbox/internal/engine"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// goldenSpec is a fully fixed RunSpec: no host state leaks into the golden.
func goldenSpec() engine.RunSpec {
	return engine.RunSpec{
		Name:  "agentbox-testbox",
		Image: "agentbox-pi:dev",
		Cmd:   []string{"pi"},
		Env: []engine.EnvVar{
			{Name: "AGENTBOX_UID", Value: "1000", Set: true},
			{Name: "AGENTBOX_GID", Value: "1000", Set: true},
			{Name: "AGENTBOX_HOME", Value: "/state/home", Set: true},
			{Name: "PI_CODING_AGENT_DIR", Value: "/state/pi", Set: true},
			{Name: "ANTHROPIC_API_KEY", Set: true, Secret: true},
		},
		Workspace: engine.Mount{Host: "/home/me/src/app", Container: "/workspace"},
		State:     &engine.Mount{Host: "agentbox-state-1000-1a2b3c4d", Container: "/state"},
		Net:       "bridge",
		Rm:        true,
		Workdir:   "/workspace",
		CapDrop:   []string{"ALL"},
		CapAdd:    []string{"CHOWN", "SETUID", "SETGID"},
		NoNewPriv: true,
		Init:      true,
		Tmpfs:     []string{"/tmp:size=256m"},
		CPUs:      "2",
		Memory:    "4g",
		PidsLimit: 512,
		Labels: map[string]string{
			"agentbox": "1", "agentbox.run": "run-1",
			"agentbox.project": "app-1a2b3c4d", "agentbox.uid": "1000", "agentbox.owner": "42@boot-1",
		},
		Entrypoint: "/entrypoint.sh",
	}
}

func assertGolden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test ./tests/cli/ -run Golden -update` to create)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// AC 21: dry-run output is stable under a golden-file test with fixed labels
// and no TTY (so -it is omitted).
func TestDryRunGoldenDefault(t *testing.T) {
	spec := goldenSpec()
	spec.TTY = false
	argv := engine.NewDocker().DryRunArgv(spec, func(string) string { return "present" })
	assertGolden(t, "dryrun_default.golden", engine.Render(argv))
}

func TestDryRunGoldenReadOnlyNoNet(t *testing.T) {
	spec := goldenSpec()
	spec.ReadOnly = true
	spec.Workspace.ReadOnly = true
	spec.Net = "none"
	spec.User = "1000:1000"
	argv := engine.NewDocker().DryRunArgv(spec, func(string) string { return "" })
	assertGolden(t, "dryrun_ro_none.golden", engine.Render(argv))
}

func TestDryRunGoldenPodmanUserns(t *testing.T) {
	spec := goldenSpec()
	spec.Userns = "keep-id"
	argv := engine.NewPodman().DryRunArgv(spec, func(string) string { return "present" })
	assertGolden(t, "dryrun_podman.golden", engine.Render(argv))
}

func TestDryRunGoldenRelabelAndEphemeral(t *testing.T) {
	spec := goldenSpec()
	spec.Workspace.Relabel = "z"
	spec.State = nil
	spec.Env = append(spec.Env, engine.EnvVar{Name: "AWS_SECRET_ACCESS_KEY", Set: false})
	argv := engine.NewDocker().DryRunArgv(spec, func(string) string { return "present" })
	assertGolden(t, "dryrun_relabel_ephemeral.golden", engine.Render(argv))
}
