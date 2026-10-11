package cli

import (
	"fmt"

	"agentbox/internal/adapter"
	"agentbox/internal/auth"
	"agentbox/internal/authstore"
)

// AuthDeps wires `agentbox auth` to the host.
type AuthDeps struct {
	Probes   auth.Probes
	UserRoot string              // agentbox user folder root (~/.agentbox)
	Env      func(string) string // host environment
	Runner   authstore.Runner    // runs host commands (opencode auth export)
}

// Auth implements `agentbox auth <harness>` (detection), `agentbox auth copy
// <harness>` (adopt the host credential so runs use it automatically) and
// `agentbox auth reset <harness>` (remove the adopted credential). It never
// prints credential values.
func Auth(o *Options, io IO, version string, deps AuthDeps) int {
	if len(o.SubArgs) == 2 {
		return authStoreCommand(o, io, version, deps)
	}
	harness := o.SubArgs[0]
	ad, err := adapter.Get(harness, version)
	if err != nil {
		fmt.Fprintf(io.Err, "agentbox: %v\n", err)
		return 2
	}
	if len(ad.CredEnv) == 0 && ad.CredFile == "" {
		fmt.Fprintf(io.Out, "harness: %s\nmechanism: none (this harness takes no credentials from agentbox)\n", harness)
		return 0
	}
	res := auth.Detect(ad, deps.Probes)
	fmt.Fprintln(io.Out, res.Line(harness))
	return 0
}

func authStoreCommand(o *Options, io IO, version string, deps AuthDeps) int {
	action, harness := o.SubArgs[0], o.SubArgs[1]
	ad, err := adapter.Get(harness, version)
	if err != nil {
		fmt.Fprintf(io.Err, "agentbox: %v\n", err)
		return 2
	}
	if ad.CredFile == "" {
		fmt.Fprintf(io.Err, "agentbox: harness %q has no credential target to adopt\n  \u2192 use --env or a custom image\n", harness)
		return 2
	}
	switch action {
	case "copy":
		data, src, err := authstore.Adopt(ad, deps.Env, deps.Runner)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: %v\n  \u2192 log in on the host first, or pass --credentials <file> manually\n", err)
			return 1
		}
		path, err := authstore.Store(deps.UserRoot, ad.Name, data)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: %v\n", err)
			return 1
		}
		fmt.Fprintf(io.Out, "copied %s credentials from %s\n", ad.Name, src.Ref)
		fmt.Fprintf(io.Out, "stored at %s (0600); %s runs now use them automatically\n", path, ad.Name)
		fmt.Fprintf(io.Out, "remove with: agentbox auth reset %s\n", ad.Name)
		return 0
	case "reset":
		if !o.Force && !confirmRemoval(o, io, "the stored "+ad.Name+" credential") {
			return 2
		}
		removed, err := authstore.Reset(deps.UserRoot, ad.Name)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: %v\n", err)
			return 1
		}
		if removed {
			fmt.Fprintf(io.Out, "removed stored %s credentials\n", ad.Name)
		} else {
			fmt.Fprintf(io.Out, "no stored %s credentials\n", ad.Name)
		}
		return 0
	}
	return 2
}
