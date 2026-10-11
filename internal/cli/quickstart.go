package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentbox/internal/adapter"
	"agentbox/internal/authstore"
	"agentbox/internal/userconfig"
)

// QuickstartDeps wires `agentbox quickstart` to the host and to a container
// run (used for the interactive login flow).
type QuickstartDeps struct {
	Env      func(string) string
	UserRoot string
	Runner   authstore.Runner
	IsTTY    bool
	// RunHarness runs the harness in a container with the given agent args and
	// a scratch --state-dir. Used for the interactive login and for the
	// in-container credential export.
	RunHarness func(harness string, agentArgs []string, stateDir string, io IO) int
	// TempDir creates the scratch state directory for the login.
	TempDir func() (string, error)
}

// Quickstart implements `agentbox quickstart [harness]`: adopt the host config
// and credentials, and if none are found offer to run the harness's own
// authentication flow, adopting whatever credential it produces.
func Quickstart(o *Options, io IO, version string, deps QuickstartDeps) int {
	harness := "pi"
	if len(o.SubArgs) > 0 {
		harness = o.SubArgs[0]
	}
	ad, err := adapter.Get(harness, version)
	if err != nil {
		fmt.Fprintf(io.Err, "agentbox: %v\n", err)
		return 2
	}
	if ad.ConfigDir == "" && ad.CredFile == "" {
		fmt.Fprintf(io.Out, "%s needs no setup (no config directory, no credentials)\n", ad.Name)
		return 0
	}

	// --- 1. host config -----------------------------------------------------
	if ad.ConfigDir != "" {
		source, err := userconfig.SourceDir(ad, o.From, deps.Env)
		if err != nil {
			fmt.Fprintf(io.Out, "config: skipped (%v)\n", err)
		} else if st, statErr := os.Stat(source); statErr == nil && st.IsDir() {
			target := userconfig.TargetDir(deps.UserRoot, ad)
			if tst, lerr := os.Lstat(target); lerr == nil && tst.Mode()&os.ModeSymlink != 0 {
				// `agentbox config link` already points the user folder at the
				// host config; copying would write into the host directory.
				linked, _ := os.Readlink(target)
				fmt.Fprintf(io.Out, "config: %s is linked to %s (nothing to copy; `agentbox config reset %s` to copy instead)\n",
					target, linked, ad.Name)
			} else {
				res, err := userconfig.Copy(target, source, ad, o.Force)
				if err != nil {
					fmt.Fprintf(io.Err, "agentbox: cannot copy host config from %s\n  \u2192 %v\n", source, err)
					return 1
				}
				fmt.Fprintf(io.Out, "config: copied %d entries from %s\n", len(res.Entries), source)
				if len(res.Skipped) > 0 {
					fmt.Fprintf(io.Out, "config: skipped %v (already present; --force replaces)\n", res.Skipped)
				}
			}
		} else {
			fmt.Fprintf(io.Out, "config: no host config found at %s (skipping)\n", source)
		}
	}

	// --- 2. host credentials ------------------------------------------------
	if ad.CredFile == "" {
		fmt.Fprintf(io.Out, "auth: %s takes no credentials; setup complete\n", ad.Name)
		return 0
	}
	data, src, adoptErr := authstore.Adopt(ad, deps.Env, deps.Runner)
	if adoptErr == nil {
		path, err := authstore.Store(deps.UserRoot, ad.Name, data)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: %v\n", err)
			return 1
		}
		fmt.Fprintf(io.Out, "auth: adopted credentials from %s\n", src.Ref)
		fmt.Fprintf(io.Out, "setup complete; credentials stored at %s\n", path)
		fmt.Fprintf(io.Out, "run `agentbox --agent %s` to start\n", ad.Name)
		return 0
	}

	// --- 3. no host credentials: offer the harness's own login flow ---------
	return quickstartLogin(o, io, ad, deps, adoptErr)
}

func quickstartLogin(o *Options, io IO, ad adapter.Adapter, deps QuickstartDeps, adoptErr error) int {
	if !deps.IsTTY {
		fmt.Fprintf(io.Out, "auth: no provider/auth setup found for %s (%v)\n", ad.Name, adoptErr)
		fmt.Fprintf(io.Out, "  \u2192 re-run `agentbox quickstart %s` in a terminal to authenticate, or log in on the host and run `agentbox auth copy %s`\n",
			ad.Name, ad.Name)
		return 0
	}
	fmt.Fprintf(io.Out, "auth: no provider/auth setup found for %s\n", ad.Name)
	fmt.Fprint(io.Out, "Authenticate now? [Y/n] ")
	answer, _ := bufio.NewReader(io.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
	default:
		fmt.Fprintf(io.Out, "auth: skipped; run `agentbox auth copy %s` later, or log in inside the container\n", ad.Name)
		return 0
	}

	stateDir, err := deps.TempDir()
	if err != nil {
		fmt.Fprintf(io.Err, "agentbox: cannot create a scratch state directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(stateDir)

	if len(ad.LoginArgs) == 0 && ad.LoginHint != "" {
		fmt.Fprintf(io.Err, "agentbox: starting %s \u2014 run %s inside to authenticate, then exit\n", ad.Name, ad.LoginHint)
	}
	if code := deps.RunHarness(ad.Name, ad.LoginArgs, stateDir, io); code != 0 {
		fmt.Fprintf(io.Err, "agentbox: %s exited with code %d before authentication completed\n", ad.Name, code)
		return 1
	}

	// Adopt whatever the login produced: the credential file for file-based
	// harnesses, or the harness's export command for database-backed ones.
	if path, data, ok := credentialFromLoginState(ad, stateDir); ok {
		stored, err := authstore.Store(deps.UserRoot, ad.Name, data)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: %v\n", err)
			return 1
		}
		fmt.Fprintf(io.Out, "auth: adopted the credential created at %s\n", path)
		fmt.Fprintf(io.Out, "setup complete; credentials stored at %s\n", stored)
		fmt.Fprintf(io.Out, "run `agentbox --agent %s` to start\n", ad.Name)
		return 0
	}
	if len(ad.CredExportArgs) > 0 {
		var buf bytes.Buffer
		capture := io
		capture.Out = &buf
		if deps.RunHarness(ad.Name, ad.CredExportArgs, stateDir, capture) == 0 && buf.Len() > 0 {
			stored, err := authstore.Store(deps.UserRoot, ad.Name, buf.Bytes())
			if err != nil {
				fmt.Fprintf(io.Err, "agentbox: %v\n", err)
				return 1
			}
			fmt.Fprintf(io.Out, "auth: exported and adopted the credential created by the login\n")
			fmt.Fprintf(io.Out, "setup complete; credentials stored at %s\n", stored)
			fmt.Fprintf(io.Out, "run `agentbox --agent %s` to start\n", ad.Name)
			return 0
		}
	}
	fmt.Fprintf(io.Out, "auth: login finished, but no credential could be adopted automatically\n")
	fmt.Fprintf(io.Out, "  \u2192 it is stored in this project's state; use `agentbox auth copy %s` after a host login to reuse it everywhere\n", ad.Name)
	return 0
}

// credentialFromLoginState maps the adapter's container credential path to the
// scratch --state-dir and reads it back.
func credentialFromLoginState(ad adapter.Adapter, stateDir string) (string, []byte, bool) {
	if ad.CredFile == "" {
		return "", nil, false
	}
	rel := strings.TrimPrefix(ad.CredFile, "/state/")
	if rel == ad.CredFile {
		return "", nil, false
	}
	path := filepath.Join(stateDir, rel)
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "", nil, false
	}
	return path, data, true
}
