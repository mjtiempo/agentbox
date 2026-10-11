package engine

import (
	"fmt"
	"sort"
	"strings"
)

// canonicalLabelOrder keeps the label block byte-stable for goldens and
// matches the PRD §8.1 example ordering.
var canonicalLabelOrder = []string{
	"agentbox", "agentbox.run", "agentbox.project", "agentbox.uid", "agentbox.owner",
}

// base is the shared argv builder for docker and podman grammar.
type base struct {
	name string
}

func (b *base) Name() string { return b.name }

func (b *base) RunArgv(spec RunSpec) []string { return b.buildArgv(spec, false, nil) }

// DryRunArgv renders the copy-pasteable argv with secret values annotated.
// getenv reports host-side presence for bare secret vars (use os.Getenv).
func (b *base) DryRunArgv(spec RunSpec, getenv func(string) string) []string {
	return b.buildArgv(spec, true, getenv)
}

func (b *base) buildArgv(spec RunSpec, dry bool, getenv func(string) string) []string {
	var argv []string
	add := func(tokens ...string) { argv = append(argv, tokens...) }

	add(b.name, "run")

	// lifecycle
	if !spec.Keep {
		add("--rm")
	}
	if spec.Init {
		add("--init")
	}
	if spec.TTY {
		add("-it")
	}

	// name
	if spec.Name != "" {
		add("--name", spec.Name)
	}

	// workdir (FR-2: container cwd is /workspace; --workdir-path overrides)
	if spec.Workdir != "" {
		add("-w", spec.Workdir)
	}

	// labels
	if len(spec.Labels) > 0 {
		add("--label", "agentbox=1")
		for _, k := range orderedLabelKeys(spec.Labels) {
			if k == "agentbox" {
				continue
			}
			add("--label", k+"="+spec.Labels[k])
		}
	}

	// identity
	if spec.User != "" {
		add("--user", spec.User)
	}
	if spec.Userns != "" {
		add("--userns=" + spec.Userns)
	}

	// network
	if spec.Net != "" {
		add("--net", spec.Net)
	}

	// caps & hardening
	for _, c := range spec.CapDrop {
		add("--cap-drop", c)
	}
	for _, c := range spec.CapAdd {
		add("--cap-add", c)
	}
	if spec.NoNewPriv {
		add("--security-opt", "no-new-privileges")
	}

	// limits
	if spec.CPUs != "" {
		add("--cpus", spec.CPUs)
	}
	if spec.Memory != "" {
		add("--memory", spec.Memory)
	}
	if spec.PidsLimit > 0 {
		add("--pids-limit", fmt.Sprint(spec.PidsLimit))
	}

	// tmpfs
	for _, t := range spec.Tmpfs {
		add("--tmpfs", t)
	}

	// mounts
	if spec.Workspace.Host != "" {
		add("-v", mountSpec(spec.Workspace, true))
	}
	if spec.State != nil && spec.State.Host != "" {
		add("-v", mountSpec(*spec.State, false))
	}
	for _, m := range spec.ExtraMounts {
		if m.Host == "" {
			continue
		}
		add("-v", mountSpec(m, false))
	}

	// env
	for _, e := range spec.Env {
		if !e.Set {
			continue
		}
		name := e.Name
		if e.Secret && dry {
			if e.Value != "" || (getenv != nil && getenv(e.Name) != "") {
				name += "=<set>"
			} else {
				name += "=<unset>"
			}
		}
		if e.Secret || e.Value == "" {
			add("-e", name)
			continue
		}
		add("-e", e.Name+"="+e.Value)
	}

	// entrypoint
	if spec.Entrypoint != "" {
		add("--entrypoint", spec.Entrypoint)
	}

	// image + cmd
	add(spec.Image)
	add(spec.Cmd...)
	return argv
}

func orderedLabelKeys(labels map[string]string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range canonicalLabelOrder {
		if _, ok := labels[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range labels {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// mountSpec renders host:container[:opts]. includeRW controls whether the
// implicit :rw is spelled out (workspace yes per PRD §8.1, state no).
func mountSpec(m Mount, includeRW bool) string {
	var opts []string
	if m.Relabel != "" {
		opts = append(opts, m.Relabel)
	}
	if m.ReadOnly {
		opts = append(opts, "ro")
	} else if includeRW {
		opts = append(opts, "rw")
	}
	if len(opts) == 0 {
		return m.Host + ":" + m.Container
	}
	return m.Host + ":" + m.Container + ":" + strings.Join(opts, ",")
}

// ShellQuote renders one argv token so a shell reproduces it exactly.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if isShellSafe(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isShellSafe(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("_@%+=:,./-", r):
		default:
			return false
		}
	}
	return true
}

// Render prints one shell-quoted argv token per line (frozen dry-run format).
func Render(argv []string) string {
	var b strings.Builder
	for _, a := range argv {
		b.WriteString(ShellQuote(a))
		b.WriteByte('\n')
	}
	return b.String()
}
