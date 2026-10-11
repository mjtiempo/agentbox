package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"agentbox/internal/engine"
	"agentbox/internal/state"
)

// State implements `agentbox state ls | rm <project> | gc` (FR-5 P1).
// Removal always requires confirmation: interactive y/N on a TTY, or
// --force in scripts.
func State(o *Options, io IO, e engine.Engine, uid int) int {
	ctx := context.Background()
	switch o.SubArgs[0] {
	case "ls":
		vols, err := state.List(ctx, e, uid)
		if err != nil {
			return stateEngineError(io, e)
		}
		if len(vols) == 0 {
			fmt.Fprintln(io.Out, "no agentbox state volumes")
			return 0
		}
		fmt.Fprintln(io.Out, "NAME\tPROJECT\tPATH\tSIZE")
		for _, v := range vols {
			size := "-"
			if v.SizeBytes > 0 {
				size = fmt.Sprintf("%d", v.SizeBytes)
			}
			fmt.Fprintf(io.Out, "%s\t%s\t%s\t%s\n", v.Name, v.Project, v.Path, size)
		}
		return 0
	case "rm":
		return stateRemove(ctx, o, io, e, uid, o.SubArgs[1])
	case "gc":
		return stateGC(ctx, o, io, e, uid)
	}
	return 2
}

func stateRemove(ctx context.Context, o *Options, io IO, e engine.Engine, uid int, target string) int {
	vols, err := state.List(ctx, e, uid)
	if err != nil {
		return stateEngineError(io, e)
	}
	var match *state.Volume
	for i := range vols {
		v := vols[i]
		if v.Name == target || v.Project == target || v.Path == target {
			match = &v
			break
		}
	}
	if match == nil {
		fmt.Fprintf(io.Err, "agentbox: no state volume matches %q\n  \u2192 run `agentbox state ls` to list projects\n", target)
		return 1
	}
	if !confirmRemoval(o, io, match.Name) {
		return 2
	}
	if err := state.Remove(ctx, e, match.Name); err != nil {
		fmt.Fprintf(io.Err, "agentbox: cannot remove state volume %s\n  \u2192 check the engine is running\n", match.Name)
		return 1
	}
	fmt.Fprintf(io.Out, "removed %s\n", match.Name)
	return 0
}

func stateGC(ctx context.Context, o *Options, io IO, e engine.Engine, uid int) int {
	orphans, err := state.Orphans(ctx, e, uid, pathExists)
	if err != nil {
		return stateEngineError(io, e)
	}
	if len(orphans) == 0 {
		fmt.Fprintln(io.Out, "nothing to collect")
		return 0
	}
	if !o.Force {
		if !io.IsTTY {
			fmt.Fprintf(io.Err, "agentbox: refusing to remove %d orphaned volume(s) without confirmation\n  \u2192 pass --force to confirm in scripts\n", len(orphans))
			for _, v := range orphans {
				fmt.Fprintf(io.Err, "  would remove: %s (%s)\n", v.Name, v.Path)
			}
			return 2
		}
		names := make([]string, 0, len(orphans))
		for _, v := range orphans {
			names = append(names, v.Name)
		}
		if !confirmRemoval(o, io, strings.Join(names, ", ")) {
			return 2
		}
	}
	for _, v := range orphans {
		if err := state.Remove(ctx, e, v.Name); err != nil {
			fmt.Fprintf(io.Err, "agentbox: cannot remove state volume %s\n  \u2192 check the engine is running\n", v.Name)
			return 1
		}
		fmt.Fprintf(io.Out, "removed %s\n", v.Name)
	}
	return 0
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func confirmRemoval(o *Options, io IO, what string) bool {
	if o.Force {
		return true
	}
	if !io.IsTTY {
		fmt.Fprintf(io.Err, "agentbox: refusing to remove %s without confirmation\n  \u2192 pass --force to confirm in scripts\n", what)
		return false
	}
	fmt.Fprintf(io.Err, "remove %s? [y/N] ", what)
	line, _ := bufio.NewReader(io.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	fmt.Fprintln(io.Err, "aborted")
	return false
}

func stateEngineError(io IO, e engine.Engine) int {
	fmt.Fprintf(io.Err, "agentbox: cannot reach the container engine (%s)\n  \u2192 is the %s daemon running? try: %s info\n", e.Name(), e.Name(), e.Name())
	return 1
}
