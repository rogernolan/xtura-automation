package buildinfo

// GitSHA and DeployedAt are injected at build time via
// -ldflags "-X empirebus-tests/service/buildinfo.GitSHA=...".
var (
	GitSHA     string
	DeployedAt string
)

// View is the build metadata exposed to clients.
type View struct {
	GitSHA     string `json:"git_sha"`
	DeployedAt string `json:"deployed_at,omitempty"`
}

// Current returns the build metadata for this binary, falling back to a
// "dev" marker when the values were not injected (e.g. local go run).
func Current() View {
	sha := GitSHA
	if sha == "" {
		sha = "dev"
	}
	return View{GitSHA: sha, DeployedAt: DeployedAt}
}
