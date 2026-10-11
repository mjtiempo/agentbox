package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"agentbox/internal/adapter"
	"agentbox/internal/auth"
	"agentbox/internal/authstore"
	"agentbox/internal/engine"
	"agentbox/internal/state"
	"agentbox/internal/tty"
	"agentbox/internal/workspace"
)

// Options is the fully parsed run configuration. main.go converts
// cli.Options into this; the run package does not import cli.
type Options struct {
	Version     string
	Workspace   string
	Agent       string
	Image       string
	DryRun      bool
	Quiet       bool
	ReadOnly    bool
	Force       bool
	Ephemeral   bool
	StateDir    string
	Env         []string
	EnvFiles    []string
	Credentials []string
	Net         string
	CPUs        string
	Memory      string
	PidsLimit   int
	User        string
	Engine      string
	Name        string
	Keep        bool
	WorkdirPath string
	Config      string
	Build       bool
	NoCheck     bool
	NoTTY       bool
	AgentArgs   []string

	// NoUserConfig disables the persistent per-harness user folder
	// (~/.agentbox/agents/<harness>).
	NoUserConfig bool

	// EngineInstance injects an engine for tests; nil resolves normally.
	EngineInstance engine.Engine
}

// Host is the process context, injectable for tests.
type Host struct {
	Env      func(string) string
	Environ  []string
	UID      int
	GID      int
	PID      int
	BootID   string
	LookPath func(string) (string, error)
	IsTTY    bool
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
}

// DefaultHost builds the real process host.
func DefaultHost() Host {
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return Host{
		Env:      os.Getenv,
		Environ:  os.Environ(),
		UID:      os.Getuid(),
		GID:      os.Getgid(),
		PID:      os.Getpid(),
		BootID:   strings.TrimSpace(string(boot)),
		LookPath: exec.LookPath,
		IsTTY:    tty.StdinIsTerminal(),
		In:       os.Stdin,
		Out:      os.Stdout,
		Err:      os.Stderr,
	}
}

const (
	usageExit  = 2
	errorExit  = 1
	graceDelay = 5 * time.Second
)

