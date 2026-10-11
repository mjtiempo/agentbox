package auth_test

import (
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/auth"
)

func pi(t *testing.T) adapter.Adapter {
	t.Helper()
	a, err := adapter.Get("pi", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDetectEnvKey(t *testing.T) {
	env := map[string]string{"ANTHROPIC_API_KEY": "sk-secret"}
	r := auth.Detect(pi(t), auth.Probes{Getenv: func(k string) string { return env[k] }})
	if r.Mechanism != "environment" || r.Detail != "ANTHROPIC_API_KEY" {
		t.Errorf("r = %+v", r)
	}
	if strings.Contains(r.Line("pi"), "sk-secret") {
		t.Error("auth output must never contain values")
	}
}

func TestDetectOAuthToken(t *testing.T) {
	env := map[string]string{"ANTHROPIC_OAUTH_TOKEN": "oauth-secret"}
	r := auth.Detect(pi(t), auth.Probes{Getenv: func(k string) string { return env[k] }})
	if r.Mechanism != "environment (oauth)" {
		t.Errorf("r = %+v", r)
	}
}

func TestDetectCredentialFile(t *testing.T) {
	a := pi(t)
	if a.HostCredFile == "" {
		t.Fatal("pi must declare a host credential file")
	}
	want := filepath.Join("/home/u", a.HostCredFile)
	r := auth.Detect(a, auth.Probes{
		Getenv: func(k string) string {
			if k == "HOME" {
				return "/home/u"
			}
			return ""
		},
		CredFileExists: func(p string) bool { return p == want },
	})
	if r.Mechanism != "credential file" || r.Detail != want {
		t.Errorf("r = %+v, want credential file at %s", r, want)
	}
}

func TestDetectCredentialFileIgnoresContainerPath(t *testing.T) {
	// The container target must never be probed on the host.
	a := pi(t)
	r := auth.Detect(a, auth.Probes{
		Getenv: func(k string) string {
			if k == "HOME" {
				return "/home/u"
			}
			return ""
		},
		CredFileExists: func(p string) bool { return p == a.CredFile },
	})
	if r.Mechanism == "credential file" {
		t.Errorf("detected the container path %q as a host file", a.CredFile)
	}
}

func TestDetectOpencodeFallsBackToState(t *testing.T) {
	a, _ := adapter.Get("opencode", "0.1.0")
	if a.HostCredFile != "" {
		t.Errorf("opencode HostCredFile = %q, want empty (credentials live in the DB)", a.HostCredFile)
	}
	r := auth.Detect(a, auth.Probes{
		Getenv:      func(string) string { return "" },
		StateExists: func() bool { return true },
	})
	if r.Mechanism != "state volume" {
		t.Errorf("r = %+v", r)
	}
}

func TestDetectStateVolume(t *testing.T) {
	r := auth.Detect(pi(t), auth.Probes{
		Getenv:      func(string) string { return "" },
		StateExists: func() bool { return true },
	})
	if r.Mechanism != "state volume" {
		t.Errorf("r = %+v", r)
	}
}

func TestDetectNone(t *testing.T) {
	r := auth.Detect(pi(t), auth.Probes{Getenv: func(string) string { return "" }})
	if r.Mechanism != "none" {
		t.Errorf("r = %+v", r)
	}
	if !strings.Contains(r.Line("pi"), "none") {
		t.Errorf("line = %q", r.Line("pi"))
	}
}

func TestPreflightNoticeClaudeWording(t *testing.T) {
	a, _ := adapter.Get("claude", "0.1.0")
	notice := auth.PreflightNotice(a)
	for _, want := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", `"claude"`, "agentbox auth claude", "--env"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q missing %q", notice, want)
		}
	}
}

func TestPreflightNoticeShell(t *testing.T) {
	a, _ := adapter.Get("shell", "0.1.0")
	notice := auth.PreflightNotice(a)
	if !strings.Contains(notice, "no credentials detected") {
		t.Errorf("notice = %q", notice)
	}
}
