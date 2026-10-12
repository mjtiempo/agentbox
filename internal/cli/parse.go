// Package cli parses agentbox arguments, renders help, and dispatches
// commands. It owns exit codes: 0 success, 1 engine/IO failure, 2 usage.
package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// Options is the parsed command line. Zero values are not meaningful for
// Net/CPUs/Memory/PidsLimit/Agent; Parse fills the documented defaults.
type Options struct {
	Workspace    string   // [workspace]; "" => $PWD
	Agent        string   // --agent/-A (default pi)
	Image        string   // --image
	DryRun       bool     // --dry-run
	Quiet        bool     // --quiet/-q
	ReadOnly     bool     // --ro
	Force        bool     // --force (also: state rm/gc force)
	Ephemeral    bool     // --ephemeral
	StateDir     string   // --state-dir
	Env          []string // --env/-e KEY (repeatable)
	EnvFiles     []string // --env-file (repeatable)
	Credentials  []string // --credentials (repeatable)
	Net          string   // --net bridge|none|host (default bridge)
	CPUs         string   // --cpus (default 2)
	Memory       string   // --memory (default 4g)
	PidsLimit    int      // --pids-limit (default 512)
	User         string   // --user uid:gid
	Engine       string   // --engine docker|podman (default auto)
	Name         string   // --name
	Keep         bool     // --keep
	WorkdirPath  string   // --workdir-path
	Config       string   // --config
	From         string   // config link/copy --from <path>
	Build        bool     // --build
	NoCheck      bool     // --no-check
	NoTTY        bool     // --no-tty
	NoUserConfig bool     // --no-user-config
	ShowHelp     bool     // --help/-h
	ShowVersion  bool     // --version

	Command   string   // "" (run) | agents | auth | doctor | state | version
	SubArgs   []string // positional args after a subcommand
	AgentArgs []string // args after the `--` boundary, forwarded verbatim

	// Explicit records which flags were set on the command line, so config
	// values apply only where the CLI did not speak (FR-12 precedence
	// CLI > project > user).
	Explicit map[string]bool
}

// UsageError is a parse/usage failure: one line plus one remedy, exit code 2.
type UsageError struct {
	Msg  string
	Hint string
}

func (e *UsageError) Error() string { return e.Msg }

// Render formats the error in the documented one-line + remedy style.
func (e *UsageError) Render() string {
	if e.Hint == "" {
		return "agentbox: " + e.Msg + "\n"
	}
	return "agentbox: " + e.Msg + "\n  \u2192 " + e.Hint + "\n"
}

func usagef(format string, args ...any) *UsageError {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

func usageHintf(hint string, format string, args ...any) *UsageError {
	return &UsageError{Msg: fmt.Sprintf(format, args...), Hint: hint}
}

func defaults() *Options {
	return &Options{
		Agent:     "pi",
		Net:       "bridge",
		CPUs:      "2",
		Memory:    "4g",
		PidsLimit: 512,
		Explicit:  map[string]bool{},
	}
}

var subcommands = map[string]bool{
	"agents": true, "auth": true, "doctor": true, "state": true, "version": true,
	"config": true, "quickstart": true,
}

// Parse parses args (without the program name). Flags may appear before or
// after the single positional workspace. Everything after `--` is forwarded
// verbatim; `--` itself is consumed and never forwarded.
func Parse(args []string) (*Options, error) {
	o := defaults()
	positional := 0
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--":
			o.AgentArgs = append(o.AgentArgs, args[i+1:]...)
			return o, nil
		case strings.HasPrefix(a, "--"):
			name, val, hasVal := splitLong(a)
			extra, err := o.applyLong(name, val, hasVal, args[i+1:])
			if err != nil {
				return nil, err
			}
			i += 1 + extra
		case strings.HasPrefix(a, "-") && len(a) > 1:
			consumed, err := o.applyShort(a, args[i+1:])
			if err != nil {
				return nil, err
			}
			i += 1 + consumed
		default:
			if positional == 0 && subcommands[a] {
				return o.parseSubcommand(a, args[i+1:])
			}
			if positional > 0 {
				return nil, usageHintf(
					"run `agentbox --help` for usage",
					"at most one workspace is allowed (got %q and %q)", o.Workspace, a)
			}
			o.Workspace = a
			positional++
			i++
		}
	}
	return o, nil
}

// splitLong splits "--name=value" into ("name", "value", true).
func splitLong(a string) (name, val string, hasVal bool) {
	body := strings.TrimPrefix(a, "--")
	if eq := strings.IndexByte(body, '='); eq >= 0 {
		return body[:eq], body[eq+1:], true
	}
	return body, "", false
}

