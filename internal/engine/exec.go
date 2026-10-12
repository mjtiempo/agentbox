package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// NewDocker returns the docker engine.
func NewDocker() Engine { return &cliEngine{base: base{name: "docker"}} }

// NewPodman returns the podman engine.
func NewPodman() Engine { return &cliEngine{base: base{name: "podman"}} }

type cliEngine struct {
	base
}

// Run implements Engine using the process's own stdio.
func (e *cliEngine) Run(ctx context.Context, argv []string) (int, error) {
	return e.RunStream(ctx, argv, Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Env: os.Environ()})
}

// RunStream execs the engine with the given stdio and maps the result.
// A nonzero engine exit is not a Go error: it is the observed exit code.
func (e *cliEngine) RunStream(ctx context.Context, argv []string, stdio Stdio) (int, error) {
	if len(argv) == 0 {
		return 1, errors.New("empty engine command")
	}
	// The engine binary is one of a fixed set (docker|podman) and every argument
	// is passed as its own argv element: no shell is involved, so argv tokens
	// cannot break out into shell interpretation. This is the documented engine
	// access model (PRD decision log: exec the engine CLI), not user-supplied
	// code execution.
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdio.In != nil {
		cmd.Stdin = stdio.In
	}
	if stdio.Out != nil {
		cmd.Stdout = stdio.Out
	}
	if stdio.Err != nil {
		cmd.Stderr = stdio.Err
	}
	if stdio.Env != nil {
		cmd.Env = stdio.Env
	}
	if stdio.Dir != "" {
		cmd.Dir = stdio.Dir
	}
	if stdio.OnStart != nil {
		if err := cmd.Start(); err != nil {
			return 1, fmt.Errorf("cannot run %s: %w", argv[0], err)
		}
		stdio.OnStart(cmd.Process)
		err := cmd.Wait()
		if err == nil {
			return 0, nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 1, fmt.Errorf("cannot run %s: %w", argv[0], err)
	}
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 1, fmt.Errorf("cannot run %s: %w", argv[0], err)
}

// redactArgv renders argv for error messages without leaking values passed as
// "-e"/"--env" KEY=value pairs (separate or attached): the value is replaced
// with <redacted>, a bare KEY is unchanged, and all other tokens pass through
// untouched.
func redactArgv(argv []string) string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		token := argv[i]
		if (token == "-e" || token == "--env") && i+1 < len(argv) {
			out = append(out, token)
			i++
			next := argv[i]
			if key, _, ok := strings.Cut(next, "="); ok {
				out = append(out, key+"=<redacted>")
			} else {
				out = append(out, next)
			}
			continue
		}
		if key, ok := strings.CutPrefix(token, "--env="); ok {
			redactAttached(&out, "--env=", key)
			continue
		}
		if strings.HasPrefix(token, "-e") && !strings.HasPrefix(token, "--") {
			redactAttached(&out, "-e", token[len("-e"):])
			continue
		}
		out = append(out, token)
	}
	return strings.Join(out, " ")
}

// redactAttached rewrites one attached KEY=value token, prefix included; a
// token without "=" is unchanged.
func redactAttached(out *[]string, prefix, rest string) {
	if key, _, ok := strings.Cut(rest, "="); ok {
		*out = append(*out, prefix+key+"=<redacted>")
		return
	}
	*out = append(*out, prefix+rest)
}

// Capture runs a read-only engine command and returns its stdout. Nonzero
// exits become errors carrying the engine's stderr (callers render one line).
func Capture(ctx context.Context, e Engine, argv []string) (string, error) {
	var out, errb bytes.Buffer
	code, err := e.RunStream(ctx, argv, Stdio{Out: &out, Err: &errb})
	if err != nil {
		return "", err
	}
	if code != 0 {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = fmt.Sprintf("exit status %d", code)
		}
		return out.String(), fmt.Errorf("%s %s failed: %s", e.Name(), redactArgv(argv[1:]), msg)
	}
	return out.String(), nil
}

// MapExit maps an engine/agent exit code to agentbox's exit code and an
// optional one-line hint (engine 125–127 => 1; 126/127 add image hints).
func MapExit(code int) (int, string) {
	switch code {
	case 126:
		return 1, "the entrypoint is not executable — rebuild the image with `make image` or pass --image with a shell entrypoint"
	case 127:
		return 1, "the image or entrypoint was not found — check --image, or build the local image with `make image`"
	case 125:
		return 1, ""
	default:
		return code, ""
	}
}

// ReachError is the one-line engine-unreachable error with its remedy.
type ReachError struct {
	Engine string
}

func (e *ReachError) Error() string {
	remedy := fmt.Sprintf("is the %s daemon running? try: %s info", e.Engine, e.Engine)
	if e.Engine == "docker" {
		remedy = "is the Docker daemon running? try: docker info"
	}
	return fmt.Sprintf("cannot reach the container engine (%s)\n  \u2192 %s", e.Engine, remedy)
}

// Detect resolves the engine: AGENTBOX_ENGINE wins, else docker, else podman.
func Detect(getenv func(string) string, lookPath func(string) (string, error)) (Engine, error) {
	if v := getenv("AGENTBOX_ENGINE"); v != "" {
		switch v {
		case "docker":
			return NewDocker(), nil
		case "podman":
			return NewPodman(), nil
		default:
			return nil, fmt.Errorf("invalid AGENTBOX_ENGINE value %q (valid: docker, podman)", v)
		}
	}
	for _, name := range []string{"docker", "podman"} {
		if _, err := lookPath(name); err == nil {
			if name == "docker" {
				return NewDocker(), nil
			}
			return NewPodman(), nil
		}
	}
	return nil, fmt.Errorf("cannot find a container engine (docker or podman)\n  \u2192 install Docker, or set AGENTBOX_ENGINE to the engine binary")
}
