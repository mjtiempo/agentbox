package cli

import (
	"fmt"

	"agentbox/internal/adapter"
	"agentbox/internal/run"
	"agentbox/internal/userconfig"
)

// Config implements `agentbox config link|copy|reset [harness]` (FR-4/user
// folder): it connects ~/.agentbox/agents/<harness> to the harness's existing
// host config directory so plugins and skills persist without manual copying.
func Config(o *Options, io IO, version string, getenv func(string) string) int {
	action := o.SubArgs[0]
	harness := "pi"
	if len(o.SubArgs) > 1 {
		harness = o.SubArgs[1]
	}
	ad, err := adapter.Get(harness, version)
	if err != nil {
		fmt.Fprintf(io.Err, "agentbox: %v\n", err)
		return 2
	}
	if ad.ConfigDir == "" {
		fmt.Fprintf(io.Err, "agentbox: harness %q has no user config folder to link or copy\n", harness)
		return 2
	}
	root := run.UserRoot(run.Host{Env: getenv})
	if root == "" {
		fmt.Fprintf(io.Err, "agentbox: cannot locate the agentbox user folder (HOME is unset)\n  \u2192 set HOME or AGENTBOX_USER_DIR\n")
		return 1
	}
	target := userconfig.TargetDir(root, ad)

	switch action {
	case "link", "copy":
		source, err := userconfig.SourceDir(ad, o.From, getenv)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: %v\n", err)
			return 1
		}
		var res userconfig.Result
		if action == "link" {
			res, err = userconfig.Link(target, source, o.Force)
		} else {
			res, err = userconfig.Copy(target, source, ad, o.Force)
		}
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: cannot %s %s\n  \u2192 %v (pass --force to replace, or --from <path> for a different source)\n", action, target, err)
			return 1
		}
		report(io, action, res)
		return 0
	case "reset":
		if !o.Force && !confirmRemoval(o, io, target) {
			return 2
		}
		res, err := userconfig.Reset(target)
		if err != nil {
			fmt.Fprintf(io.Err, "agentbox: cannot reset %s\n  \u2192 %v\n", target, err)
			return 1
		}
		report(io, action, res)
		return 0
	}
	return 2
}

func report(io IO, action string, res userconfig.Result) {
	switch res.Action {
	case "unchanged":
		fmt.Fprintf(io.Out, "%s: nothing to do (%s)\n", action, res.Target)
	case "linked":
		fmt.Fprintf(io.Out, "linked %s -> %s\n", res.Target, res.Source)
	case "copied":
		fmt.Fprintf(io.Out, "copied %d entries from %s to %s\n", len(res.Entries), res.Source, res.Target)
		for _, name := range res.Entries {
			fmt.Fprintf(io.Out, "  + %s\n", name)
		}
	case "reset":
		if res.Source != "" {
			fmt.Fprintf(io.Out, "removed link %s -> %s\n", res.Target, res.Source)
		} else {
			fmt.Fprintf(io.Out, "removed %s\n", res.Target)
		}
	}
	if len(res.Skipped) > 0 {
		fmt.Fprintf(io.Out, "  skipped (sessions/auth/history or already present): %v\n", res.Skipped)
	}
}
