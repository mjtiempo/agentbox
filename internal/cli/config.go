package cli

import (
	"agentbox/internal/config"
)

// ApplyConfig merges a resolved config layer into the parsed options.
// Precedence is CLI > project > user: a value is applied only when the user
// did not set the corresponding flag (FR-12).
func ApplyConfig(o *Options, v *config.Values) {
	if v == nil {
		return
	}
	if v.Agent != nil && !o.Explicit["agent"] {
		o.Agent = *v.Agent
	}
	if v.Image != nil && !o.Explicit["image"] {
		o.Image = *v.Image
	}
	if v.CPUs != nil && !o.Explicit["cpus"] {
		o.CPUs = *v.CPUs
	}
	if v.Memory != nil && !o.Explicit["memory"] {
		o.Memory = *v.Memory
	}
	if v.PidsLimit != nil && !o.Explicit["pids-limit"] {
		o.PidsLimit = *v.PidsLimit
	}
	if v.RO != nil && !o.Explicit["ro"] {
		o.ReadOnly = *v.RO
	}
	if v.Ephemeral != nil && !o.Explicit["ephemeral"] {
		o.Ephemeral = *v.Ephemeral
	}
	if v.Quiet != nil && !o.Explicit["quiet"] {
		o.Quiet = *v.Quiet
	}
	if v.Net != nil && !o.Explicit["net"] {
		o.Net = *v.Net
	}
	if len(v.Env) > 0 && !o.Explicit["env"] {
		o.Env = append([]string{}, v.Env...)
	}
	if len(v.EnvFile) > 0 && !o.Explicit["env-file"] {
		o.EnvFiles = append([]string{}, v.EnvFile...)
	}
	if v.StateDir != nil && !o.Explicit["state-dir"] {
		o.StateDir = *v.StateDir
	}
	if len(v.Credentials) > 0 && !o.Explicit["credentials"] {
		o.Credentials = append([]string{}, v.Credentials...)
	}
	if v.User != nil && !o.Explicit["user"] {
		o.User = *v.User
	}
	if v.Name != nil && !o.Explicit["name"] {
		o.Name = *v.Name
	}
	if v.Keep != nil && !o.Explicit["keep"] {
		o.Keep = *v.Keep
	}
	if v.WorkdirPath != nil && !o.Explicit["workdir-path"] {
		o.WorkdirPath = *v.WorkdirPath
	}
	if v.Engine != nil && !o.Explicit["engine"] {
		o.Engine = *v.Engine
	}
	if v.NoCheck != nil && !o.Explicit["no-check"] {
		o.NoCheck = *v.NoCheck
	}
	if v.NoTTY != nil && !o.Explicit["no-tty"] {
		o.NoTTY = *v.NoTTY
	}
	if v.NoUserConfig != nil && !o.Explicit["no-user-config"] {
		o.NoUserConfig = *v.NoUserConfig
	}
	if v.Build != nil && !o.Explicit["build"] {
		o.Build = *v.Build
	}
	if v.Force != nil && !o.Explicit["force"] {
		o.Force = *v.Force
	}
}
