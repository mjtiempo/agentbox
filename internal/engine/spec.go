// Package engine wraps the container engine CLI (docker/podman).
//
// # Frozen contracts (Wave 0) — changes require the integrator
//
// RunArgv is pure and golden-testable. The rendered dry-run format is one
// shell-quoted argv token per line, in exactly this order:
//
//	<engine> run
//	lifecycle   (--rm --init, plus -it when TTY)
//	--name
//	-w          (RunSpec.Workdir; FR-2 container cwd)
//	labels
//	identity    (--user, --userns)
//	--net
//	caps        (--cap-drop ALL, one --cap-add per value, --security-opt no-new-privileges)
//	limits      (--cpus, --memory, --pids-limit)
//	--tmpfs
//	mounts      (-v, options joined z,ro / z,rw / ro / rw as applicable)
//	env         (-e)
//	--entrypoint (only when non-empty)
//	image
//	cmd
//
// New flags land in their section only. Secret-ish env values render as
// <set>/<unset> in --dry-run; AGENTBOX_TEST_NAME pins name/run uuid/owner
// label; TTY detection governs -it.
//
// # The `--` boundary
//
// The CLI parser consumes `--`: everything after it is forwarded verbatim to
// the agent binary as Cmd tail elements. `--` itself is never forwarded as an
// argv element (policy: bare `--` is not part of the in-container command line).
//
// # Env rendering rule (frozen)
//
//	Set=false            => omit the flag entirely (never pass empty)
//	Set=true, Value==""  => "-e Name" (value read from agentbox's own env)
//	Set=true, Value!=""  => "-e Name=Value"
//
// EnvVar.Secret marks values that must never appear in argv: the exec path
// renders them as a bare "-e Name" and injects the value into the engine
// process environment, so secrets stay out of host process listings.
package engine

import (
	"context"
	"io"
	"os"
)

// Mount is a single bind/volume mount.
type Mount struct {
	Host, Container string
	ReadOnly        bool
	Relabel         string // "" | "z" (FR-2 SELinux relabel); rendered as a mount option
}

// EnvVar describes one container environment variable.
type EnvVar struct {
	Name   string // e.g. ANTHROPIC_API_KEY
	Value  string // literal value; "" => bare "-e Name" (value read from agentbox's own env)
	Set    bool   // host-side presence; false => omit the flag entirely (never pass empty)
	Secret bool   // true => never render the value; dry-run shows <set>/<unset>, exec uses bare -e
}

// RunSpec is the single source of truth for what runs.
type RunSpec struct {
	Name      string   // container name (AGENTBOX_TEST_NAME overrides)
	Image     string   // ensure/pull happens in the engine layer before Run (FR-10/E3); not part of RunArgv
	Cmd       []string // [adapter args] [user args]; entrypoint comes from the image or --entrypoint, never argv; CLI "--" is not forwarded
	Env       []EnvVar
	Workspace Mount
	State     *Mount // nil when --ephemeral

	// ExtraMounts are additional read-only/aux mounts (--credentials). They
	// render in the mounts section after the state mount. Integration decision:
	// RunSpec gained this field in Wave 2 for FR-6 P1 credential mounts.
	ExtraMounts []Mount
	Net         string   // bridge|none|host
	Rm          bool     // always true (FR-3) until --keep (P2)
	Workdir     string   // "/workspace" (FR-2) until --workdir-path (P2)
	CapDrop     []string // ["ALL"] (FR-8)
	CapAdd      []string // ["CHOWN","SETUID","SETGID"] (FR-8/FR-9); each rendered as its own --cap-add
	NoNewPriv   bool     // always true (FR-8)
	Init        bool     // always true (FR-3)
	Tmpfs       []string // ["/tmp:size=256m"] (FR-8)
	CPUs        string   // "2"
	Memory      string   // "4g"
	PidsLimit   int
	TTY         bool              // "-it" when host stdin is a TTY and !--no-tty
	User        string            // "" unless --user or image:<ref>
	Userns      string            // "" | "keep-id" (Podman ownership, FR-9 P2; D2); rendered --userns=<value>
	ReadOnly    bool              // workspace readonly (AGENTBOX_READONLY=1 marker too)
	Labels      map[string]string // agentbox=1, agentbox.run, agentbox.project, agentbox.uid, agentbox.owner
	Keep        bool              // P2: do not pass --rm

	// Entrypoint is rendered as `--entrypoint <value>` only when non-empty
	// (Adapter.UseEntrypoint => "/entrypoint.sh"). image:<ref> leaves it empty
	// so the image's own entrypoint runs (FR-4 P1). Integration decision:
	// RunSpec gained this field in Wave 1; the ordering slot is between env and
	// image, exactly as listed in the frozen format above.
	Entrypoint string
}

// Engine is the container-engine abstraction.
type Engine interface {
	Name() string // "docker"|"podman"

	// RunArgv is pure: it builds the full engine argv from a RunSpec.
	RunArgv(RunSpec) []string

	// Run executes argv, returning the engine's observed exit code.
	Run(ctx context.Context, argv []string) (int, error)

	// RunStream executes argv with the given stdio attached. It returns the
	// engine's observed exit code.
	RunStream(ctx context.Context, argv []string, stdio Stdio) (int, error)

	// DryRunArgv renders the same argv with secret values annotated
	// <set>/<unset> for --dry-run (FR-11/AC 15).
	DryRunArgv(spec RunSpec, getenv func(string) string) []string
}

// Stdio bundles the streams and process environment handed to the engine.
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	Env []string
	Dir string

	// OnStart, when set, receives the engine process as soon as it starts so
	// the caller can forward signals to it (FR-1).
	OnStart func(*os.Process)
}
