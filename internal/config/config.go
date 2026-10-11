// Package config implements the FR-12 TOML configuration file:
// discovery, precedence (CLI > project > user), the project-config
// restriction set, and provenance reporting.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Values is a config layer. nil pointers mean "not set in this layer".
type Values struct {
	Agent        *string
	Image        *string
	CPUs         *string
	Memory       *string
	PidsLimit    *int
	RO           *bool
	Ephemeral    *bool
	Quiet        *bool
	Net          *string
	Env          []string
	EnvFile      []string
	StateDir     *string
	Credentials  []string
	User         *string
	Name         *string
	Keep         *bool
	WorkdirPath  *string
	Engine       *string
	NoCheck      *bool
	NoTTY        *bool
	NoUserConfig *bool
	Build        *bool
	Force        *bool
}

// Error is a config failure with a one-line remedy (PRD §8.2 style).
type Error struct {
	Msg    string
	Remedy string
}

func (e *Error) Error() string { return e.Msg }

// Render formats the error the way the CLI prints it.
func (e *Error) Render() string {
	if e.Remedy == "" {
		return "agentbox: " + e.Msg + "\n"
	}
	return "agentbox: " + e.Msg + "\n  \u2192 " + e.Remedy + "\n"
}

// projectKeys are the only keys a workspace agentbox.toml may set (FR-12).
var projectKeys = map[string]bool{
	"agent": true, "cpus": true, "memory": true, "pids-limit": true,
	"ro": true, "ephemeral": true,
}

// boundaryKeys are explicitly rejected in project config with the FR-12
// message.
var boundaryKeys = map[string]bool{
	"image": true, "net": true, "env": true, "env-file": true,
	"state-dir": true, "credentials": true, "user": true, "name": true, "keep": true,
}

// userKeys align 1:1 with flags (minus non-persistent ones).
var userKeys = map[string]bool{
	"agent": true, "image": true, "ro": true, "ephemeral": true, "state-dir": true,
	"env": true, "env-file": true, "credentials": true, "net": true, "cpus": true,
	"memory": true, "pids-limit": true, "user": true, "engine": true, "name": true,
	"keep": true, "workdir-path": true, "quiet": true, "no-check": true,
	"no-tty": true, "no-user-config": true, "build": true, "force": true,
}

const boundaryRemedy = `boundary-relevant keys (image, net, env, env-file, state-dir, credentials, user, name, keep) and agent = "image:<ref>" are CLI/user-config only`

// LoadUser reads a user config: all flag-aligned keys are allowed.
func LoadUser(path string) (*Values, error) {
	return load(path, false, "")
}

// LoadProject reads a workspace config: only boundary-neutral keys are
// allowed, and the file must resolve inside the workspace.
func LoadProject(path, workspace string) (*Values, error) {
	if workspace != "" {
		if err := ensureInside(path, workspace); err != nil {
			return nil, err
		}
	}
	return load(path, true, workspace)
}

// ensureInside rejects a project config that resolves outside the workspace
// (symlink escape, FR-12).
func ensureInside(path, workspace string) error {
	canonWS, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		canonWS = filepath.Clean(workspace)
	}
	canonPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return &Error{
			Msg:    fmt.Sprintf("cannot resolve project config %q", path),
			Remedy: "check the path and its symlinks",
		}
	}
	if canonPath != canonWS && !strings.HasPrefix(canonPath+string(filepath.Separator), canonWS+string(filepath.Separator)) {
		return &Error{
			Msg:    fmt.Sprintf("project config %q resolves outside the workspace", path),
			Remedy: "keep agentbox.toml inside the project, or pass --config for an explicit file",
		}
	}
	return nil
}