// Run executes the agent run. Exit codes: agent passthrough (128+signal),
// agentbox failures 1, usage/validation failures 2 (FR-1/§6).
func Run(opts Options, host Host) int {
	ctx := context.Background()

	// --- workspace (FR-2) -------------------------------------------------
	canonical, err := workspace.Canonical(opts.Workspace)
	if err != nil {
		return fail(host, fmt.Sprintf("cannot resolve workspace %q", opts.Workspace), "check the path")
	}
	if err := workspace.Validate(opts.Workspace, workspace.Options{Home: host.Env("HOME"), Force: opts.Force}); err != nil {
		fmt.Fprint(host.Err, renderError(err))
		return errorExit
	}

	// --- semantic flag conflicts (FR-1) ----------------------------------
	if opts.Ephemeral && opts.StateDir != "" {
		return usage(host, "--ephemeral cannot be combined with --state-dir", "pick one: --ephemeral discards state, --state-dir keeps it in a host directory")
	}
	if strings.HasPrefix(opts.Agent, "image:") && opts.Image != "" {
		return usage(host, "--agent image:<ref> cannot be combined with --image", "the image reference already names the image")
	}
	if opts.Agent == "" {
		opts.Agent = "pi"
	}
	if opts.Net == "" {
		opts.Net = "bridge"
	}

	// --- engine (FR-3) ----------------------------------------------------
	eng, err := resolveEngineFor(opts, host)
	if err != nil {
		return fail(host, err.Error(), "")
	}
	// One listing call serves three purposes: it proves the engine is
	// reachable (FR-3 remedy on failure), feeds the concurrent-run warning, and
	// provides the rows for the stale-container sweep.
	var ownedContainers []ContainerInfo
	if !opts.DryRun {
		ownedContainers, err = ListOwnContainers(ctx, eng, host.UID)
		if err != nil {
			reach := &engine.ReachError{Engine: eng.Name()}
			fmt.Fprint(host.Err, reach.Error()+"\n")
			return errorExit
		}
	}

	// --- adapter (FR-4) ---------------------------------------------------
	ad, err := adapter.Get(opts.Agent, opts.Version)
	if err != nil {
		return fail(host, err.Error(), "")
	}

	// --- env files --------------------------------------------------------
	var kvs []KV
	for _, f := range opts.EnvFiles {
		parsed, err := ParseEnvFile(f)
		if err != nil {
			return fail(host, fmt.Sprintf("cannot read --env-file %q", f), "check the path; values are never echoed")
		}
		kvs = append(kvs, parsed...)
	}

	// --- credentials (FR-6 P1) -------------------------------------------
	// Explicit --credentials wins; otherwise a credential adopted with
	// `agentbox auth copy <harness>` is used automatically (never implicitly
	// adopted: the user ran that command once).
	credFiles := opts.Credentials
	credFromStore := ""
	if len(credFiles) == 0 && ad.CredFile != "" {
		if _, path, ok, loadErr := authstore.Load(UserRoot(host), ad.Name); loadErr == nil && ok {
			credFiles = []string{path}
			credFromStore = path
		}
	}
	credMounts, err := credentialMounts(ad, credFiles)
	if err != nil {
		return fail(host, err.Error(), "")
	}

	// --- identity ---------------------------------------------------------
	naming := BuildNaming(NamingInput{
		TestName:  host.Env("AGENTBOX_TEST_NAME"),
		Canonical: canonical,
		UID:       host.UID,
		PID:       host.PID,
		BootID:    host.BootID,
	})
	if opts.Name != "" {
		naming.Name = opts.Name
	}

	// --- user / image:<ref> (FR-9 P1) ------------------------------------
	user, errCode := resolveUser(opts, host, ad)
	if errCode != 0 {
		return errCode
	}

	// --- state (FR-5) -----------------------------------------------------
	relabel := workspace.SELinuxRelabel()
	var st *engine.Mount
	if !opts.Ephemeral {
		if opts.StateDir != "" {
			st = state.DirMount(opts.StateDir, relabel)
		} else {
			st = state.VolumeMount(host.UID, canonical, relabel)
		}
	}
	workdir := opts.WorkdirPath
	if workdir == "" {
		workdir = "/workspace"
	}

	// --- environment ------------------------------------------------------
	envs, notices, warns := buildEnvironment(ad, opts, host, kvs)
	if credFromStore != "" {
		notices = append(notices, fmt.Sprintf(
			"using credentials copied with `agentbox auth copy %s` (%s)", ad.Name, credFromStore))
	}
	home := ad.Home
	if home == "" {
		home = state.DefaultHome
	}

	spec := Assemble(SpecInput{
		Adapter:   ad,
		Image:     ResolveImage(opts.Image, ad),
		Naming:    naming,
		Workspace: canonical,
		State:     st,
		Net:       opts.Net,
		CPUs:      opts.CPUs,
		Memory:    opts.Memory,
		PidsLimit: opts.PidsLimit,
		User:      user,
		Userns:    UsernsFor(eng.Name()),
		ReadOnly:  opts.ReadOnly,
		TTY:       host.IsTTY && !opts.NoTTY,
		Keep:      opts.Keep,
		Workdir:   workdir,
		Relabel:   relabel,
		Env:       envs,
		AgentArgs: opts.AgentArgs,
		Ephemeral: opts.Ephemeral,
		UID:       host.UID,
	})
	spec.ExtraMounts = credMounts
	if len(credMounts) > 0 {
		// The entrypoint chowns this path's parent chain: Docker creates the
		// mount destination as root inside the state volume, which would block
		// the agent from writing siblings (opencode's data dir needs this).
		spec.Env = append(spec.Env, engine.EnvVar{Name: "AGENTBOX_CRED_FILE", Value: ad.CredFile, Set: true})
		if len(ad.CredImportArgs) > 0 {
			// Harness that stores credentials in its own DB: the entrypoint
			// imports the mounted file once per credential revision.
			importArgv := append([]string{}, ad.CredImportArgs...)
			importArgv = append(importArgv, ad.CredFile)
			importArgv = append(importArgv, ad.TrailerArgs...)
			spec.Env = append(spec.Env, engine.EnvVar{
				Name: "AGENTBOX_CRED_IMPORT", Value: strings.Join(importArgv, " "), Set: true,
			})
		}
	}

	// --- persistent user folder (~/.agentbox/agents/<harness>) ------------
	// User-level harness content (plugins, skills, themes, settings) survives
	// across sessions and projects; sessions and auth stay per-project.
	userMounts, userNotice, err := userConfigMounts(ad, host, relabel, opts.NoUserConfig)
	if err != nil {
		return fail(host, err.Error(), "check permissions on the agentbox user folder")
	}
	if userNotice != "" && !opts.Quiet {
		fmt.Fprintf(host.Err, "agentbox: note: %s\n", userNotice)
	}
	spec.ExtraMounts = append(spec.ExtraMounts, userMounts...)

	// --- credential preflight notice (FR-6 P1) ----------------------------
	// Printed only when a harness accepts credentials and no mechanism at all
	// is detected; never contains values. A pre-existing state volume (or a
	// user-managed --state-dir) may hold a previous in-container login, so a
	// fresh volume is not counted as a mechanism.
	if len(ad.CredEnv) > 0 && !opts.DryRun && len(opts.Credentials) == 0 && !opts.Quiet {
		statePossible := false
		if st != nil {
			if opts.StateDir != "" {
				statePossible = true
			} else {
				statePossible = volumeExists(ctx, eng, st.Host)
			}
		}
		res := auth.Detect(ad, auth.Probes{
			Getenv: host.Env,
			CredFileExists: func(p string) bool {
				st, statErr := os.Stat(p)
				return statErr == nil && !st.IsDir()
			},
			StateExists: func() bool { return statePossible },
		})
		if res.Mechanism == "none" {
			fmt.Fprintf(host.Err, "agentbox: %s\n", auth.PreflightNotice(ad))
		}
	}

	// --- banner, notices, force echo (FR-11) ------------------------------
	if opts.Force {
		fmt.Fprintf(host.Err, "agentbox: --force: workspace guardrail overridden for %s\n", canonical)
	}
	if !opts.Quiet {
		fmt.Fprintln(host.Err, Banner(ad.Name, workdir, canonical,
			workspaceMode(opts.ReadOnly), opts.Net,
			stateDescription(opts.Ephemeral, opts.StateDir, naming.Project)))
		if opts.Net == "host" {
			fmt.Fprintln(host.Err, "agentbox: warning: --net host breaks the network boundary — the container shares the host network namespace")
		}
		for _, n := range notices {
			fmt.Fprintf(host.Err, "agentbox: %s\n", n)
		}
		for _, w := range warns {
			fmt.Fprintf(host.Err, "agentbox: warning: %s\n", w)
		}
		for _, n := range workspace.Scan(canonical) {
			fmt.Fprintf(host.Err, "agentbox: %s\n", n)
		}
	}

	// --- dry-run (FR-11/AC 21) --------------------------------------------
	if opts.DryRun {
		fmt.Fprint(host.Out, engine.Render(eng.DryRunArgv(spec, host.Env)))
		return 0
	}

	// FR-9 P1: a fresh named volume is root-owned, and the entrypoint skips
	// chown when it starts as a non-root user; fail up front with a remedy
	// instead of a late permission error.
	if user != "" && user != "0" && user != "0:0" && st != nil && opts.StateDir == "" {
		if !volumeExists(ctx, eng, st.Host) {
			return usage(host,
				"--user with a fresh per-project state volume would be unwritable",
				"pass --ephemeral, or --state-dir pointing at a directory owned by that uid")
		}
	}

	// --- ensure state volume ---------------------------------------------
	if st != nil && opts.StateDir == "" {
		if err := ensureVolume(ctx, eng, st.Host, state.VolumeLabels(host.UID, naming.Project, canonical)); err != nil {
			return fail(host, "cannot create the state volume", "check that the engine is running; try: docker volume ls")
		}
	}

	// --- concurrent-run warning (FR-5) ------------------------------------
	if !opts.Ephemeral && opts.StateDir == "" && RunningProjectRuns(ownedContainers, naming.Project) > 0 {
		if !opts.Quiet {
			fmt.Fprintf(host.Err, "agentbox: %s\n", state.ConcurrentWarning(naming.Project))
		}
	}

	// --- image availability (FR-10) ---------------------------------------
	if opts.Build {
		if isImageRef(opts.Agent) {
			return usage(host, "--build cannot be used with --agent image:<ref>",
				"the image reference already names an image")
		}
		root := findRepoRoot()
		if root == "" {
			return fail(host, "--build needs the agentbox source checkout (images/<harness>/Dockerfile)",
				"run `make image` from a checkout, or drop --build to use the published image")
		}
		if !opts.Quiet {
			fmt.Fprintf(host.Err, "agentbox: building image %s from %s\n", spec.Image, root)
		}
		if err := BuildImage(ctx, eng, ad, opts.Version, root); err != nil {
			return fail(host, err.Error(), "check the Dockerfile; AGENTBOX_DEBUG=1 shows the engine command")
		}
	} else if !imageExists(ctx, eng, spec.Image) {
		if !opts.Quiet {
			fmt.Fprintf(host.Err, "agentbox: pulling image %s\n", spec.Image)
		}
		if _, err := engine.Capture(ctx, eng, []string{eng.Name(), "pull", spec.Image}); err != nil {
			return fail(host, "cannot pull image "+spec.Image,
				"check the reference, or build the local image with `make image`")
		}
	}

	// --- startup sweep (FR-3) ---------------------------------------------
	if removed, err := SweepStale(ctx, eng, ownedContainers, host.BootID, ProcAlive); err == nil && len(removed) > 0 {
		if host.Env("AGENTBOX_DEBUG") == "1" {
			fmt.Fprintf(host.Err, "agentbox: swept %d stale container(s)\n", len(removed))
		}
	}

	// --- exec --------------------------------------------------------------
	argv := eng.RunArgv(spec)
	if host.Env("AGENTBOX_DEBUG") == "1" {
		fmt.Fprint(host.Err, engine.Render(eng.DryRunArgv(spec, host.Env)))
	}

	var proc atomic.Pointer[os.Process]
	stdio := engine.Stdio{
		In:      host.In,
		Out:     host.Out,
		Err:     host.Err,
		Env:     ChildEnv(host.Environ, kvs),
		OnStart: func(p *os.Process) { proc.Store(p) },
	}

	sigCh := make(chan os.Signal, 4)
	sigList := []os.Signal{syscall.SIGTERM, syscall.SIGHUP}
	if !spec.TTY {
		sigList = append(sigList, syscall.SIGINT)
	}
	signal.Notify(sigCh, sigList...)
	defer signal.Stop(sigCh)

	done := make(chan struct{})
	forceRemove := func() {
		_ = RemoveOwn(context.Background(), eng, naming.RunID)
	}
	go watchSignals(sigCh,
		func() *os.Process { return proc.Load() },
		forceRemove, graceDelay, done, os.Exit)

	code, err := eng.RunStream(ctx, argv, stdio)
	close(done)
	if err != nil {
		fmt.Fprintf(host.Err, "agentbox: %v\n", err)
		code = errorExit
	}

	// --- cleanup (FR-3): only this run's container -------------------------
	// With --rm, a clean engine exit already removed the container; the extra
	// query is only needed when the engine process itself failed (err != nil)
	// or a signal handler already force-removed it.
	if !opts.Keep && err != nil {
		_ = RemoveOwn(context.Background(), eng, naming.RunID)
	}

	mapped, hint := engine.MapExit(code)
	if hint != "" {
		fmt.Fprintf(host.Err, "agentbox: %s\n", hint)
	}
	if mapped != 0 {
		fmt.Fprintf(host.Err, "agentbox: exit %d\n", mapped)
	}
	return mapped
}