// applyLong applies a long flag. It returns the number of additional argv
// tokens it consumed (0 or 1).
func (o *Options) applyLong(name, val string, hasVal bool, rest []string) (int, error) {
	// Flags that take a value.
	needsValue := map[string]func(string) error{
		"agent":       func(v string) error { o.Agent = v; return nil },
		"image":       func(v string) error { o.Image = v; return nil },
		"state-dir":   func(v string) error { o.StateDir = v; return nil },
		"env":         func(v string) error { o.Env = append(o.Env, v); return nil },
		"env-file":    func(v string) error { o.EnvFiles = append(o.EnvFiles, v); return nil },
		"credentials": func(v string) error { o.Credentials = append(o.Credentials, v); return nil },
		"net": func(v string) error {
			switch v {
			case "bridge", "none", "host":
				o.Net = v
				return nil
			}
			return usagef("invalid --net value %q (valid: bridge, none, host)", v)
		},
		"cpus":         func(v string) error { o.CPUs = v; return nil },
		"memory":       func(v string) error { o.Memory = v; return nil },
		"user":         func(v string) error { o.User = v; return nil },
		"engine":       o.setEngine,
		"name":         func(v string) error { o.Name = v; return nil },
		"workdir-path": func(v string) error { o.WorkdirPath = v; return nil },
		"config":       func(v string) error { o.Config = v; return nil },
		"pids-limit": func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return usageHintf("pass a positive integer, e.g. --pids-limit 512",
					"invalid --pids-limit value %q", v)
			}
			o.PidsLimit = n
			return nil
		},
	}

	if fn, ok := needsValue[name]; ok {
		if hasVal {
			if err := fn(val); err != nil {
				return 0, err
			}
			o.mark(name)
			return 0, nil
		}
		if len(rest) == 0 || looksLikeFlag(rest[0]) {
			return 0, usageHintf("pass it as `--"+name+" <value>`", "--%s requires a value", name)
		}
		if err := fn(rest[0]); err != nil {
			return 0, err
		}
		o.mark(name)
		return 1, nil
	}

	// Boolean flags.
	if hasVal {
		return 0, usagef("flag --%s does not take a value", name)
	}
	switch name {
	case "dry-run":
		o.DryRun = true
	case "quiet":
		o.Quiet = true
	case "ro":
		o.ReadOnly = true
	case "force":
		o.Force = true
	case "ephemeral":
		o.Ephemeral = true
	case "keep":
		o.Keep = true
	case "build":
		o.Build = true
	case "no-check":
		o.NoCheck = true
	case "no-tty":
		o.NoTTY = true
	case "no-user-config":
		o.NoUserConfig = true
	case "help":
		o.ShowHelp = true
	case "version":
		o.ShowVersion = true
	default:
		return 0, unknownFlag("--" + name)
	}
	o.mark(name)
	return 0, nil
}

// setEngine validates and stores an --engine value. applyLong and
// parseSubcommand share it so both positions accept the same values with the
// same error text.
func (o *Options) setEngine(v string) error {
	switch v {
	case "docker", "podman":
		o.Engine = v
		return nil
	}
	return usagef("invalid --engine value %q (valid: docker, podman)", v)
}

func (o *Options) mark(name string) {
	if o.Explicit == nil {
		o.Explicit = map[string]bool{}
	}
	o.Explicit[name] = true
}

func unknownFlag(flag string) *UsageError {
	return usageHintf(
		fmt.Sprintf("agent arguments go after \"--\": agentbox -- %s foo", flag),
		"unknown flag %q", flag)
}

func (o *Options) applyShort(a string, rest []string) (int, error) {
	// a starts with "-" and len(a) > 1.
	short := a[1]
	switch short {
	case 'q':
		if len(a) > 2 {
			return 0, unknownFlag(a)
		}
		o.Quiet = true
		o.mark("quiet")
		return 0, nil
	case 'h':
		if len(a) > 2 {
			return 0, unknownFlag(a)
		}
		o.ShowHelp = true
		o.mark("help")
		return 0, nil
	case 'A':
		v, consumed, err := shortValue(a, rest, "-A")
		if err != nil {
			return 0, err
		}
		o.Agent = v
		o.mark("agent")
		return consumed, nil
	case 'e':
		v, consumed, err := shortValue(a, rest, "-e")
		if err != nil {
			return 0, err
		}
		o.Env = append(o.Env, v)
		o.mark("env")
		return consumed, nil
	default:
		return 0, unknownFlag("-" + string(short))
	}
}

