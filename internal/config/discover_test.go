package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/config"
)

// mustWrite writes a file with default permissions; t.Fatal on error.
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// resetXDG restores the test process's XDG_CONFIG_HOME after a
// test temporarily sets it. Tests that mutate the env restore via
// t.Cleanup so parallel-test runs (if ever enabled) don't bleed.
func resetXDG(t *testing.T) {
	t.Helper()
	prev, prevSet := os.LookupEnv("XDG_CONFIG_HOME")
	t.Cleanup(func() {
		if prevSet {
			_ = os.Setenv("XDG_CONFIG_HOME", prev)
		} else {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		}
	})
	_ = os.Unsetenv("XDG_CONFIG_HOME")
}

// TestDiscover_ProjectConfigInCwd: a tempdir with .distill-ai.toml
// resolves to its path.
func TestDiscover_ProjectConfigInCwd(t *testing.T) {
	resetXDG(t)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".distill-ai.toml"), `default_budget = 1`)
	project, _, err := config.Discover(dir, t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want, _ := filepath.Abs(filepath.Join(dir, ".distill-ai.toml"))
	if project != want {
		t.Errorf("project = %q, want %q", project, want)
	}
}

// TestDiscover_ProjectConfigInParent: walks up from a child dir to
// find the parent's .distill-ai.toml.
func TestDiscover_ProjectConfigInParent(t *testing.T) {
	resetXDG(t)
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".distill-ai.toml"), ``)
	child := filepath.Join(root, "sub", "nested")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	project, _, err := config.Discover(child, t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want, _ := filepath.Abs(filepath.Join(root, ".distill-ai.toml"))
	if project != want {
		t.Errorf("project = %q, want %q", project, want)
	}
}

