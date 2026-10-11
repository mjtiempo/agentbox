// Package userconfig implements `agentbox config link|copy|reset`: it connects
// the persistent agentbox user folder (~/.agentbox/agents/<harness>) to the
// harness's existing host config directory, so plugins and skills survive
// across sessions and projects without hand-copying subdirectories.
package userconfig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"agentbox/internal/adapter"
)

// Env is the host environment lookup.
type Env func(string) string

// TargetDir is the agentbox user folder for a harness.
func TargetDir(userRoot string, ad adapter.Adapter) string {
	return filepath.Join(userRoot, "agents", ad.Name)
}

// SourceDir resolves the harness's host config directory: an explicit --from
// path wins, then $XDG_CONFIG_HOME/<HostConfigXDG> (~/.config fallback), then
// $HOME/<HostConfigDir>.
func SourceDir(ad adapter.Adapter, from string, getenv Env) (string, error) {
	if from != "" {
		abs, err := filepath.Abs(from)
		if err != nil {
			return "", fmt.Errorf("cannot resolve --from %q", from)
		}
		return abs, nil
	}
	home := getenv("HOME")
	if ad.HostConfigXDG != "" {
		base := getenv("XDG_CONFIG_HOME")
		if base == "" {
			if home == "" {
				return "", fmt.Errorf("cannot locate the %s config: HOME and XDG_CONFIG_HOME are unset", ad.Name)
			}
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, ad.HostConfigXDG), nil
	}
	if ad.HostConfigDir == "" {
		return "", fmt.Errorf("harness %q has no host config directory", ad.Name)
	}
	if home == "" {
		return "", fmt.Errorf("cannot locate the %s config: HOME is unset", ad.Name)
	}
	return filepath.Join(home, ad.HostConfigDir), nil
}

// Result describes what an action did.
type Result struct {
	Action  string // linked | copied | reset | unchanged
	Target  string
	Source  string
	Entries []string // entries linked/copied/removed
	Skipped []string // entries deliberately not copied
}

// Link symlinks the whole host config directory at the agentbox user folder.
// An existing link to the same source is a no-op; anything else needs force.
func Link(target, source string, force bool) (Result, error) {
	res := Result{Action: "linked", Target: target, Source: source}
	st, err := os.Stat(source)
	if err != nil {
		return res, fmt.Errorf("no host config found at %s", source)
	}
	if !st.IsDir() {
		return res, fmt.Errorf("%s is not a directory", source)
	}
	if tst, err := os.Lstat(target); err == nil {
		switch {
		case tst.Mode()&os.ModeSymlink != 0:
			old, _ := os.Readlink(target)
			if old == source {
				res.Action = "unchanged"
				return res, nil
			}
			if !force {
				return res, fmt.Errorf("%s is already linked to %s", target, old)
			}
			if err := os.Remove(target); err != nil {
				return res, err
			}
		case tst.IsDir():
			empty, err := onlyEmptyDirs(target)
			if err != nil {
				return res, err
			}
			if !empty && !force {
				return res, fmt.Errorf("%s already contains files", target)
			}
			if err := os.RemoveAll(target); err != nil {
				return res, err
			}
		default:
			return res, fmt.Errorf("%s exists and is not a directory", target)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return res, err
	}
	if err := os.Symlink(source, target); err != nil {
		return res, err
	}
	res.Entries = []string{filepath.Base(source)}
	return res, nil
}

// Copy copies the host config directory's entries into the agentbox user
// folder, skipping the adapter's UserExclude names (sessions, auth, history)
// and any entry that already exists unless force is set.
func Copy(target, source string, ad adapter.Adapter, force bool) (Result, error) {
	res := Result{Action: "copied", Target: target, Source: source}
	st, err := os.Stat(source)
	if err != nil {
		return res, fmt.Errorf("no host config found at %s", source)
	}
	if !st.IsDir() {
		return res, fmt.Errorf("%s is not a directory", source)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return res, err
	}
	excluded := map[string]bool{}
	for _, name := range ad.UserExclude {
		excluded[name] = true
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		name := e.Name()
		if excluded[name] {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		copied, skipped, err := copyTree(filepath.Join(source, name), filepath.Join(target, name), force)
		if err != nil {
			return res, err
		}
		if copied {
			res.Entries = append(res.Entries, name)
		} else if skipped {
			res.Skipped = append(res.Skipped, name)
		}
	}
	sort.Strings(res.Entries)
	sort.Strings(res.Skipped)
	return res, nil
}

// Reset removes the agentbox user folder for a harness: the symlink (never its
// target) or the copied directory.
func Reset(target string) (Result, error) {
	res := Result{Action: "reset", Target: target}
	st, err := os.Lstat(target)
	if os.IsNotExist(err) {
		res.Action = "unchanged"
		return res, nil
	}
	if err != nil {
		return res, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		old, _ := os.Readlink(target)
		res.Source = old
		if err := os.Remove(target); err != nil {
			return res, err
		}
		return res, nil
	}
	if !st.IsDir() {
		return res, fmt.Errorf("%s is not a directory", target)
	}
	entries, _ := os.ReadDir(target)
	for _, e := range entries {
		res.Entries = append(res.Entries, e.Name())
	}
	if err := os.RemoveAll(target); err != nil {
		return res, err
	}
	return res, nil
}

// onlyEmptyDirs reports whether path contains only (recursively) empty dirs.
// The standard user dirs pre-created by a previous run are therefore
// replaceable by `config link` without force.
func onlyEmptyDirs(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			return false, nil
		}
		empty, err := onlyEmptyDirs(filepath.Join(path, e.Name()))
		if err != nil {
			return false, err
		}
		if !empty {
			return false, nil
		}
	}
	return true, nil
}

// copyTree copies src to dst. Directories merge into an existing destination
// (pre-created standard dirs are the common case); existing files are kept
// unless force is set. It reports whether anything was copied or skipped.
func copyTree(src, dst string, force bool) (copied, skipped bool, err error) {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return false, false, err
	}
	dstInfo, dstErr := os.Lstat(dst)
	dstExists := dstErr == nil

	switch {
	case srcInfo.Mode()&os.ModeSymlink != 0:
		if dstExists && !force {
			return false, true, nil
		}
		if dstExists {
			if err := os.RemoveAll(dst); err != nil {
				return false, false, err
			}
		}
		link, err := os.Readlink(src)
		if err != nil {
			return false, false, err
		}
		return true, false, os.Symlink(link, dst)

	case srcInfo.IsDir():
		if dstExists && !dstInfo.IsDir() {
			if !force {
				return false, true, nil
			}
			if err := os.RemoveAll(dst); err != nil {
				return false, false, err
			}
			dstExists = false
		}
		if !dstExists {
			if err := os.MkdirAll(dst, srcInfo.Mode().Perm()|0o700); err != nil {
				return false, false, err
			}
			copied = true
		}
		children, err := os.ReadDir(src)
		if err != nil {
			return copied, skipped, err
		}
		for _, child := range children {
			childCopied, childSkipped, err := copyTree(
				filepath.Join(src, child.Name()), filepath.Join(dst, child.Name()), force)
			if err != nil {
				return copied, skipped, err
			}
			copied = copied || childCopied
			skipped = skipped || childSkipped
		}
		return copied, skipped, nil

	case srcInfo.Mode().IsRegular():
		if dstExists && !force {
			return false, true, nil
		}
		if err := copyFile(src, dst, srcInfo.Mode().Perm()); err != nil {
			return false, false, err
		}
		return true, false, nil

	default:
		// Sockets, devices, fifos: skip.
		return false, true, nil
	}
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