// resolveUser applies --user and the image:<ref> ownership rules (FR-9 P1).
func resolveUser(opts Options, host Host, ad adapter.Adapter) (string, int) {
	user := opts.User
	if strings.HasPrefix(opts.Agent, "image:") {
		if user == "" {
			user = fmt.Sprintf("%d:%d", host.UID, host.GID)
		}
		if !opts.Ephemeral && opts.StateDir == "" {
			return "", usage(host,
				"--agent image:<ref> needs --ephemeral or a writable --state-dir",
				"the image has no agentbox entrypoint to prepare state; pass --ephemeral, or --state-dir with a directory owned by your uid")
		}
	}
	if user == "0" || user == "0:0" {
		fmt.Fprintln(host.Err, "agentbox: warning: --user 0 runs the agent as root (debug only)")
	}
	return user, 0
}

// buildEnvironment assembles the container env: identity, adapter config,
// read-only marker, allowlisted credentials, --env, and env-file keys.
func buildEnvironment(ad adapter.Adapter, opts Options, host Host, kvs []KV) ([]engine.EnvVar, []string, []string) {
	home := ad.Home
	if home == "" {
		home = state.DefaultHome
	}
	envs := []engine.EnvVar{
		{Name: "AGENTBOX_UID", Value: strconv.Itoa(host.UID), Set: true},
		{Name: "AGENTBOX_GID", Value: strconv.Itoa(host.GID), Set: true},
		{Name: "AGENTBOX_HOME", Value: home, Set: true},
	}
	envs = append(envs, ad.Env...)
	if ad.ConfigDir != "" {
		// The entrypoint chowns this chain so the agent can create session
		// dirs next to the persistent user-folder mounts.
		envs = append(envs, engine.EnvVar{Name: "AGENTBOX_CONFIG_DIR", Value: ad.ConfigDir, Set: true})
	}
	if ad.WarmupCache != "" {
		// The entrypoint prints a first-run notice when this cache does not
		// exist yet, so the slower first start is not mistaken for a hang.
		envs = append(envs, engine.EnvVar{Name: "AGENTBOX_CACHE_DIR", Value: ad.WarmupCache, Set: true})
	}
	if opts.Quiet {
		envs = append(envs, engine.EnvVar{Name: "AGENTBOX_QUIET", Value: "1", Set: true})
	}
	if opts.ReadOnly {
		envs = append(envs, engine.EnvVar{Name: "AGENTBOX_READONLY", Value: "1", Set: true})
	}
	// Terminal capability variables: the engine does not propagate TERM, and
	// TUI harnesses need it (plus COLORTERM for truecolor). Bare -e keeps the
	// value out of argv. Only in TTY mode.
	if host.IsTTY && !opts.NoTTY {
		for _, name := range []string{"TERM", "COLORTERM"} {
			if host.Env(name) != "" {
				envs = append(envs, engine.EnvVar{Name: name, Set: true})
			}
		}
	}
	creds, notices, warns := adapter.BuildEnv(ad, host.Env, opts.Env)
	envs = append(envs, creds...)
	for _, kv := range kvs {
		envs = append(envs, engine.EnvVar{Name: kv.Key, Set: true, Secret: true})
	}
	return envs, notices, warns
}