func load(path string, project bool, workspace string) (*Values, error) {
	var raw map[string]any
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		// Never echo file contents: only the position is safe.
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return nil, &Error{
				Msg:    fmt.Sprintf("cannot parse config file %s (line %d)", path, pe.Position.Line),
				Remedy: "fix the TOML syntax; file contents are never echoed",
			}
		}
		return nil, &Error{
			Msg:    fmt.Sprintf("cannot parse config file %s", path),
			Remedy: "fix the TOML syntax; file contents are never echoed",
		}
	}
	_ = md
	if err := validateKeys(raw, project, path); err != nil {
		return nil, err
	}
	v, err := fromRaw(raw, path)
	if err != nil {
		return nil, err
	}
	if project && v.Agent != nil && strings.HasPrefix(*v.Agent, "image:") {
		return nil, &Error{
			Msg:    fmt.Sprintf("agent = \"image:<ref>\" cannot be set by project config (%s)", filepath.Base(path)),
			Remedy: "project config may name built-in adapters only: pi, shell, claude, opencode",
		}
	}
	return v, nil
}

func validateKeys(raw map[string]any, project bool, path string) error {
	base := filepath.Base(path)
	for key := range raw {
		if project {
			if projectKeys[key] {
				continue
			}
			if key == "quiet" {
				return &Error{
					Msg:    fmt.Sprintf("%q cannot be set by project config (%s): a project must not hide the provenance line or the FR-2 notices", key, base),
					Remedy: "use --quiet or a user config if you really want to suppress output",
				}
			}
			if boundaryKeys[key] {
				return &Error{
					Msg:    fmt.Sprintf("%q cannot be set by project config (%s)", key, base),
					Remedy: boundaryRemedy,
				}
			}
			return &Error{
				Msg:    fmt.Sprintf("%q cannot be set by project config (%s)", key, base),
				Remedy: "project config may set only: agent, cpus, memory, pids-limit, ro, ephemeral",
			}
		}
		if !userKeys[key] {
			return &Error{
				Msg:    fmt.Sprintf("unknown config key %q in %s", key, base),
				Remedy: "config keys align 1:1 with flags; see `agentbox --help`",
			}
		}
	}
	return nil
}

func fromRaw(raw map[string]any, path string) (*Values, error) {
	v := &Values{}
	for key, val := range raw {
		switch key {
		case "agent":
			v.Agent = ptrString(mustString(key, val))
		case "image":
			v.Image = ptrString(mustString(key, val))
		case "cpus":
			v.CPUs = ptrString(mustString(key, val))
		case "memory":
			v.Memory = ptrString(mustString(key, val))
		case "pids-limit":
			n, err := asInt(key, val)
			if err != nil {
				return nil, err
			}
			v.PidsLimit = &n
		case "ro":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.RO = &b
		case "ephemeral":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.Ephemeral = &b
		case "quiet":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.Quiet = &b
		case "net":
			v.Net = ptrString(mustString(key, val))
		case "env":
			list, err := asStringList(key, val)
			if err != nil {
				return nil, err
			}
			v.Env = list
		case "env-file":
			list, err := asStringList(key, val)
			if err != nil {
				return nil, err
			}
			v.EnvFile = list
		case "state-dir":
			v.StateDir = ptrString(mustString(key, val))
		case "credentials":
			list, err := asStringList(key, val)
			if err != nil {
				return nil, err
			}
			v.Credentials = list
		case "user":
			v.User = ptrString(mustString(key, val))
		case "name":
			v.Name = ptrString(mustString(key, val))
		case "keep":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.Keep = &b
		case "workdir-path":
			v.WorkdirPath = ptrString(mustString(key, val))
		case "engine":
			v.Engine = ptrString(mustString(key, val))
		case "no-check":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.NoCheck = &b
		case "no-tty":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.NoTTY = &b
		case "no-user-config":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.NoUserConfig = &b
		case "build":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.Build = &b
		case "force":
			b, err := asBool(key, val)
			if err != nil {
				return nil, err
			}
			v.Force = &b
		}
	}
	return v, nil
}

func typeErr(key, want string) *Error {
	return &Error{
		Msg:    fmt.Sprintf("config key %q must be %s", key, want),
		Remedy: "fix the value type in the config file",
	}
}

func mustString(key string, val any) string {
	s, _ := val.(string)
	return s
}

func asInt(key string, val any) (int, error) {
	switch n := val.(type) {
	case int64:
		return int(n), nil
	case int:
		return n, nil
	}
	return 0, typeErr(key, "an integer")
}

