package engine_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"agentbox/internal/engine"
)

func baseSpec() engine.RunSpec {
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

// indexOf returns the index of needle in argv, or -1.
func indexOf(argv []string, needle string) int {
	for i, a := range argv {
		if a == needle {
			return i
		}
	}
	return -1
}

func mustIndexAfter(t *testing.T, argv []string, a, b string) {
	t.Helper()
	ia, ib := indexOf(argv, a), indexOf(argv, b)
	if ia < 0 || ib < 0 {
		t.Fatalf("argv missing %q (%d) or %q (%d): %v", a, ia, b, ib, argv)
	}
	if ia >= ib {
		t.Errorf("%q must come before %q: %v", a, b, argv)
	}
}

func TestRunArgvOrderingIsFrozen(t *testing.T) {
	argv := engine.NewDocker().RunArgv(baseSpec())
	wantPrefix := []string{"docker", "run"}
	if !reflect.DeepEqual(argv[:2], wantPrefix) {
		t.Fatalf("argv must start %v, got %v", wantPrefix, argv[:2])
	}
	order := []string{
		"--rm", "--init", "--name", "-w", "--label",
		"--net", "--cap-drop", "--cap-add", "--security-opt",
		"--cpus", "--memory", "--pids-limit", "--tmpfs", "-v",
		"-e", "--entrypoint",
	}
	last := 0
	for _, token := range order {
		i := indexOf(argv, token)
		if i < 0 {
			t.Fatalf("argv missing %q: %v", token, argv)
		}
		if i < last {
			t.Errorf("section %q out of order: %v", token, argv)
		}
		last = i
	}
	if argv[len(argv)-2] != "agentbox-pi:dev" || argv[len(argv)-1] != "pi" {
		t.Errorf("image and cmd must be last: %v", argv[len(argv)-3:])
	}
	// Exact label block, canonical order (PRD §8.1).
	lb := indexOf(argv, "--label")
	gotLabels := []string{}
	for i := lb; i < len(argv)-1 && argv[i] == "--label"; i += 2 {
		gotLabels = append(gotLabels, argv[i+1])
	}
	wantLabels := []string{
		"agentbox=1", "agentbox.run=run-1", "agentbox.project=app-1a2b3c4d",
		"agentbox.uid=1000", "agentbox.owner=42@boot-1",
	}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Errorf("labels = %v, want %v", gotLabels, wantLabels)
	}
	mustIndexAfter(t, argv, "--name", "--label")
	mustIndexAfter(t, argv, "--cap-drop", "--cap-add")
	mustIndexAfter(t, argv, "--net", "--cap-drop")
	mustIndexAfter(t, argv, "-v", "-e")
	mustIndexAfter(t, argv, "-e", "--entrypoint")
}

func TestRunArgvWorkdirAndMounts(t *testing.T) {
	argv := engine.NewDocker().RunArgv(baseSpec())
	w := indexOf(argv, "-w")
	if w < 0 || argv[w+1] != "/workspace" {
		t.Errorf("-w /workspace missing: %v", argv)
	}
	v := indexOf(argv, "-v")
	if argv[v+1] != "/home/me/src/app:/workspace:rw" {
		t.Errorf("workspace mount = %q", argv[v+1])
	}
	if argv[v+3] != "agentbox-state-1000-1a2b3c4d:/state" {
		t.Errorf("state mount = %q", argv[v+3])
	}
}

func TestRunArgvReadOnlyAndRelabel(t *testing.T) {
	s := baseSpec()
	s.ReadOnly = true
	s.Workspace.ReadOnly = true
	s.Workspace.Relabel = "z"
	s.State.Relabel = "z"
	argv := engine.NewDocker().RunArgv(s)
	v := indexOf(argv, "-v")
	if argv[v+1] != "/home/me/src/app:/workspace:z,ro" {
		t.Errorf("ro+relabel workspace mount = %q", argv[v+1])
	}
	if argv[v+3] != "agentbox-state-1000-1a2b3c4d:/state:z" {
		t.Errorf("relabel state mount = %q", argv[v+3])
	}
}

func TestRunArgvEphemeralHasNoStateMount(t *testing.T) {
	s := baseSpec()
	s.State = nil
	argv := engine.NewDocker().RunArgv(s)
	for _, a := range argv {
		if strings.Contains(a, ":/state") {
			t.Errorf("ephemeral run must not mount /state: %v", argv)
		}
	}
}

