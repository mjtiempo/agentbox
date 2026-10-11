package doctor_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/doctor"
	"agentbox/internal/engine"
)

type fakeEngine struct {
	responses map[string]string
}

func (f *fakeEngine) Name() string                                            { return "docker" }
func (f *fakeEngine) RunArgv(engine.RunSpec) []string                         { return nil }
func (f *fakeEngine) DryRunArgv(engine.RunSpec, func(string) string) []string { return nil }
func (f *fakeEngine) Run(context.Context, []string) (int, error)              { return 0, nil }

func (f *fakeEngine) RunStream(_ context.Context, argv []string, stdio engine.Stdio) (int, error) {
	out, ok := f.responses[strings.Join(argv, " ")]
	if !ok {
		return 1, nil
	}
	if w, ok := stdio.Out.(io.Writer); ok && w != nil {
		io.WriteString(w, out)
	}
	return 0, nil
}

func pi(t *testing.T) adapter.Adapter {
	t.Helper()
	a, err := adapter.Get("pi", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func healthyEngine() *fakeEngine {
	return &fakeEngine{responses: map[string]string{
		"docker version --format {{.Server.Version}}":                     "29.8.2\n",
		"docker info --format {{json .SecurityOptions}}":                  `["name=seccomp,profile=builtin","name=cgroupns"]` + "\n",
		"docker image inspect --format {{.Id}} agentbox-pi:0.1.0-pi1.1.0": "sha256:deadbeef\n",
	}}
}

func TestRunHealthy(t *testing.T) {
	r := doctor.Run(context.Background(), healthyEngine(), pi(t), doctor.Options{Version: "0.1.0", NoCheck: true})
	if !r.EngineOK || r.EngineVersion != "29.8.2" {
		t.Errorf("engine: %+v", r)
	}
	if r.Rootless {
		t.Error("seccomp/cgroupns must not be reported as rootless")
	}
	if !r.ImagePresent || r.ImageID != "sha256:deadbeef" {
		t.Errorf("image: %+v", r)
	}
	if r.Update != "skipped (--no-check)" {
		t.Errorf("update = %q", r.Update)
	}
}

func TestRunRootlessDetection(t *testing.T) {
	e := healthyEngine()
	e.responses["docker info --format {{json .SecurityOptions}}"] = `["name=rootless","name=seccomp"]` + "\n"
	r := doctor.Run(context.Background(), e, pi(t), doctor.Options{Version: "0.1.0", NoCheck: true})
	if !r.Rootless {
		t.Error("rootless must be detected")
	}
}

func TestRunEngineUnreachable(t *testing.T) {
	e := &fakeEngine{responses: map[string]string{}}
	r := doctor.Run(context.Background(), e, pi(t), doctor.Options{Version: "0.1.0", NoCheck: true})
	if r.EngineOK {
		t.Error("EngineOK must be false")
	}
	out := r.Render()
	if !strings.Contains(out, "unreachable") || !strings.Contains(out, "docker info") {
		t.Errorf("render = %q", out)
	}
}

func TestRenderContent(t *testing.T) {
	r := doctor.Run(context.Background(), healthyEngine(), pi(t), doctor.Options{Version: "0.1.0", NoCheck: true})
	out := r.Render()
	for _, want := range []string{
		"agentbox: 0.1.0",
		"engine:   docker 29.8.2 (rootful)",
		"security:",
		"hardening defaults:",
		"--cap-drop ALL",
		"image:    agentbox-pi:0.1.0-pi1.1.0 (present, sha256:deadbeef)",
		"update:   skipped (--no-check)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "agentbox/") {
			t.Errorf("update check must be labeled: UA=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"tag_name":"v0.2.0"}`)
	}))
	defer srv.Close()

	r := doctor.Run(context.Background(), healthyEngine(), pi(t), doctor.Options{
		Version: "0.1.0", UpdateURL: srv.URL,
	})
	if !strings.Contains(r.Update, "v0.2.0") || !strings.Contains(r.Update, "newer") {
		t.Errorf("update = %q", r.Update)
	}
}

func TestUpdateCheckUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	r := doctor.Run(context.Background(), healthyEngine(), pi(t), doctor.Options{
		Version: "0.1.0", UpdateURL: srv.URL,
	})
	if !strings.Contains(r.Update, "unavailable") {
		t.Errorf("update = %q", r.Update)
	}
}
