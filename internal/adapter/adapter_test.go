package adapter_test

import (
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
)

func TestBuiltinNames(t *testing.T) {
	want := []string{"claude", "opencode", "pi", "shell"}
	got := adapter.Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names() = %v, want %v", got, want)
		}
	}
}

func TestGetBuiltins(t *testing.T) {
	for _, name := range []string{"pi", "shell", "claude", "opencode"} {
		a, err := adapter.Get(name, "0.1.0")
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if a.Name != name {
			t.Errorf("Get(%q).Name = %q", name, a.Name)
		}
		if !a.UseEntrypoint {
			t.Errorf("%s must use the agentbox entrypoint", name)
		}
		if a.Image == "" || !strings.Contains(a.Image, "agentbox-") {
			t.Errorf("%s image = %q", name, a.Image)
		}
	}
}

func TestGetUnknownListsValidNames(t *testing.T) {
	_, err := adapter.Get("bogus", "0.1.0")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, name := range []string{"pi", "shell", "claude", "opencode"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q must list %q", err, name)
		}
	}
}

func TestPiDescriptor(t *testing.T) {
	a, _ := adapter.Get("pi", "0.1.0")
	if a.Image != "agentbox-pi:0.1.0-pi1.1.0" {
		t.Errorf("pi image = %q", a.Image)
	}
	if a.Home != "" {
		t.Errorf("pi Home = %q, want default /state/home", a.Home)
	}
	if len(a.EphemeralArgs) != 1 || a.EphemeralArgs[0] != "--no-session" {
		t.Errorf("pi EphemeralArgs = %v, want [--no-session]", a.EphemeralArgs)
	}
	if len(a.DefaultArgs) != 1 || a.DefaultArgs[0] != "pi" {
		t.Errorf("pi DefaultArgs = %v, want [pi]", a.DefaultArgs)
	}
	if a.CredFile != "/state/pi/auth.json" {
		t.Errorf("pi CredFile = %q", a.CredFile)
	}
	envs := map[string]string{}
	for _, e := range a.Env {
		envs[e.Name] = e.Value
	}
	if envs["PI_CODING_AGENT_DIR"] != "/state/pi" {
		t.Errorf("pi Env = %+v, want PI_CODING_AGENT_DIR=/state/pi", a.Env)
	}
	// jiti (pi's extension loader) caches transpiled packages under $TMPDIR;
	// without a persistent TMPDIR every start re-transpiles all configured
	// packages (several seconds).
	if envs["TMPDIR"] != "/state/home/.cache/tmp" {
		t.Errorf("pi TMPDIR = %q, want the persistent state cache dir", envs["TMPDIR"])
	}
	if envs["JITI_RESPECT_TMPDIR_ENV"] != "1" {
		t.Errorf("pi JITI_RESPECT_TMPDIR_ENV = %q, want 1", envs["JITI_RESPECT_TMPDIR_ENV"])
	}
	if a.WarmupCache != "/state/home/.cache/tmp" {
		t.Errorf("pi WarmupCache = %q, want the persistent cache dir", a.WarmupCache)
	}
	for _, name := range []string{"shell", "claude", "opencode"} {
		if other, _ := adapter.Get(name, "0.1.0"); other.WarmupCache != "" {
			t.Errorf("%s WarmupCache = %q, want none", name, other.WarmupCache)
		}
	}
}

func TestClaudeAndOpencodeDescriptors(t *testing.T) {
	c, _ := adapter.Get("claude", "0.1.0")
	if c.Image != "agentbox-claude:0.1.0-claude2.1.296" {
		t.Errorf("claude image = %q", c.Image)
	}
	if c.CredFile != "/state/claude/.credentials.json" {
		t.Errorf("claude CredFile = %q", c.CredFile)
	}
	found := false
	for _, e := range c.Env {
		if e.Name == "CLAUDE_CONFIG_DIR" && e.Value == "/state/claude" {
			found = true
		}
	}
	if !found {
		t.Errorf("claude Env = %+v", c.Env)
	}

	o, _ := adapter.Get("opencode", "0.1.0")
	if o.Image != "agentbox-opencode:0.1.0-opencode2.0.26" {
		t.Errorf("opencode image = %q", o.Image)
	}
	envs := map[string]string{}
	for _, e := range o.Env {
		envs[e.Name] = e.Value
	}
	if envs["XDG_CONFIG_HOME"] != "/state/opencode/config" || envs["XDG_DATA_HOME"] != "/state/opencode/data" {
		t.Errorf("opencode Env = %+v", o.Env)
	}
	// Bun needs an exec-capable TMPDIR (the container /tmp is noexec).
	if envs["TMPDIR"] != "/state/home/.cache/tmp" {
		t.Errorf("opencode TMPDIR = %q, want the state cache dir", envs["TMPDIR"])
	}
	// The background service costs ~5s per warm start and dies with the
	// container anyway; a private server is the right mode here. The flag must
	// come last (opencode rejects it before a subcommand).
	if len(o.DefaultArgs) != 1 || o.DefaultArgs[0] != "opencode" {
		t.Errorf("opencode DefaultArgs = %v, want [opencode]", o.DefaultArgs)
	}
	if len(o.TrailerArgs) != 1 || o.TrailerArgs[0] != "--standalone" {
		t.Errorf("opencode TrailerArgs = %v, want [--standalone]", o.TrailerArgs)
	}
	// service.json carries a generated password: never copied to the user folder.
	excluded := map[string]bool{}
	for _, name := range o.UserExclude {
		excluded[name] = true
	}
	if !excluded["service.json"] {
		t.Errorf("opencode UserExclude = %v, want service.json", o.UserExclude)
	}
	// --credentials target: a neutral path the user imports from.
	if o.CredFile != "/state/opencode/credentials.json" {
		t.Errorf("opencode CredFile = %q", o.CredFile)
	}
	// opencode stores credentials in its DB, so the entrypoint imports the
	// mounted file (subcommand only; path and trailer are appended).
	if len(o.CredImportArgs) != 2 || o.CredImportArgs[0] != "auth" || o.CredImportArgs[1] != "import" {
		t.Errorf("opencode CredImportArgs = %v, want [auth import]", o.CredImportArgs)
	}
	for name, want := range map[string]string{
		"XDG_CONFIG_HOME": "/state/opencode/config",
		"XDG_DATA_HOME":   "/state/opencode/data",
		"XDG_STATE_HOME":  "/state/opencode/state",
		"XDG_CACHE_HOME":  "/state/opencode/cache",
	} {
		if envs[name] != want {
			t.Errorf("opencode %s = %q, want %q", name, envs[name], want)
		}
	}
}

