package buildinfo

import "testing"

func TestCurrentFallsBackToDev(t *testing.T) {
	GitSHA = ""
	DeployedAt = ""
	view := Current()
	if view.GitSHA != "dev" {
		t.Fatalf("got git_sha %q", view.GitSHA)
	}
	if view.DeployedAt != "" {
		t.Fatalf("got deployed_at %q", view.DeployedAt)
	}
}

func TestCurrentReturnsBuildValues(t *testing.T) {
	GitSHA = "abc1234"
	DeployedAt = "2026-08-12T10:39:00Z"
	view := Current()
	if view.GitSHA != "abc1234" {
		t.Fatalf("got git_sha %q", view.GitSHA)
	}
	if view.DeployedAt != "2026-08-12T10:39:00Z" {
		t.Fatalf("got deployed_at %q", view.DeployedAt)
	}
}
