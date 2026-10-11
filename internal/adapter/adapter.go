// Package adapter defines harness descriptors.
//
// Frozen contract (Wave 0) — changes require the integrator.
package adapter

import "agentbox/internal/engine"

// Adapter is a built-in harness descriptor.
type Adapter struct {
	Name     string          // pi|claude|opencode|shell
	Image    string          // e.g. agentbox-pi:<agentboxVer>-pi<harnessVer>
	Env      []engine.EnvVar // always-set container vars, e.g. PI_CODING_AGENT_DIR=/state/pi
	CredEnv  []string        // host-env allowlist names; BuildEnv emits EnvVar{Set: hostHas(k)} (bare -e) per FR-6
	CredFile string          // container target for --credentials ("" = none)
	// HostCredFile is the host credential file relative to $HOME (e.g.
	// ".pi/agent/auth.json"), used by `agentbox auth` to report a detected
	// credential file. Empty when the harness keeps credentials elsewhere
	// (opencode stores them in its state DB).
	HostCredFile string
	// HostCredExport, when set, is the host command whose stdout carries the
	// credentials (opencode keeps them in its database and exports them with
	// `opencode auth export`). `agentbox auth copy` uses it; HostCredFile is
	// used otherwise.
	HostCredExport []string
	Home           string   // container HOME; "" => /state/home; runner emits AGENTBOX_HOME (FR-9)
	DefaultArgs    []string // args passed to the harness in the image
	EphemeralArgs  []string // appended when --ephemeral (pi: ["--no-session"])
	// TrailerArgs are appended after the user args. opencode needs this: its
	// --standalone flag is rejected before a subcommand (but works last), and
	// user args must stay in their documented position.
	TrailerArgs []string

	// CredImportArgs, when set, is the harness subcommand that imports the
	// mounted --credentials file into the harness's own store (opencode keeps
	// credentials in its per-project database). The runner appends the
	// credential target and TrailerArgs and the entrypoint runs it once per
	// credential revision. Empty for harnesses that read the file directly.
	CredImportArgs []string

	// LoginArgs starts the harness's interactive authentication flow
	// (opencode: ["auth", "login"]). Empty means the harness authenticates
	// from inside its TUI; LoginHint then names the in-TUI command.
	LoginArgs []string
	LoginHint string

	// CredExportArgs exports credentials from inside the container
	// (opencode: ["auth", "export"]), used by `agentbox quickstart` to adopt
	// a credential created by an in-container login. Empty for file-based
	// harnesses, whose CredFile is read from the login state directory.
	CredExportArgs []string
	UseEntrypoint  bool // true: force --entrypoint /entrypoint.sh (built-ins, incl. --image override); false: image's own (image:<ref>)

	// ConfigDir is the container path of the harness config directory (the
	// same path its Env var points at). Empty means the adapter has no user
	// config directory and no persistent user folder.
	ConfigDir string

	// UserDirs are config subdirectories that persist across projects in the
	// agentbox user folder (~/.agentbox/agents/<name>/): plugins, skills,
	// themes, and similar user-level content. Sessions, auth, and history are
	// deliberately NOT listed: they stay in the per-project state volume so
	// project A cannot read project B's sessions (FR-5).
	UserDirs []string

	// UserExclude lists config entries that are never mounted from the user
	// folder even if present (sessions, auth, history). It makes
	// `agentbox config link <harness>` safe with a whole host config folder.
	UserExclude []string

	// HostConfigDir is the harness's host config directory relative to $HOME
	// (e.g. ".pi/agent"). HostConfigXDG, when set, means the directory lives
	// under $XDG_CONFIG_HOME/<value> instead (falling back to ~/.config).
	// Used by `agentbox config link|copy` to discover the existing folder.
	HostConfigDir string
	HostConfigXDG string

	// WarmupCache is a container path the harness populates on its first run
	// (e.g. pi's jiti transpile cache). When it does not exist yet, the
	// entrypoint prints a one-line notice so the slower first run is not
	// mistaken for a hang. Empty means the harness has no warm-up cache.
	WarmupCache string
}
