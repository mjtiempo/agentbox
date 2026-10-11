// Package doctor reports engine, image, security posture, and update status
// (FR-11 P1, FR-10 P1, FR-9 P2). The update check is the only network call,
// it is labeled, and it is suppressible with --no-check (NFR-4).
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
)

// DefaultUpdateURL is the release endpoint queried unless overridden.
const DefaultUpdateURL = "https://api.github.com/repos/agentbox-dev/agentbox/releases/latest"

// Options controls the checks.
type Options struct {
	Version   string
	NoCheck   bool
	UpdateURL string
	Timeout   time.Duration
}

// Report is the doctor output.
type Report struct {
	Version       string
	EngineName    string
	EngineVersion string
	EngineOK      bool
	SecurityOpts  string
	Rootless      bool
	Image         string
	ImagePresent  bool
	ImageID       string
	Update        string
}

// Run performs the checks. Engine failures degrade to EngineOK=false rather
// than aborting, so doctor is useful on a machine with no engine.
func Run(ctx context.Context, e engine.Engine, ad adapter.Adapter, o Options) Report {
	r := Report{EngineName: e.Name(), Image: ad.Image, Version: o.Version}
	if o.Timeout == 0 {
		o.Timeout = 3 * time.Second
	}

	if out, err := engine.Capture(ctx, e, []string{e.Name(), "version", "--format", "{{.Server.Version}}"}); err == nil {
		r.EngineOK = true
		r.EngineVersion = strings.TrimSpace(out)
	}
	if out, err := engine.Capture(ctx, e, []string{e.Name(), "info", "--format", "{{json .SecurityOptions}}"}); err == nil {
		r.SecurityOpts = strings.TrimSpace(out)
		r.Rootless = strings.Contains(strings.ToLower(r.SecurityOpts), "rootless")
	}
	if out, err := engine.Capture(ctx, e, []string{e.Name(), "image", "inspect", "--format", "{{.Id}}", ad.Image}); err == nil {
		r.ImagePresent = true
		r.ImageID = strings.TrimSpace(out)
	}

	if o.NoCheck {
		r.Update = "skipped (--no-check)"
	} else {
		r.Update = checkUpdate(ctx, o)
	}
	return r
}

func checkUpdate(ctx context.Context, o Options) string {
	url := o.UpdateURL
	if url == "" {
		url = DefaultUpdateURL
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "unavailable (bad update URL)"
	}
	req.Header.Set("User-Agent", "agentbox/"+o.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "unavailable (offline or endpoint unreachable)"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("unavailable (HTTP %d; no published release yet)", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "unavailable (unreadable release metadata)"
	}
	if body.TagName == "" {
		return "unavailable (no tag in release metadata)"
	}
	if strings.TrimPrefix(body.TagName, "v") == strings.TrimPrefix(o.Version, "v") {
		return "up to date (" + body.TagName + ")"
	}
	return "newer release available: " + body.TagName + " (current " + o.Version + ")"
}

// Render formats the report.
func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agentbox: %s\n", r.Version)
	if r.EngineOK {
		mode := "rootful"
		if r.Rootless {
			mode = "rootless"
		}
		fmt.Fprintf(&b, "engine:   %s %s (%s)\n", r.EngineName, r.EngineVersion, mode)
	} else {
		fmt.Fprintf(&b, "engine:   %s unreachable\n  \u2192 is the %s daemon running? try: %s info\n", r.EngineName, r.EngineName, r.EngineName)
	}
	if r.SecurityOpts != "" {
		fmt.Fprintf(&b, "security: %s\n", r.SecurityOpts)
	}
	fmt.Fprintf(&b, "hardening defaults: --cap-drop ALL --cap-add CHOWN,SETUID,SETGID --security-opt no-new-privileges --tmpfs /tmp:size=256m --init\n")
	if r.ImagePresent {
		fmt.Fprintf(&b, "image:    %s (present, %s)\n", r.Image, r.ImageID)
	} else {
		fmt.Fprintf(&b, "image:    %s (not present; `make image` builds it locally)\n", r.Image)
	}
	fmt.Fprintf(&b, "update:   %s\n", r.Update)
	return b.String()
}
