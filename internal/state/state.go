// Package state owns per-project state volume naming (FR-5) and the /state
// mount spec, plus the ls/rm/gc helpers used by `agentbox state`.
package state

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agentbox/internal/engine"
	"agentbox/internal/workspace"
)

const (
	// ContainerPath is where state is mounted in every adapter.
	ContainerPath = "/state"
	// DefaultHome is the default container HOME (FR-9).
	DefaultHome = "/state/home"
	// LabelUID/Project/Path carry the volume metadata state ls/gc need.
	labelKey = "agentbox"
)

// VolumeName is agentbox-state-<uid>-<hash8> (FR-5).
func VolumeName(uid int, canonical string) string {
	return fmt.Sprintf("agentbox-state-%d-%s", uid, workspace.Hash8(canonical))
}

// VolumeMount is the named-volume mount spec for a project.
func VolumeMount(uid int, canonical, relabel string) *engine.Mount {
	return &engine.Mount{
		Host:      VolumeName(uid, canonical),
		Container: ContainerPath,
		Relabel:   relabel,
	}
}

// DirMount is the --state-dir bind-mount variant (FR-5 P1).
func DirMount(hostPath, relabel string) *engine.Mount {
	return &engine.Mount{Host: hostPath, Container: ContainerPath, Relabel: relabel}
}

// ConcurrentWarning is printed when a deterministic volume may be shared.
func ConcurrentWarning(project string) string {
	return fmt.Sprintf(
		"note: state volume for %s may be in use by a concurrent run — harness session/auth files may race; use --ephemeral or distinct --state-dir paths for parallel runs",
		project)
}

// VolumeLabels are the labels applied when the volume is created.
func VolumeLabels(uid int, project, canonical string) map[string]string {
	return map[string]string{
		labelKey:           "1",
		"agentbox.uid":     strconv.Itoa(uid),
		"agentbox.project": project,
		"agentbox.path":    canonical,
	}
}

// Volume is one state volume as reported by the engine.
type Volume struct {
	Name      string
	Project   string
	Path      string
	SizeBytes int64
}

// ParseVolumeList parses tab-separated `volume ls` output:
// name, uid, project, path, size (size optional).
func ParseVolumeList(out string) []Volume {
	var vols []Volume
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "agentbox-state-") {
			continue
		}
		v := Volume{Name: fields[0], Project: fields[2], Path: fields[3]}
		if len(fields) > 4 {
			if n, err := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64); err == nil {
				v.SizeBytes = n
			}
		}
		vols = append(vols, v)
	}
	return vols
}

// List returns the state volumes owned by uid.
func List(ctx context.Context, e engine.Engine, uid int) ([]Volume, error) {
	argv := []string{
		e.Name(), "volume", "ls",
		"--filter", fmt.Sprintf("label=agentbox.uid=%d", uid),
		"--format", `{{.Name}}` + "\t" + `{{.Label "agentbox.uid"}}` + "\t" +
			`{{.Label "agentbox.project"}}` + "\t" + `{{.Label "agentbox.path"}}`,
	}
	out, err := engine.Capture(ctx, e, argv)
	if err != nil {
		return nil, err
	}
	return ParseVolumeList(out), nil
}

// Remove deletes one state volume.
func Remove(ctx context.Context, e engine.Engine, name string) error {
	_, err := engine.Capture(ctx, e, []string{e.Name(), "volume", "rm", name})
	return err
}

// Orphans returns state volumes whose recorded workspace path no longer
// exists, without removing anything. Callers confirm before removing.
func Orphans(ctx context.Context, e engine.Engine, uid int, pathExists func(string) bool) ([]Volume, error) {
	vols, err := List(ctx, e, uid)
	if err != nil {
		return nil, err
	}
	var orphans []Volume
	for _, v := range vols {
		if v.Path == "" || pathExists(v.Path) {
			continue
		}
		orphans = append(orphans, v)
	}
	return orphans, nil
}

// GC removes state volumes whose recorded workspace path no longer exists.
// It is the programmatic path (no confirmation); the CLI lists first via
// Orphans and asks before calling this.
func GC(ctx context.Context, e engine.Engine, uid int, pathExists func(string) bool) ([]string, error) {
	orphans, err := Orphans(ctx, e, uid, pathExists)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, v := range orphans {
		if err := Remove(ctx, e, v.Name); err != nil {
			return removed, err
		}
		removed = append(removed, v.Name)
	}
	return removed, nil
}
