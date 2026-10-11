package run

import (
	"regexp"
	"strings"
	"testing"
)

func TestNamingDefault(t *testing.T) {
	n := BuildNaming(NamingInput{
		Canonical: "/home/me/src/app",
		UID:       1000,
		PID:       4242,
		BootID:    "boot-a1b2",
		Rand:      func() string { return "8f3c2a" },
	})
	if n.Name != "agentbox-app-8f3c2a" {
		t.Errorf("Name = %q", n.Name)
	}
	if !strings.HasPrefix(n.Project, "app-") || len(n.Project) != len("app-")+8 {
		t.Errorf("Project = %q, want app-<hash8>", n.Project)
	}
	if n.RunID == "" || n.RunID == n.Name {
		t.Errorf("RunID = %q", n.RunID)
	}
	if n.Owner != "4242@boot-a1b2" {
		t.Errorf("Owner = %q", n.Owner)
	}
}

func TestNamingProjectIDStableAndPathSpecific(t *testing.T) {
	in := NamingInput{UID: 1000, PID: 1, BootID: "b", Rand: func() string { return "aaaaaa" }}
	in.Canonical = "/home/me/src/app"
	a := BuildNaming(in)
	b := BuildNaming(in) // same path, same everything
	if a.Project != b.Project || a.Name != b.Name {
		t.Errorf("same input must give same ids: %+v vs %+v", a, b)
	}
	in.Canonical = "/home/me/src/other"
	c := BuildNaming(in)
	if a.Project == c.Project {
		t.Errorf("different paths must give different project ids: %q", a.Project)
	}
	if !strings.HasPrefix(c.Project, "other-") {
		t.Errorf("project slug = %q, want other-...", c.Project)
	}
}

func TestNamingProjectSlugSanitized(t *testing.T) {
	in := NamingInput{Canonical: "/x/My App!", UID: 1, PID: 1, BootID: "b", Rand: func() string { return "aaaaaa" }}
	n := BuildNaming(in)
	if !strings.HasPrefix(n.Project, "my-app-") {
		t.Errorf("Project = %q, want my-app-<hash8>", n.Project)
	}
	valid := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	if !valid.MatchString(strings.TrimSuffix(n.Project, "-"+n.Project[len(n.Project)-8:])) {
		t.Errorf("slug not docker-name-safe: %q", n.Project)
	}
	if !regexp.MustCompile(`^agentbox-[a-z0-9][a-z0-9-]*-[a-z0-9]+$`).MatchString(n.Name) {
		t.Errorf("name not docker-name-safe: %q", n.Name)
	}
}

func TestNamingTestNamePinsEverything(t *testing.T) {
	n := BuildNaming(NamingInput{
		TestName:  "agentbox-testbox",
		Canonical: "/tmp/whatever-i-am",
		UID:       1000,
		PID:       999,
		BootID:    "boot-real",
		Rand:      func() string { return "zzzzzz" },
	})
	if n.Name != "agentbox-testbox" {
		t.Errorf("Name = %q", n.Name)
	}
	if n.RunID != "agentbox-test-run" {
		t.Errorf("RunID = %q", n.RunID)
	}
	if n.Owner != "1@agentbox-test" {
		t.Errorf("Owner = %q", n.Owner)
	}
	if n.Project != "test-project" {
		t.Errorf("Project = %q", n.Project)
	}
}

func TestNamingFallbackSlugForRoot(t *testing.T) {
	n := BuildNaming(NamingInput{Canonical: "/", UID: 0, PID: 1, BootID: "b", Rand: func() string { return "aaaaaa" }})
	if !strings.HasPrefix(n.Project, "workspace-") {
		t.Errorf("Project = %q, want workspace-<hash8>", n.Project)
	}
}