func asBool(key string, val any) (bool, error) {
	b, ok := val.(bool)
	if !ok {
		return false, typeErr(key, "a boolean")
	}
	return b, nil
}

func asStringList(key string, val any) ([]string, error) {
	raw, ok := val.([]any)
	if !ok {
		if s, ok := val.(string); ok {
			return []string{s}, nil
		}
		return nil, typeErr(key, "a list of strings")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, typeErr(key, "a list of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func ptrString(s string) *string { return &s }

// Merge layers later values over earlier ones (user < project < CLI).
func Merge(layers ...*Values) *Values {
	out := &Values{}
	for _, l := range layers {
		if l == nil {
			continue
		}
		mergeInto(out, l)
	}
	return out
}

func mergeInto(dst, src *Values) {
	if src.Agent != nil {
		dst.Agent = src.Agent
	}
	if src.Image != nil {
		dst.Image = src.Image
	}
	if src.CPUs != nil {
		dst.CPUs = src.CPUs
	}
	if src.Memory != nil {
		dst.Memory = src.Memory
	}
	if src.PidsLimit != nil {
		dst.PidsLimit = src.PidsLimit
	}
	if src.RO != nil {
		dst.RO = src.RO
	}
	if src.Ephemeral != nil {
		dst.Ephemeral = src.Ephemeral
	}
	if src.Quiet != nil {
		dst.Quiet = src.Quiet
	}
	if src.Net != nil {
		dst.Net = src.Net
	}
	if src.Env != nil {
		dst.Env = src.Env
	}
	if src.EnvFile != nil {
		dst.EnvFile = src.EnvFile
	}
	if src.StateDir != nil {
		dst.StateDir = src.StateDir
	}
	if src.Credentials != nil {
		dst.Credentials = src.Credentials
	}
	if src.User != nil {
		dst.User = src.User
	}
	if src.Name != nil {
		dst.Name = src.Name
	}
	if src.Keep != nil {
		dst.Keep = src.Keep
	}
	if src.WorkdirPath != nil {
		dst.WorkdirPath = src.WorkdirPath
	}
	if src.Engine != nil {
		dst.Engine = src.Engine
	}
	if src.NoCheck != nil {
		dst.NoCheck = src.NoCheck
	}
	if src.NoTTY != nil {
		dst.NoTTY = src.NoTTY
	}
	if src.NoUserConfig != nil {
		dst.NoUserConfig = src.NoUserConfig
	}
	if src.Build != nil {
		dst.Build = src.Build
	}
	if src.Force != nil {
		dst.Force = src.Force
	}
}

// Loaded is the resolved config plus the provenance paths.
type Loaded struct {
	Values     *Values
	Provenance []string
}

// Resolve discovers and loads config files: --config (or the user config)
// first, then the workspace agentbox.toml, merged project-over-user.
func Resolve(explicitConfig, workspace string, env func(string) string) (*Loaded, error) {
	loaded := &Loaded{Values: &Values{}}
	var user, project *Values

	userPath := explicitConfig
	explicit := explicitConfig != ""
	if !explicit {
		userPath = userConfigPath(env)
	}
	if userPath != "" {
		if _, err := os.Stat(userPath); err == nil {
			v, err := LoadUser(userPath)
			if err != nil {
				return nil, err
			}
			user = v
			loaded.Provenance = append(loaded.Provenance, userPath)
		} else if explicit {
			return nil, &Error{
				Msg:    fmt.Sprintf("config file %q does not exist", userPath),
				Remedy: "check the path passed to --config",
			}
		}
	}

	if workspace != "" {
		p := filepath.Join(workspace, "agentbox.toml")
		if _, err := os.Stat(p); err == nil {
			v, err := LoadProject(p, workspace)
			if err != nil {
				return nil, err
			}
			project = v
			loaded.Provenance = append(loaded.Provenance, p)
		}
	}

	loaded.Values = Merge(user, project)
	return loaded, nil
}

func userConfigPath(env func(string) string) string {
	if xdg := env("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "agentbox", "config.toml")
	}
	if home := env("HOME"); home != "" {
		return filepath.Join(home, ".config", "agentbox", "config.toml")
	}
	return ""
}
