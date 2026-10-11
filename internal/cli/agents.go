package cli

import (
	"fmt"
	"sort"
	"strings"

	"agentbox/internal/adapter"
)

// Agents implements `agentbox agents` (FR-4 P1): lists adapters with image,
// pinned harness version, and the credential env names they accept.
func Agents(_ *Options, io IO, version string) int {
	builtins := adapter.Builtins(version)
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(io.Out, "NAME\tIMAGE\tHARNESS\tCREDENTIAL ENV")
	for _, name := range names {
		a := builtins[name]
		harness := harnessVersion(name)
		creds := "-"
		if len(a.CredEnv) > 0 {
			creds = strings.Join(a.CredEnv, ",")
		}
		fmt.Fprintf(io.Out, "%s\t%s\t%s\t%s\n", a.Name, a.Image, harness, creds)
	}
	fmt.Fprintln(io.Out, "image:<ref>\t<your image>\t-\t-")
	return 0
}

func harnessVersion(name string) string {
	switch name {
	case "pi":
		return adapter.PiVersion
	case "claude":
		return adapter.ClaudeVersion
	case "opencode":
		return adapter.OpencodeVersion
	default:
		return "-"
	}
}