// credentialMounts maps --credentials host files onto the adapter's expected
// container target, read-only (FR-6 P1).
func credentialMounts(ad adapter.Adapter, files []string) ([]engine.Mount, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if ad.CredFile == "" {
		return nil, fmt.Errorf("harness %q has no credential file target; use --env or a custom image", ad.Name)
	}
	var mounts []engine.Mount
	seen := map[string]bool{}
	for _, f := range files {
		if f == "" {
			continue
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		st, err := os.Stat(f)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("credential file %q does not exist", f)
			}
			return nil, fmt.Errorf("cannot read credential file %q (check permissions)", f)
		}
		if st.IsDir() {
			return nil, fmt.Errorf("credential file %q is a directory, not a file", f)
		}
		mounts = append(mounts, engine.Mount{Host: f, Container: ad.CredFile, ReadOnly: true})
	}
	return mounts, nil
}

// UsernsFor returns the engine-specific user-namespace mode. Podman needs
// --userns=keep-id so the dropped host uid maps to itself and workspace files
// stay host-owned (FR-9 P2). Docker's --userns accepts only host/private, so
// it gets nothing.
func UsernsFor(engineName string) string {
	if engineName == "podman" {
		return "keep-id"
	}
	return ""
}

// ResolveEngine resolves the engine for a subcommand or run: explicit
// --engine, else AGENTBOX_ENGINE, else auto-detect.
func ResolveEngine(name string, host Host) (engine.Engine, error) {
	switch name {
	case "":
		return engine.Detect(host.Env, host.LookPath)
	case "docker":
		return engine.NewDocker(), nil
	case "podman":
		return engine.NewPodman(), nil
	default:
		return nil, fmt.Errorf("invalid --engine value %q (valid: docker, podman)", name)
	}
}

