package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
)

// BuildImage builds an adapter image locally from the vendored Dockerfile
// (FR-10 P2). It requires a source checkout: the Dockerfiles live in
// images/<harness>/Dockerfile next to the repository root.
func BuildImage(ctx context.Context, e engine.Engine, ad adapter.Adapter, agentboxVersion, repoRoot string) error {
	dir, ok := dockerfileDir(ad.Name)
	if !ok {
		return fmt.Errorf("--build cannot build %q: it is not a built-in adapter image", ad.Name)
	}
	dockerfile := filepath.Join(repoRoot, "images", dir, "Dockerfile")
	if _, err := os.Stat(dockerfile); err != nil {
		return fmt.Errorf("cannot find the vendored Dockerfile at %s", dockerfile)
	}
	argv := []string{e.Name(), "build", "-f", dockerfile, "-t", ad.Image}
	if dir != "base" {
		argv = append(argv, "--build-arg", "BASE_IMAGE=agentbox-base:"+agentboxVersion)
	}
	argv = append(argv, repoRoot)
	if _, err := engine.Capture(ctx, e, argv); err != nil {
		return fmt.Errorf("image build failed for %s: %w", ad.Image, err)
	}
	return nil
}

// dockerfileDir maps an adapter to its images/ subdirectory.
func dockerfileDir(adapterName string) (string, bool) {
	switch adapterName {
	case "pi":
		return "pi", true
	case "claude":
		return "claude", true
	case "opencode":
		return "opencode", true
	case "shell":
		return "base", true
	}
	return "", false
}

// findRepoRoot walks up from the executable looking for the checkout layout
// (go.mod + images/). It returns "" when the binary was installed without the
// source tree.
func findRepoRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	for i := 0; i < 6; i++ {
		if fileExists(filepath.Join(dir, "go.mod")) && dirExists(filepath.Join(dir, "images")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// isImageRef reports whether the adapter name is the image:<ref> form.
func isImageRef(name string) bool { return strings.HasPrefix(name, "image:") }
