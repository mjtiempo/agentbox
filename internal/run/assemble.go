package run

import (
	"fmt"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
	"agentbox/internal/entrypoint"
)

// SpecInput is everything Assemble needs to produce an engine.RunSpec.
type SpecInput struct {
	Adapter   adapter.Adapter
	Image     string
	Naming    Naming
	Workspace string // canonical host path
	State     *engine.Mount
	Net       string
	CPUs      string
	Memory    string
	PidsLimit int
	User      string
	Userns    string
	ReadOnly  bool
	TTY       bool
	Keep      bool
	Workdir   string
	Relabel   string
	Env       []engine.EnvVar
	AgentArgs []string
	Ephemeral bool
	UID       int
}

// Assemble builds the RunSpec: hardening defaults, mounts, labels, entrypoint,
// and the FR-4 argument composition `[adapter defaults] [ephemeral args]
// [user args] [adapter trailer args]`.
func Assemble(in SpecInput) engine.RunSpec {
	cmd := append([]string{}, in.Adapter.DefaultArgs...)
	if in.Ephemeral {
		cmd = append(cmd, in.Adapter.EphemeralArgs...)
	}
	cmd = append(cmd, in.AgentArgs...)
	cmd = append(cmd, in.Adapter.TrailerArgs...)

	workdir := in.Workdir
	if workdir == "" {
		workdir = "/workspace"
	}
	entry := ""
	if in.Adapter.UseEntrypoint {
		entry = entrypoint.Path
	}
	state := in.State
	if in.Ephemeral {
		state = nil
	}
	labels := in.Naming.Labels(in.UID)
	if in.Keep {
		// Marked so the next run's startup sweep leaves it alone (FR-3).
		labels["agentbox.keep"] = "1"
	}
	return engine.RunSpec{
		Name:       in.Naming.Name,
		Image:      in.Image,
		Cmd:        cmd,
		Env:        in.Env,
		Workspace:  engine.Mount{Host: in.Workspace, Container: workdir, ReadOnly: in.ReadOnly, Relabel: in.Relabel},
		State:      state,
		Net:        in.Net,
		Workdir:    workdir,
		CapDrop:    []string{"ALL"},
		CapAdd:     []string{"CHOWN", "SETUID", "SETGID"},
		NoNewPriv:  true,
		Init:       true,
		Tmpfs:      []string{"/tmp:size=256m"},
		CPUs:       in.CPUs,
		Memory:     in.Memory,
		PidsLimit:  in.PidsLimit,
		TTY:        in.TTY,
		User:       in.User,
		Userns:     in.Userns,
		ReadOnly:   in.ReadOnly,
		Labels:     labels,
		Keep:       in.Keep,
		Entrypoint: entry,
	}
}

// ResolveImage applies the --image override over the adapter default.
func ResolveImage(override string, a adapter.Adapter) string {
	if override != "" {
		return override
	}
	return a.Image
}

// Banner is the single stderr line printed before handing over the TTY.
func Banner(agent, workdir, canonical, mode, net, state string) string {
	return fmt.Sprintf("agentbox: %s @ %s \u2190 %s (%s, net: %s, state: %s)",
		agent, workdir, canonical, mode, net, state)
}

func workspaceMode(readOnly bool) string {
	if readOnly {
		return "ro"
	}
	return "rw"
}

// stateDescription renders the banner's state field: hash8 for a named
// volume, the host path for --state-dir, or "ephemeral". A project id that
// carries no <hash8> suffix (AGENTBOX_TEST_NAME pins it) is shown as-is so the
// banner stays deterministic in tests.
func stateDescription(ephemeral bool, stateDir, project string) string {
	if ephemeral {
		return "ephemeral"
	}
	if stateDir != "" {
		return stateDir
	}
	if len(project) > 9 && project[len(project)-9] == '-' && isHex8(project[len(project)-8:]) {
		return project[len(project)-8:]
	}
	return project
}

func isHex8(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