func TestShellDescriptor(t *testing.T) {
	a, _ := adapter.Get("shell", "0.1.0")
	if a.Image != "agentbox-base:0.1.0" {
		t.Errorf("shell image = %q", a.Image)
	}
	if len(a.DefaultArgs) != 1 || a.DefaultArgs[0] != "bash" {
		t.Errorf("shell DefaultArgs = %v", a.DefaultArgs)
	}
	if len(a.CredEnv) != 0 {
		t.Errorf("shell must not implicitly pass credentials: %v", a.CredEnv)
	}
}

func TestImageRefAdapter(t *testing.T) {
	a, err := adapter.Get("image:alpine:3.20", "0.1.0")
	if err != nil {
		t.Fatalf("image ref: %v", err)
	}
	if a.Image != "alpine:3.20" {
		t.Errorf("image = %q", a.Image)
	}
	if a.UseEntrypoint {
		t.Error("image:<ref> must run the image's own entrypoint (FR-4 P1)")
	}
	if len(a.DefaultArgs) != 0 {
		t.Errorf("image:<ref> DefaultArgs = %v, want none", a.DefaultArgs)
	}
	if len(a.Env) != 0 || len(a.CredEnv) != 0 {
		t.Errorf("image:<ref> must not inject env: %+v", a)
	}
}

// C2: BuildEnv never inherits the host wholesale.
func TestBuildEnvAllowlist(t *testing.T) {
	host := map[string]string{
		"ANTHROPIC_API_KEY":     "sk-secret",
		"HTTPS_PROXY":           "http://proxy:3128",
		"SSL_CERT_FILE":         "/etc/ssl/corp.pem",
		"AWS_SECRET_ACCESS_KEY": "poison",
		"RANDOM_VAR":            "poison",
	}
	getenv := func(k string) string { return host[k] }

	pi, _ := adapter.Get("pi", "0.1.0")
	envs, notices, warns := adapter.BuildEnv(pi, getenv, nil)

	names := map[string]engine.EnvVar{}
	for _, e := range envs {
		names[e.Name] = e
	}
	if _, ok := names["AWS_SECRET_ACCESS_KEY"]; ok {
		t.Error("non-allowlisted host var leaked into the container env")
	}
	if _, ok := names["RANDOM_VAR"]; ok {
		t.Error("host env inherited wholesale")
	}
	if e, ok := names["ANTHROPIC_API_KEY"]; !ok || !e.Secret || !e.Set || e.Value != "" {
		t.Errorf("ANTHROPIC_API_KEY must be bare -e secret: %+v", e)
	}
	if e, ok := names["HTTPS_PROXY"]; !ok || !e.Set {
		t.Errorf("HTTPS_PROXY missing: %+v", e)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %v, want none", notices)
	}
	if len(warns) == 0 || !strings.Contains(strings.Join(warns, " "), "SSL_CERT_FILE") {
		t.Errorf("path-valued var must warn: %v", warns)
	}
}

func TestBuildEnvUnsetAllowlistEntriesOmitted(t *testing.T) {
	pi, _ := adapter.Get("pi", "0.1.0")
	envs, _, _ := adapter.BuildEnv(pi, func(string) string { return "" }, nil)
	if len(envs) != 0 {
		t.Errorf("unset allowlist entries must be omitted, got %+v", envs)
	}
}

func TestBuildEnvExtraKeys(t *testing.T) {
	host := map[string]string{"GIT_AUTHOR_NAME": "Mark"}
	pi, _ := adapter.Get("pi", "0.1.0")
	envs, notices, _ := adapter.BuildEnv(pi, func(k string) string { return host[k] }, []string{"GIT_AUTHOR_NAME", "UNSET_KEY"})
	if len(envs) != 1 || envs[0].Name != "GIT_AUTHOR_NAME" || !envs[0].Secret {
		t.Errorf("envs = %+v", envs)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "UNSET_KEY") {
		t.Errorf("unset --env must produce a notice: %v", notices)
	}
}

func TestCredEnvAllowlistContent(t *testing.T) {
	pi, _ := adapter.Get("pi", "0.1.0")
	have := map[string]bool{}
	for _, k := range pi.CredEnv {
		have[k] = true
	}
	for _, want := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN",
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENROUTER_API_KEY",
		"GEMINI_API_KEY", "GOOGLE_API_KEY",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy",
		"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS",
	} {
		if !have[want] {
			t.Errorf("pi allowlist missing %q", want)
		}
	}
	claude, _ := adapter.Get("claude", "0.1.0")
	have = map[string]bool{}
	for _, k := range claude.CredEnv {
		have[k] = true
	}
	if !have["ANTHROPIC_API_KEY"] || !have["ANTHROPIC_OAUTH_TOKEN"] {
		t.Errorf("claude allowlist missing anthropic vars: %v", claude.CredEnv)
	}
}