func ensureVolume(ctx context.Context, e engine.Engine, name string, labels map[string]string) error {
	argv := []string{e.Name(), "volume", "create"}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		argv = append(argv, "--label", k+"="+labels[k])
	}
	argv = append(argv, name)
	if _, err := engine.Capture(ctx, e, argv); err != nil {
		// Docker's `volume create` is idempotent; Podman's fails when the
		// volume already exists. An existing volume is success either way.
		if volumeExists(ctx, e, name) {
			return nil
		}
		return err
	}
	return nil
}

func volumeExists(ctx context.Context, e engine.Engine, name string) bool {
	_, err := engine.Capture(ctx, e, []string{e.Name(), "volume", "inspect", name})
	return err == nil
}

func imageExists(ctx context.Context, e engine.Engine, image string) bool {
	_, err := engine.Capture(ctx, e, []string{e.Name(), "image", "inspect", "--format", "{{.Id}}", image})
	return err == nil
}

func resolveEngineFor(opts Options, host Host) (engine.Engine, error) {
	if opts.EngineInstance != nil {
		return opts.EngineInstance, nil
	}
	return ResolveEngine(opts.Engine, host)
}

func renderError(err error) string {
	if we, ok := err.(*workspace.Error); ok {
		return we.Render()
	}
	return "agentbox: " + err.Error() + "\n"
}

func fail(host Host, msg, remedy string) int {
	if remedy == "" {
		fmt.Fprintf(host.Err, "agentbox: %s\n", msg)
	} else {
		fmt.Fprintf(host.Err, "agentbox: %s\n  \u2192 %s\n", msg, remedy)
	}
	return errorExit
}

func usage(host Host, msg, remedy string) int {
	fmt.Fprintf(host.Err, "agentbox: %s\n  \u2192 %s\n", msg, remedy)
	return usageExit
}

// filepath is used by main wiring helpers; keep the import honest.
var _ = filepath.Join