func TestRunArgvTTY(t *testing.T) {
	s := baseSpec()
	s.TTY = true
	argv := engine.NewDocker().RunArgv(s)
	if indexOf(argv, "-it") < 0 {
		t.Errorf("TTY run must include -it: %v", argv)
	}
	s.TTY = false
	if indexOf(engine.NewDocker().RunArgv(s), "-it") >= 0 {
		t.Error("non-TTY run must omit -it")
	}
}

func TestRunArgvIdentity(t *testing.T) {
	s := baseSpec()
	s.User = "1000:1000"
	s.Userns = "keep-id"
	argv := engine.NewDocker().RunArgv(s)
	u := indexOf(argv, "--user")
	if u < 0 || argv[u+1] != "1000:1000" {
		t.Errorf("--user missing: %v", argv)
	}
	ns := indexOf(argv, "--userns=keep-id")
	if ns < 0 {
		t.Errorf("--userns=keep-id missing: %v", argv)
	}
	mustIndexAfter(t, argv, "--user", "--userns=keep-id")
	mustIndexAfter(t, argv, "--userns=keep-id", "--net")
}

func TestEnvRenderingRules(t *testing.T) {
	s := baseSpec()
	s.Env = []engine.EnvVar{
		{Name: "UNSET_VAR", Set: false},               // omit entirely
		{Name: "BARE_VAR", Set: true},                 // bare -e
		{Name: "LITERAL_VAR", Value: "v1", Set: true}, // -e NAME=value
		{Name: "SECRET_VAR", Value: "leaked", Set: true, Secret: true},
	}
	argv := engine.NewDocker().RunArgv(s)
	if indexOf(argv, "UNSET_VAR") >= 0 || indexOf(argv, "UNSET_VAR=") >= 0 {
		t.Errorf("Set=false must be omitted: %v", argv)
	}
	if indexOf(argv, "BARE_VAR") < 0 {
		t.Errorf("bare form missing: %v", argv)
	}
	if indexOf(argv, "LITERAL_VAR=v1") < 0 {
		t.Errorf("literal form missing: %v", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "leaked") {
			t.Fatalf("secret value leaked into argv: %v", argv)
		}
	}
	if indexOf(argv, "SECRET_VAR") < 0 {
		t.Errorf("secret must still be passed as bare -e: %v", argv)
	}
}

func TestDryRunArgvRedactsSecrets(t *testing.T) {
	s := baseSpec()
	argv := engine.NewDocker().DryRunArgv(s, func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "sk-real-value"
		}
		return ""
	})
	if indexOf(argv, "ANTHROPIC_API_KEY=<set>") < 0 {
		t.Errorf("dry-run must render set secrets as <set>: %v", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "sk-real-value") {
			t.Fatalf("secret leaked in dry-run: %v", argv)
		}
	}
	argv = engine.NewDocker().DryRunArgv(s, func(string) string { return "" })
	if indexOf(argv, "ANTHROPIC_API_KEY=<unset>") < 0 {
		t.Errorf("dry-run must render absent secrets as <unset>: %v", argv)
	}
	// Literal values are not redacted.
	if indexOf(argv, "PI_CODING_AGENT_DIR=/state/pi") < 0 {
		t.Errorf("literal env must be shown: %v", argv)
	}
}

