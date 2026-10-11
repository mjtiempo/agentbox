package run

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"agentbox/internal/workspace"
)

// Pinned identities used when AGENTBOX_TEST_NAME is set (AC 21 determinism).
const (
	testRunID   = "agentbox-test-run"
	testOwner   = "1@agentbox-test"
	testProject = "test-project"
)

// NamingInput carries everything needed to derive container identity.
type NamingInput struct {
	TestName  string // AGENTBOX_TEST_NAME; pins all ids when non-empty
	Canonical string // canonical workspace path
	UID       int
	PID       int
	BootID    string
	Rand      func() string // injectable for tests; defaults to crypto/rand hex6
}

// Naming is the derived identity set.
type Naming struct {
	Name    string // agentbox-<slug>-<rand>  (TEST_NAME pins it whole)
	Project string // <slug>-<hash8>
	RunID   string // agentbox.run label
	Owner   string // agentbox.owner label: <pid>@<boot-id>
}

// BuildNaming derives the container name and labels per FR-3.
func BuildNaming(in NamingInput) Naming {
	if in.TestName != "" {
		return Naming{
			Name:    in.TestName,
			Project: testProject,
			RunID:   testRunID,
			Owner:   testOwner,
		}
	}
	rnd := in.Rand
	if rnd == nil {
		rnd = randomHex6
	}
	slug := slugify(filepath.Base(in.Canonical))
	return Naming{
		Name:    "agentbox-" + slug + "-" + rnd(),
		Project: slug + "-" + workspace.Hash8(in.Canonical),
		RunID:   "run-" + rnd(),
		Owner:   fmt.Sprintf("%d@%s", in.PID, in.BootID),
	}
}

// Labels returns the full label set in the canonical order rendered by the
// engine argv builder.
func (n Naming) Labels(uid int) map[string]string {
	return map[string]string{
		"agentbox":         "1",
		"agentbox.run":     n.RunID,
		"agentbox.project": n.Project,
		"agentbox.uid":     fmt.Sprint(uid),
		"agentbox.owner":   n.Owner,
	}
}

func slugify(base string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "workspace"
	}
	return s
}

func randomHex6() string {
	var buf [3]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "000000"
	}
	return hex.EncodeToString(buf[:])
}