// TestDiscover_StopsAtGitRoot: a .git/ directory at the parent
// stops the walk even when no config exists at or above it.
func TestDiscover_StopsAtGitRoot(t *testing.T) {
	resetXDG(t)
	root := t.TempDir()
	// Place .git/ at root and a stray config one dir above.
	gitDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A higher-up config that Discover must NOT pick up.
	mustWrite(t, filepath.Join(root, ".distill-ai.toml"), ``)
	child := filepath.Join(gitDir, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	project, _, err := config.Discover(child, t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if project != "" {
		t.Errorf("project = %q, want empty (git-root stop)", project)
	}
}

// TestDiscover_ConfigAtGitRootIsFound: the git-root stop happens
// after the config check, so a config sitting at the git root
// still wins.
func TestDiscover_ConfigAtGitRootIsFound(t *testing.T) {
	resetXDG(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustWrite(t, filepath.Join(root, ".distill-ai.toml"), ``)
	project, _, err := config.Discover(root, t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want, _ := filepath.Abs(filepath.Join(root, ".distill-ai.toml"))
	if project != want {
		t.Errorf("project = %q, want %q", project, want)
	}
}

// TestDiscover_HonoursXdgConfigHome: XDG_CONFIG_HOME wins over
// the home-directory fallback.
func TestDiscover_HonoursXdgConfigHome(t *testing.T) {
	resetXDG(t)
	xdg := t.TempDir()
	mustWrite(t, filepath.Join(xdg, "distill-ai", "config.toml"), ``)
	if err := os.Setenv("XDG_CONFIG_HOME", xdg); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	home := t.TempDir() // ignored when XDG is set
	_, user, err := config.Discover(t.TempDir(), home)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := filepath.Join(xdg, "distill-ai", "config.toml")
	if user != want {
		t.Errorf("user = %q, want %q", user, want)
	}
}

// TestDiscover_FallsBackToHomeConfig: with XDG unset, looks under
// $HOME/.config/distill-ai/config.toml.
func TestDiscover_FallsBackToHomeConfig(t *testing.T) {
	resetXDG(t)
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".config", "distill-ai", "config.toml"), ``)
	_, user, err := config.Discover(t.TempDir(), home)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := filepath.Join(home, ".config", "distill-ai", "config.toml")
	if user != want {
		t.Errorf("user = %q, want %q", user, want)
	}
}

// TestDiscover_NoConfigsReturnsEmpty: a clean tempdir for cwd and
// home produces two empty paths.
func TestDiscover_NoConfigsReturnsEmpty(t *testing.T) {
	resetXDG(t)
	project, user, err := config.Discover(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if project != "" || user != "" {
		t.Errorf("expected both empty, got project=%q user=%q", project, user)
	}
}

// TestDiscover_DepthCapPreventsRunaway: a very deep path with no
// git root, no config, and the user's home not on the same branch
// resolves to an empty project path without timing out. We can't
// easily construct an actually-pathological symlink without
// root, so this test uses a moderately deep tempdir tree as a
// regression guard.
func TestDiscover_DepthCapPreventsRunaway(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("deep path semantics differ on Windows")
	}
	resetXDG(t)
	root := t.TempDir()
	deep := root
	for i := 0; i < 8; i++ {
		deep = filepath.Join(deep, "x")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	project, _, err := config.Discover(deep, t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if project != "" {
		t.Errorf("project = %q, want empty (no config in tree)", project)
	}
}

// TestDiscover_EmptyCwdSkipsProjectWalk: an empty cwd disables
// the project-config search entirely.
func TestDiscover_EmptyCwdSkipsProjectWalk(t *testing.T) {
	resetXDG(t)
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".config", "distill-ai", "config.toml"), ``)
	project, user, err := config.Discover("", home)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if project != "" {
		t.Errorf("project = %q, want empty", project)
	}
	if user == "" {
		t.Errorf("user = empty, want resolved path")
	}
}

// TestDiscover_EmptyHomeSkipsUserConfig: an empty home arg with
// XDG unset disables the user-config search.
func TestDiscover_EmptyHomeSkipsUserConfig(t *testing.T) {
	resetXDG(t)
	_, user, err := config.Discover(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if user != "" {
		t.Errorf("user = %q, want empty", user)
	}
}

// TestLoadAll_BothPresentMerges: project + user configs both
// exist; LoadAll merges with project winning.
func TestLoadAll_BothPresentMerges(t *testing.T) {
	resetXDG(t)
	projectDir := t.TempDir()
	mustWrite(t, filepath.Join(projectDir, ".distill-ai.toml"),
		`default_budget = 200`+"\n"+`default_output = "json"`+"\n")
	homeDir := t.TempDir()
	mustWrite(t, filepath.Join(homeDir, ".config", "distill-ai", "config.toml"),
		`default_budget = 100`+"\n"+`default_tokenizer = "tiktoken"`+"\n")
	cfg, err := config.LoadAll(projectDir, homeDir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if cfg == nil {
		t.Fatalf("LoadAll returned nil Config")
	}
	if cfg.DefaultBudget != 200 {
		t.Errorf("DefaultBudget = %d, want 200 (project wins)", cfg.DefaultBudget)
	}
	if cfg.DefaultOutput != "json" {
		t.Errorf("DefaultOutput = %q, want json (project sets, user absent)", cfg.DefaultOutput)
	}
	if cfg.DefaultTokenizer != "tiktoken" {
		t.Errorf("DefaultTokenizer = %q, want tiktoken (user sets, project absent)", cfg.DefaultTokenizer)
	}
}

// TestLoadAll_NeitherPresentReturnsEmpty: with no configs present
// LoadAll returns a non-nil zero Config and no error.
func TestLoadAll_NeitherPresentReturnsEmpty(t *testing.T) {
	resetXDG(t)
	cfg, err := config.LoadAll(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if cfg == nil {
		t.Fatalf("LoadAll returned nil Config")
	}
	if cfg.DefaultBudget != 0 || cfg.DefaultOutput != "" {
		t.Errorf("expected zero Config, got %+v", cfg)
	}
}

// TestLoadAll_PropagatesLoadError: a malformed project config
// surfaces a *LoadError carrying the path.
func TestLoadAll_PropagatesLoadError(t *testing.T) {
	resetXDG(t)
	projectDir := t.TempDir()
	mustWrite(t, filepath.Join(projectDir, ".distill-ai.toml"), `default_budget =`)
	_, err := config.LoadAll(projectDir, t.TempDir())
	if err == nil {
		t.Fatalf("LoadAll: expected error from malformed project config")
	}
	if !strings.Contains(err.Error(), projectDir) {
		t.Errorf("error %q does not name project dir", err)
	}
}
