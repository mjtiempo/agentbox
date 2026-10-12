package run

import (
	"context"
	"strings"
	"testing"

	"agentbox/internal/engine"
)

// sweepFakeEngine records RunStream argv so tests can assert which containers
// the stale sweep acted on.
type sweepFakeEngine struct {
	calls []string
}

func (f *sweepFakeEngine) Name() string                                            { return "docker" }
func (f *sweepFakeEngine) RunArgv(engine.RunSpec) []string                         { return nil }
func (f *sweepFakeEngine) DryRunArgv(engine.RunSpec, func(string) string) []string { return nil }
func (f *sweepFakeEngine) Run(context.Context, []string) (int, error)              { return 0, nil }
func (f *sweepFakeEngine) RunStream(_ context.Context, argv []string, _ engine.Stdio) (int, error) {
	f.calls = append(f.calls, strings.Join(argv, " "))
	return 0, nil
}

func TestParseContainerList(t *testing.T) {
	// Legacy two-field rows, four-field rows, and five-field rows carrying the
	// keep label must all parse (older callers keep working).
	out := strings.Join([]string{
		"abc123\t7@boot-a",
		"def456\t8@boot-a\tapp-1a2b3c4d\trunning",
		"ghi789\t9@boot-a\tapp-1a2b3c4d\texited\t1",
		"",
		"jkl012\t",
	}, "\n")
	got := ParseContainerList(out)
	if len(got) != 4 {
		t.Fatalf("parsed %d, want 4: %+v", len(got), got)
	}
	if got[0].ID != "abc123" || got[0].Owner != "7@boot-a" || got[0].Project != "" || got[0].Keep != "" {
		t.Errorf("2-field row = %+v", got[0])
	}
	if got[1].Project != "app-1a2b3c4d" || got[1].State != "running" || got[1].Keep != "" {
		t.Errorf("4-field row = %+v", got[1])
	}
	if got[2].Project != "app-1a2b3c4d" || got[2].State != "exited" || got[2].Keep != "1" {
		t.Errorf("5-field row = %+v", got[2])
	}
	if got[3].ID != "jkl012" || got[3].Owner != "" {
		t.Errorf("empty-owner row = %+v", got[3])
	}
}

func TestSweepStale(t *testing.T) {
	procAlive := func(pid int) bool { return pid == 7 }
	cases := []struct {
		name       string
		container  ContainerInfo
		wantRemove bool
	}{
		{"dead owner is swept", ContainerInfo{ID: "dead1", Owner: "8@boot-a"}, true},
		{"kept container is preserved", ContainerInfo{ID: "kept1", Owner: "8@boot-a", Keep: "1"}, false},
		{"live owner is preserved", ContainerInfo{ID: "live1", Owner: "7@boot-a"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &sweepFakeEngine{}
			removed, err := SweepStale(context.Background(), e, []ContainerInfo{tc.container}, "boot-a", procAlive)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantRemove {
				if len(removed) != 1 || removed[0] != tc.container.ID {
					t.Fatalf("removed = %v, want [%s]", removed, tc.container.ID)
				}
				if len(e.calls) != 1 || !strings.Contains(e.calls[0], "rm -f "+tc.container.ID) {
					t.Fatalf("calls = %v, want rm -f %s", e.calls, tc.container.ID)
				}
				return
			}
			if len(removed) != 0 {
				t.Fatalf("removed = %v, want none", removed)
			}
			if len(e.calls) != 0 {
				t.Fatalf("no engine call expected, got %v", e.calls)
			}
		})
	}
}
