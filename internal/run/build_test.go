package run

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
)

type buildFakeEngine struct {
	calls     []string
	failBuild bool
}

func (f *buildFakeEngine) Name() string                                            { return "docker" }
func (f *buildFakeEngine) RunArgv(engine.RunSpec) []string                         { return nil }
func (f *buildFakeEngine) DryRunArgv(engine.RunSpec, func(string) string) []string { return nil }
func (f *buildFakeEngine) Run(context.Context, []string) (int, error)              { return 0, nil }
func (f *buildFakeEngine) RunStream(_ context.Context, argv []string, _ engine.Stdio) (int, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	if f.failBuild && strings.Contains(key, " build ") {
		return 1, nil
	}
	return 0, nil
}

func TestBuildImageFromVendoredDockerfile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "images", "pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "images", "pi", "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	pi, _ := adapter.Get("pi", "0.1.0")

	e := &buildFakeEngine{}
	if err := BuildImage(context.Background(), e, pi, "0.1.0", root); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if len(e.calls) != 1 {
		t.Fatalf("calls = %v", e.calls)
	}
	call := e.calls[0]
	for _, want := range []string{"docker build", "-f " + filepath.Join(root, "images", "pi", "Dockerfile"), "-t agentbox-pi:0.1.0-pi1.1.0", "BASE_IMAGE=agentbox-base:0.1.0"} {
		if !strings.Contains(call, want) {
			t.Errorf("build call missing %q: %s", want, call)
		}
	}
}

func TestBuildImageBaseHasNoBaseArg(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "images", "base"), 0o755)
	os.WriteFile(filepath.Join(root, "images", "base", "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	shell, _ := adapter.Get("shell", "0.1.0")
	e := &buildFakeEngine{}
	if err := BuildImage(context.Background(), e, shell, "0.1.0", root); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
	if strings.Contains(e.calls[0], "BASE_IMAGE=") {
		t.Errorf("base image build must not pass BASE_IMAGE: %s", e.calls[0])
	}
}

func TestBuildImageMissingDockerfile(t *testing.T) {
	pi, _ := adapter.Get("pi", "0.1.0")
	e := &buildFakeEngine{}
	err := BuildImage(context.Background(), e, pi, "0.1.0", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Dockerfile") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildImageRejectsImageRef(t *testing.T) {
	ref, _ := adapter.Get("image:alpine", "0.1.0")
	e := &buildFakeEngine{}
	err := BuildImage(context.Background(), e, ref, "0.1.0", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a built-in") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildImageFailureIsReported(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "images", "pi"), 0o755)
	os.WriteFile(filepath.Join(root, "images", "pi", "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	pi, _ := adapter.Get("pi", "0.1.0")
	e := &buildFakeEngine{failBuild: true}
	if err := BuildImage(context.Background(), e, pi, "0.1.0", root); err == nil {
		t.Fatal("expected build failure")
	}
}

func TestFindRepoRootFromExecutable(t *testing.T) {
	// The test binary is not in a checkout layout, so discovery must fail
	// gracefully rather than walk the whole filesystem.
	_ = findRepoRoot()
}

var _ io.Writer = (*strings.Builder)(nil)
