package cli

import (
	"context"
	"fmt"

	"agentbox/internal/adapter"
	"agentbox/internal/doctor"
	"agentbox/internal/engine"
)

// Doctor implements `agentbox doctor` (FR-11 P1): engine, image, security
// posture, and the optional labeled update check (NFR-4).
func Doctor(o *Options, io IO, version string, e engine.Engine) int {
	agentName := o.Agent
	if agentName == "" {
		agentName = "pi"
	}
	ad, err := adapter.Get(agentName, version)
	if err != nil {
		fmt.Fprintf(io.Err, "agentbox: %v\n", err)
		return 2
	}
	r := doctor.Run(context.Background(), e, ad, doctor.Options{
		Version: version,
		NoCheck: o.NoCheck,
	})
	fmt.Fprint(io.Out, r.Render())
	if !r.EngineOK {
		return 1
	}
	return 0
}