func TestRunArgvExtraMountsReadOnly(t *testing.T) {
	s := baseSpec()
	s.ExtraMounts = []engine.Mount{
		{Host: "/home/me/.pi/auth.json", Container: "/state/pi/auth.json", ReadOnly: true},
	}
	argv := engine.NewDocker().RunArgv(s)
	v := indexOf(argv, "-v")
	found := false
	for i := v; i < len(argv)-1; i++ {
		if argv[i] == "-v" && argv[i+1] == "/home/me/.pi/auth.json:/state/pi/auth.json:ro" {
			found = true
		}
	}
	if !found {
		t.Errorf("credential mount must render :ro after state: %v", argv)
	}
	// Credential mounts come after the state mount.
	stateIdx, credIdx := -1, -1
	for i, a := range argv {
		if a == "agentbox-state-1000-1a2b3c4d:/state" {
			stateIdx = i
		}
		if a == "/home/me/.pi/auth.json:/state/pi/auth.json:ro" {
			credIdx = i
		}
	}
	if stateIdx < 0 || credIdx < 0 || credIdx < stateIdx {
		t.Errorf("mount order wrong: state=%d cred=%d %v", stateIdx, credIdx, argv)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"simple":           "simple",
		"/home/me/src/app": "/home/me/src/app",
		"a b":              "'a b'",
		"it's":             `'it'\''s'`,
		"":                 "''",
		"--flag=x":         "--flag=x",
		"$HOME":            "'$HOME'",
		"a:b,c=d":          "a:b,c=d",
		"line\nbreak":      "'line\nbreak'",
	}
	for in, want := range cases {
		if got := engine.ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderOneTokenPerLine(t *testing.T) {
	argv := []string{"docker", "run", "-e", "X=a b"}
	got := engine.Render(argv)
	want := "docker\nrun\n-e\n'X=a b'\n"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestPodmanGrammarSharesBuilder(t *testing.T) {
	s := baseSpec()
	s.Userns = "keep-id"
	dockerArgv := engine.NewDocker().RunArgv(s)
	podmanArgv := engine.NewPodman().RunArgv(s)
	if dockerArgv[0] != "docker" || podmanArgv[0] != "podman" {
		t.Fatalf("engine names: %v / %v", dockerArgv[0], podmanArgv[0])
	}
	if !reflect.DeepEqual(dockerArgv[1:], podmanArgv[1:]) {
		t.Errorf("engines must share the builder:\n%v\n%v", dockerArgv[1:], podmanArgv[1:])
	}
}

func TestMapExitCode(t *testing.T) {
	cases := []struct {
		code     int
		wantCode int
		wantHint bool
	}{
		{0, 0, false},
		{7, 7, false},
		{130, 130, false},
		{125, 1, false},
		{126, 1, true},
		{127, 1, true},
	}
	for _, tc := range cases {
		code, hint := engine.MapExit(tc.code)
		if code != tc.wantCode || (hint != "") != tc.wantHint {
			t.Errorf("MapExit(%d) = (%d, %q)", tc.code, code, hint)
		}
	}
}

func TestReachErrorRemedy(t *testing.T) {
	err := &engine.ReachError{Engine: "docker"}
	msg := err.Error()
	if !strings.Contains(msg, "cannot reach the container engine") {
		t.Errorf("reach error must be one line: %q", msg)
	}
	if !strings.Contains(msg, "docker info") {
		t.Errorf("reach error must carry a remedy: %q", msg)
	}
	if !strings.Contains(msg, "Docker daemon") {
		t.Errorf("reach error should mention the daemon: %q", msg)
	}
	if strings.Contains(msg, "dial unix") {
		t.Errorf("reach error must not leak a raw dump: %q", msg)
	}
	podman := (&engine.ReachError{Engine: "podman"}).Error()
	if !strings.Contains(podman, "podman info") {
		t.Errorf("podman remedy = %q", podman)
	}
}

func TestDetect(t *testing.T) {
	notFound := func(string) (string, error) { return "", errors.New("not found") }
	found := func(string) (string, error) { return "/usr/bin/x", nil }

	// AGENTBOX_ENGINE override wins.
	e, err := engine.Detect(func(k string) string {
		if k == "AGENTBOX_ENGINE" {
			return "podman"
		}
		return ""
	}, notFound)
	if err != nil || e.Name() != "podman" {
		t.Errorf("override: %v %v", e, err)
	}
	// docker preferred over podman.
	e, err = engine.Detect(func(string) string { return "" }, found)
	if err != nil || e.Name() != "docker" {
		t.Errorf("auto-detect: %v %v", e, err)
	}
	// Invalid override is an error with remedy.
	_, err = engine.Detect(func(k string) string {
		if k == "AGENTBOX_ENGINE" {
			return "rkt"
		}
		return ""
	}, found)
	if err == nil || !strings.Contains(err.Error(), "AGENTBOX_ENGINE") {
		t.Errorf("invalid override: %v", err)
	}
	// Nothing found: one line + remedy.
	_, err = engine.Detect(func(string) string { return "" }, notFound)
	if err == nil || !strings.Contains(err.Error(), "docker") || !strings.Contains(err.Error(), "engine") {
		t.Errorf("no engine: %v", err)
	}
}
