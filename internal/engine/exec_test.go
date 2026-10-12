package engine

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRedactArgv(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"short env flag", []string{"-e", "KEY=secret"}, "-e KEY=<redacted>"},
		{"long env flag", []string{"--env", "K=v"}, "--env K=<redacted>"},
		{"attached long env flag", []string{"--env=KEY=secret"}, "--env=KEY=<redacted>"},
		{"attached short env flag", []string{"-eKEY=secret"}, "-eKEY=<redacted>"},
		{"attached long env flag without value", []string{"--env=KEY"}, "--env=KEY"},
		{"attached short env flag without value", []string{"-eKEY"}, "-eKEY"},
		{"bare key", []string{"-e", "KEY"}, "-e KEY"},
		{"unrelated tokens", []string{"run", "--rm", "img", "pi"}, "run --rm img pi"},
		{"full argv", []string{"run", "-e", "KEY=secret"}, "run -e KEY=<redacted>"},
		{"dangling flag", []string{"run", "-e"}, "run -e"},
	}
	for _, tc := range cases {
		if got := redactArgv(tc.in); got != tc.want {
			t.Errorf("%s: redactArgv(%v) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

type captureFakeEngine struct {
	name   string
	code   int
	stderr string
}

func (f captureFakeEngine) Name() string                                     { return f.name }
func (f captureFakeEngine) RunArgv(RunSpec) []string                         { return nil }
func (f captureFakeEngine) DryRunArgv(RunSpec, func(string) string) []string { return nil }
func (f captureFakeEngine) Run(context.Context, []string) (int, error)       { return f.code, nil }
func (f captureFakeEngine) RunStream(_ context.Context, _ []string, stdio Stdio) (int, error) {
	if stdio.Err != nil {
		io.WriteString(stdio.Err, f.stderr)
	}
	return f.code, nil
}

func TestCaptureRedactsEnvValues(t *testing.T) {
	e := captureFakeEngine{name: "docker", code: 1, stderr: "boom"}
	argv := []string{"docker", "run", "-e", "API_KEY=super-secret", "--env", "OTHER=hidden", "img"}
	_, err := Capture(context.Background(), e, argv)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "super-secret") || strings.Contains(msg, "hidden") {
		t.Errorf("error leaked a secret: %v", err)
	}
	for _, want := range []string{"docker run", "API_KEY=<redacted>", "OTHER=<redacted>", "failed: boom"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}
