package cli

import (
	"fmt"
	"io"
	"os"

	"agentbox/internal/config"
)

// IO bundles the streams and TTY state handed to a command.
type IO struct {
	In    io.Reader
	Out   io.Writer
	Err   io.Writer
	IsTTY bool
}

// Deps are the side-effecting operations Main dispatches to. main.go wires the
// real implementations; tests inject stubs. A nil entry returns exit code 1
// with a clear "not implemented" line.
type Deps struct {
	Run        func(*Options, IO) int
	Agents     func(*Options, IO) int
	Auth       func(*Options, IO) int
	Doctor     func(*Options, IO) int
	State      func(*Options, IO) int
	Config     func(*Options, IO) int
	Quickstart func(*Options, IO) int
}

// Main parses args and dispatches. It owns the process exit code:
// 0 success, 1 engine/IO failure or nonzero agent exit, 2 usage.
func Main(args []string, io IO, version string, deps Deps) int {
	opts, err := Parse(args)
	if err != nil {
		writeUsageError(io.Err, err)
		return 2
	}
	if opts.ShowHelp {
		fmt.Fprint(io.Out, Help())
		return 0
	}
	if opts.ShowVersion || opts.Command == "version" {
		fmt.Fprintf(io.Out, "agentbox %s\n", version)
		return 0
	}

	// Config applies to the run path only; the provenance line is always
	// printed when a config file was applied, even under --quiet (FR-12).
	if opts.Command == "" {
		ws := opts.Workspace
		if ws == "" {
			ws, _ = os.Getwd()
		}
		loaded, err := config.Resolve(opts.Config, ws, os.Getenv)
		if err != nil {
			if ce, ok := err.(*config.Error); ok {
				fmt.Fprint(io.Err, ce.Render())
			} else {
				fmt.Fprintf(io.Err, "agentbox: %v\n", err)
			}
			return 2
		}
		for _, p := range loaded.Provenance {
			fmt.Fprintf(io.Err, "agentbox: config: %s\n", p)
		}
		ApplyConfig(opts, loaded.Values)
	}

	switch opts.Command {
	case "":
		if deps.Run == nil {
			return notImplemented(io, "run")
		}
		return deps.Run(opts, io)
	case "agents":
		if deps.Agents == nil {
			return notImplemented(io, "agents")
		}
		return deps.Agents(opts, io)
	case "auth":
		if deps.Auth == nil {
			return notImplemented(io, "auth")
		}
		return deps.Auth(opts, io)
	case "doctor":
		if deps.Doctor == nil {
			return notImplemented(io, "doctor")
		}
		return deps.Doctor(opts, io)
	case "state":
		if deps.State == nil {
			return notImplemented(io, "state")
		}
		return deps.State(opts, io)
	case "config":
		if deps.Config == nil {
			return notImplemented(io, "config")
		}
		return deps.Config(opts, io)
	case "quickstart":
		if deps.Quickstart == nil {
			return notImplemented(io, "quickstart")
		}
		return deps.Quickstart(opts, io)
	}
	return notImplemented(io, opts.Command)
}

func writeUsageError(w io.Writer, err error) {
	if ue, ok := err.(*UsageError); ok {
		fmt.Fprint(w, ue.Render())
		return
	}
	fmt.Fprintf(w, "agentbox: %v\n", err)
}

func notImplemented(io IO, what string) int {
	fmt.Fprintf(io.Err, "agentbox: %s is not implemented in this build\n", what)
	return 1
}
