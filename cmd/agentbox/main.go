// Command agentbox runs an AI coding agent inside an ephemeral container
// whose only host-visible, writable surface is the named workspace.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"agentbox/internal/auth"
	"agentbox/internal/authstore"
	"agentbox/internal/cli"
	"agentbox/internal/engine"
	"agentbox/internal/run"
	"agentbox/internal/state"
	"agentbox/internal/workspace"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	host := run.DefaultHost()
	io := cli.IO{
		In:    os.Stdin,
		Out:   os.Stdout,
		Err:   os.Stderr,
		IsTTY: host.IsTTY,
	}
	deps := cli.Deps{
		Run: func(o *cli.Options, _ cli.IO) int {
			return run.Run(toRunOptions(o), host)
		},
		Agents: func(o *cli.Options, io cli.IO) int {
			return cli.Agents(o, io, version)
		},
		Auth: func(o *cli.Options, io cli.IO) int {
			harness := ""
			if len(o.SubArgs) > 0 {
				harness = o.SubArgs[len(o.SubArgs)-1]
			}
			return cli.Auth(o, io, version, cli.AuthDeps{
				Probes:   authProbes(o, host, harness),
				UserRoot: run.UserRoot(host),
				Env:      os.Getenv,
				Runner:   hostRunner,
			})
		},
		Doctor: func(o *cli.Options, io cli.IO) int {
			eng, err := run.ResolveEngine(o.Engine, host)
			if err != nil {
				fmt.Fprintf(io.Err, "agentbox: %v\n", err)
				return 1
			}
			return cli.Doctor(o, io, version, eng)
		},
		State: func(o *cli.Options, io cli.IO) int {
			eng, err := run.ResolveEngine(o.Engine, host)
			if err != nil {
				fmt.Fprintf(io.Err, "agentbox: %v\n", err)
				return 1
			}
			return cli.State(o, io, eng, host.UID)
		},
		Config: func(o *cli.Options, io cli.IO) int {
			return cli.Config(o, io, version, os.Getenv)
		},
		Quickstart: func(o *cli.Options, io cli.IO) int {
			return cli.Quickstart(o, io, version, cli.QuickstartDeps{
				Env:      os.Getenv,
				UserRoot: run.UserRoot(host),
				Runner:   hostRunner,
				IsTTY:    host.IsTTY,
				RunHarness: func(harness string, agentArgs []string, stateDir string, hio cli.IO) int {
					ro := toRunOptions(o)
					ro.Agent = harness
					ro.AgentArgs = agentArgs
					ro.StateDir = stateDir
					ro.Ephemeral = false
					ro.DryRun = false
					loginHost := host
					loginHost.In = hio.In
					loginHost.Out = hio.Out
					loginHost.Err = hio.Err
					return run.Run(ro, loginHost)
				},
				TempDir: func() (string, error) { return os.MkdirTemp("", "agentbox-quickstart-") },
			})
		},
	}
	os.Exit(cli.Main(os.Args[1:], io, version, deps))
}

// hostRunner executes a host command and returns stdout (used by
// `agentbox auth copy` for `opencode auth export`). The command comes from the
// built-in adapter descriptor — a fixed argv with no shell — so there is no
// injection surface. Stderr, never stdout, feeds the error text so credential
// material cannot leak into messages.
//
// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
func hostRunner(name string, args ...string) ([]byte, error) {
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out.Bytes(), nil
}

// authProbes builds the real credential-detection probes for `agentbox auth`.
func authProbes(o *cli.Options, host run.Host, harness string) auth.Probes {
	return auth.Probes{
		Getenv: host.Env,
		CredFileExists: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && !st.IsDir()
		},
		StoredCredential: func() (string, bool) {
			_, path, ok, _ := authstore.Load(run.UserRoot(host), harness)
			return path, ok
		},
		StateExists: func() bool {
			ws, err := workspace.Canonical(o.Workspace)
			if err != nil {
				return false
			}
			eng, err := run.ResolveEngine(o.Engine, host)
			if err != nil {
				return false
			}
			name := state.VolumeName(host.UID, ws)
			out, err := engine.Capture(context.Background(), eng, []string{
				eng.Name(), "volume", "ls", "--filter", "name=^" + name + "$", "--format", "{{.Name}}",
			})
			if err != nil {
				return false
			}
			return strings.TrimSpace(out) == name
		},
	}
}

// toRunOptions converts the parsed CLI options into the run package's input.
func toRunOptions(o *cli.Options) run.Options {
	return run.Options{
		Version:      version,
		Workspace:    o.Workspace,
		Agent:        o.Agent,
		Image:        o.Image,
		DryRun:       o.DryRun,
		Quiet:        o.Quiet,
		ReadOnly:     o.ReadOnly,
		Force:        o.Force,
		Ephemeral:    o.Ephemeral,
		StateDir:     o.StateDir,
		Env:          o.Env,
		EnvFiles:     o.EnvFiles,
		Credentials:  o.Credentials,
		Net:          o.Net,
		CPUs:         o.CPUs,
		Memory:       o.Memory,
		PidsLimit:    o.PidsLimit,
		User:         o.User,
		Engine:       o.Engine,
		Name:         o.Name,
		Keep:         o.Keep,
		WorkdirPath:  o.WorkdirPath,
		Config:       o.Config,
		Build:        o.Build,
		NoCheck:      o.NoCheck,
		NoTTY:        o.NoTTY,
		NoUserConfig: o.NoUserConfig,
		AgentArgs:    o.AgentArgs,
	}
}
