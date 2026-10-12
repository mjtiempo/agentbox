package run

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"agentbox/internal/engine"
)

// ContainerInfo is one container row from the sweep/concurrency query.
type ContainerInfo struct {
	ID      string
	Owner   string
	Project string
	State   string // running|exited|created|...
	Keep    string // agentbox.keep label; "1" marks an intentionally kept container
}

// ParseOwner parses the agentbox.owner label `<pid>@<boot-id>`.
func ParseOwner(s string) (pid int, bootID string, ok bool) {
	left, right, found := strings.Cut(s, "@")
	if !found {
		return 0, "", false
	}
	n, err := strconv.Atoi(left)
	if err != nil || n <= 0 || right == "" {
		return 0, "", false
	}
	return n, right, true
}

// OwnerAlive reports whether the owner pid is still running on this boot.
// A different boot id means the owner is stale; malformed owners are treated
// as stale so they can be reaped.
func OwnerAlive(owner, selfBootID string, procAlive func(int) bool) bool {
	pid, boot, ok := ParseOwner(owner)
	if !ok {
		return false
	}
	if boot != selfBootID {
		return false
	}
	return procAlive(pid)
}

// ParseContainerList parses tab-separated `ps` output. The first two fields
// are id and owner; project, state, and the keep label are optional so older
// two-field callers keep working.
func ParseContainerList(out string) []ContainerInfo {
	var infos []ContainerInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		info := ContainerInfo{ID: strings.TrimSpace(fields[0]), Owner: strings.TrimSpace(fields[1])}
		if len(fields) > 2 {
			info.Project = strings.TrimSpace(fields[2])
		}
		if len(fields) > 3 {
			info.State = strings.TrimSpace(fields[3])
		}
		if len(fields) > 4 {
			info.Keep = strings.TrimSpace(fields[4])
		}
		infos = append(infos, info)
	}
	return infos
}

// ListOwnContainers returns this uid's agentbox containers with owner, project,
// and state labels in a single engine call. Besides feeding the concurrent-run
// warning and the stale sweep, a successful call proves the engine is
// reachable, so no separate probe is needed (FR-3).
func ListOwnContainers(ctx context.Context, e engine.Engine, uid int) ([]ContainerInfo, error) {
	out, err := engine.Capture(ctx, e, []string{
		e.Name(), "ps", "-a",
		"--filter", fmt.Sprintf("label=agentbox.uid=%d", uid),
		"--format", `{{.ID}}` + "\t" + `{{.Label "agentbox.owner"}}` + "\t" +
			`{{.Label "agentbox.project"}}` + "\t" + `{{.State}}` + "\t" +
			`{{.Label "agentbox.keep"}}`,
	})
	if err != nil {
		return nil, err
	}
	return ParseContainerList(out), nil
}

// RunningProjectRuns counts live containers for one project (concurrent-run
// warning, FR-5).
func RunningProjectRuns(containers []ContainerInfo, project string) int {
	n := 0
	for _, c := range containers {
		if c.Project == project && c.State == "running" {
			n++
		}
	}
	return n
}

// SweepStale removes containers whose agentbox.owner process is dead
// (SIGKILL / terminal close recovery, FR-3). It never touches containers with
// a live owner (concurrent runs, UC2) and never selects by the shared
// agentbox=1 label. Containers marked agentbox.keep=1 are skipped: an
// intentionally kept container is not stale even though its owner is gone.
func SweepStale(ctx context.Context, e engine.Engine, containers []ContainerInfo, selfBootID string, procAlive func(int) bool) ([]string, error) {
	var removed []string
	for _, c := range containers {
		if c.Keep == "1" {
			continue
		}
		if OwnerAlive(c.Owner, selfBootID, procAlive) {
			continue
		}
		if _, err := engine.Capture(ctx, e, []string{e.Name(), "rm", "-f", c.ID}); err != nil {
			return removed, err
		}
		removed = append(removed, c.ID)
	}
	return removed, nil
}

// RemoveOwn force-removes only this run's container, selected by the
// agentbox.run=<uuid> label (FR-3). It never uses the shared agentbox=1 label.
func RemoveOwn(ctx context.Context, e engine.Engine, runID string) error {
	out, err := engine.Capture(ctx, e, []string{
		e.Name(), "ps", "-aq", "--filter", "label=agentbox.run=" + runID,
	})
	if err != nil {
		return err
	}
	for _, id := range strings.Fields(out) {
		if _, err := engine.Capture(ctx, e, []string{e.Name(), "rm", "-f", id}); err != nil {
			return err
		}
	}
	return nil
}

// ProcAlive reports whether /proc/<pid> exists.
func ProcAlive(pid int) bool {
	return pid > 0 && dirExists(fmt.Sprintf("/proc/%d", pid))
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