func shortValue(a string, rest []string, flag string) (string, int, error) {
	if len(a) > 2 {
		return a[2:], 0, nil
	}
	if len(rest) == 0 || looksLikeFlag(rest[0]) {
		return "", 0, usageHintf("pass it as `"+flag+" <value>`", "%s requires a value", flag)
	}
	return rest[0], 1, nil
}

func looksLikeFlag(s string) bool {
	return len(s) > 1 && strings.HasPrefix(s, "-")
}

// parseSubcommand parses the arguments following a subcommand token.
func (o *Options) parseSubcommand(cmd string, args []string) (*Options, error) {
	o.Command = cmd
	positional := 0
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			o.ShowHelp = true
			i++
		case a == "--force":
			o.Force = true
			i++
		case a == "--no-check":
			o.NoCheck = true
			i++
		case a == "--from":
			if i+1 >= len(args) || looksLikeFlag(args[i+1]) {
				return nil, usageHintf("pass it as `--from <path>`", "--from requires a value")
			}
			o.From = args[i+1]
			i += 2
		case strings.HasPrefix(a, "--from="):
			o.From = strings.TrimPrefix(a, "--from=")
			i++
		case a == "--engine":
			if i+1 >= len(args) || looksLikeFlag(args[i+1]) {
				return nil, usageHintf("pass it as `--engine <value>`", "--engine requires a value")
			}
			if err := o.setEngine(args[i+1]); err != nil {
				return nil, err
			}
			i += 2
		case strings.HasPrefix(a, "--engine="):
			if err := o.setEngine(strings.TrimPrefix(a, "--engine=")); err != nil {
				return nil, err
			}
			i++
		case strings.HasPrefix(a, "-") && len(a) > 1:
			return nil, unknownFlag(a)
		default:
			o.SubArgs = append(o.SubArgs, a)
			positional++
			i++
		}
	}
	switch cmd {
	case "auth":
		switch len(o.SubArgs) {
		case 0:
			return nil, usageHintf("usage: agentbox auth [copy|reset] <harness>",
				"auth needs a harness name")
		case 1:
			// A lone copy/reset is the subcommand form missing its harness;
			// no harness is named "copy" or "reset".
			if o.SubArgs[0] == "copy" || o.SubArgs[0] == "reset" {
				return nil, usageHintf("usage: agentbox auth "+o.SubArgs[0]+" <harness>",
					"auth %s needs a harness name", o.SubArgs[0])
			}
		case 2:
			switch o.SubArgs[0] {
			case "copy", "reset":
			default:
				return nil, usageHintf("valid auth subcommands: copy, reset",
					"unknown auth subcommand %q", o.SubArgs[0])
			}
		default:
			return nil, usageHintf("usage: agentbox auth [copy|reset] <harness>",
				"auth takes at most a subcommand and a harness name")
		}
	case "doctor", "agents", "version":
		if len(o.SubArgs) != 0 {
			return nil, usagef("%s takes no arguments", cmd)
		}
	case "quickstart":
		if len(o.SubArgs) > 1 {
			return nil, usagef("quickstart takes at most one harness name")
		}
	case "state":
		if len(o.SubArgs) == 0 {
			return nil, usageHintf("usage: agentbox state ls | rm <project> | gc",
				"state needs a subcommand (ls, rm, gc)")
		}
		switch o.SubArgs[0] {
		case "ls", "gc":
			if len(o.SubArgs) > 1 {
				return nil, usagef("state %s takes no arguments", o.SubArgs[0])
			}
		case "rm":
			if len(o.SubArgs) != 2 {
				return nil, usageHintf("usage: agentbox state rm <project>",
					"state rm needs a project name")
			}
		default:
			return nil, usageHintf("valid state subcommands: ls, rm, gc",
				"unknown state subcommand %q", o.SubArgs[0])
		}
	case "config":
		if len(o.SubArgs) == 0 {
			return nil, usageHintf("usage: agentbox config link|copy|reset [harness] [--from <path>] [--force]",
				"config needs a subcommand (link, copy, reset)")
		}
		switch o.SubArgs[0] {
		case "link", "copy":
			if len(o.SubArgs) > 2 {
				return nil, usagef("config %s takes at most one harness name", o.SubArgs[0])
			}
		case "reset":
			if len(o.SubArgs) > 2 {
				return nil, usagef("config reset takes at most one harness name")
			}
			if o.From != "" {
				return nil, usagef("config reset does not take --from")
			}
		default:
			return nil, usageHintf("valid config subcommands: link, copy, reset",
				"unknown config subcommand %q", o.SubArgs[0])
		}
	}
	return o, nil
}
