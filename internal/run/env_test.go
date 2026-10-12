package run

import (
	"testing"

	"agentbox/internal/engine"
)

func envValue(envs []engine.EnvVar, name string) (string, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func identityHost(uid, gid int) Host {
	return Host{
		Env:   func(string) string { return "" },
		UID:   uid,
		GID:   gid,
		IsTTY: false,
	}
}

func TestBuildEnvironmentDefaultHostIdentity(t *testing.T) {
	ad := testAdapter(t, "pi")
	envs, _, _ := buildEnvironment(ad, Options{}, identityHost(1000, 1001), nil)
	if got, _ := envValue(envs, "AGENTBOX_UID"); got != "1000" {
		t.Errorf("AGENTBOX_UID = %q, want the host uid 1000", got)
	}
	if got, _ := envValue(envs, "AGENTBOX_GID"); got != "1001" {
		t.Errorf("AGENTBOX_GID = %q, want the host gid 1001", got)
	}
}

func TestBuildEnvironmentRootUserIsExplicit(t *testing.T) {
	ad := testAdapter(t, "pi")
	for _, user := range []string{"0", "0:0"} {
		envs, _, _ := buildEnvironment(ad, Options{User: user}, identityHost(1000, 1001), nil)
		if got, _ := envValue(envs, "AGENTBOX_UID"); got != "0" {
			t.Errorf("--user %s: AGENTBOX_UID = %q, want 0", user, got)
		}
		if got, _ := envValue(envs, "AGENTBOX_GID"); got != "0" {
			t.Errorf("--user %s: AGENTBOX_GID = %q, want 0", user, got)
		}
	}
}

func TestBuildEnvironmentNonRootUserKeepsHostIdentity(t *testing.T) {
	// A non-root --user takes the entrypoint's non-root path (setpriv from the
	// engine's own --user), so the dropped AGENTBOX_UID/GID stay the host ones.
	ad := testAdapter(t, "pi")
	envs, _, _ := buildEnvironment(ad, Options{User: "1234:5678"}, identityHost(1000, 1001), nil)
	if got, _ := envValue(envs, "AGENTBOX_UID"); got != "1000" {
		t.Errorf("AGENTBOX_UID = %q, want the unchanged host uid 1000", got)
	}
	if got, _ := envValue(envs, "AGENTBOX_GID"); got != "1001" {
		t.Errorf("AGENTBOX_GID = %q, want the unchanged host gid 1001", got)
	}
}
