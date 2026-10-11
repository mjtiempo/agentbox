package run

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"agentbox/internal/adapter"
	"agentbox/internal/engine"
)

// DefaultUserRoot is the host directory holding the persistent per-harness
// user folders. Override with AGENTBOX_USER_DIR.
const DefaultUserRoot = ".agentbox"

// UserRoot returns the host agentbox user folder root, or "" when HOME is
// unavailable.
func UserRoot(host Host) string {
	if v := host.Env("AGENTBOX_USER_DIR"); v != "" {
		return v
	}
	home := host.Env("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, DefaultUserRoot)
}

// userConfigMounts prepares <root>/agents/<harness>/ and mounts every entry in
// it at the harness config path, so plugins, skills, themes, and settings
// survive across sessions and projects. The harness's own user-level dirs are
// pre-created so installs performed inside the container land in the
// persistent folder.
//
// Sessions, auth, and history are not part of this folder: they stay in the
// per-project state volume, so project A still cannot read project B's
// sessions (FR-5). Disable the whole feature with --no-user-config.
func userConfigMounts(ad adapter.Adapter, host Host, relabel string, disabled bool) ([]engine.Mount, string, error) {
	if disabled || ad.ConfigDir == "" {
		return nil, "", nil
	}
	root := UserRoot(host)
	if root == "" {
		return nil, fmt.Sprintf(
			"HOME is not set; skipping the persistent agentbox user folder (%s/agents/%s)",
			DefaultUserRoot, ad.Name), nil
	}
	dir := filepath.Join(root, "agents", ad.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("cannot create the agentbox user folder %s: %w", dir, err)
	}
	// When the user folder is a symlink to the harness's real host config
	// (`agentbox config link`), do not create directories inside the target:
	// the harness's own dirs are already there, and creating them would modify
	// the user's real config.
	if st, err := os.Lstat(dir); err == nil && st.Mode()&os.ModeSymlink == 0 {
		for _, sub := range ad.UserDirs {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				return nil, "", fmt.Errorf("cannot create the agentbox user folder %s: %w", filepath.Join(dir, sub), err)
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("cannot read the agentbox user folder %s: %w", dir, err)
	}
	excluded := map[string]bool{}
	for _, name := range ad.UserExclude {
		excluded[name] = true
	}
	var mounts []engine.Mount
	for _, e := range entries {
		if excluded[e.Name()] {
			continue
		}
		mounts = append(mounts, engine.Mount{
			Host:      filepath.Join(dir, e.Name()),
			Container: path.Join(ad.ConfigDir, e.Name()),
			Relabel:   relabel,
		})
	}
	return mounts, "", nil
}
