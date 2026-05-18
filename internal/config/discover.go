package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// projectConfigFilename is the basename Discover looks for when
// walking from CWD toward the filesystem root. Exposed as a
// constant so tests and callers stay in sync with the documented
// filename in docs/config.md.
const projectConfigFilename = ".distill-ai.toml"

// userConfigSubpath is the path under XDG_CONFIG_HOME (or
// $HOME/.config when XDG is unset) at which Discover looks for the
// user config.
const userConfigSubpath = "distill-ai/config.toml"

// gitRootMarker is the directory name Discover stops at while
// walking toward the filesystem root. The stop protects against a
// working directory inside a clone of a repo without its own
// config from accidentally picking up the user's parent-directory
// home config; the user-config arm covers the parent-directory
// case explicitly.
const gitRootMarker = ".git"

// discoverDepthCap limits how many directory steps Discover takes
// before giving up on the project-config walk. Protects against
// pathological symlink loops and against a CWD beneath an
// exceptionally deep filesystem. Chosen as 32: ARCHITECTURE.md does
// not pin a number; 32 is generous (a monorepo "src/foo/bar/baz"
// from any reasonable home directory still resolves) without
// allowing runaway behaviour.
const discoverDepthCap = 32

// Discover returns the absolute paths to the project and user
// config files that Load should consult, or empty strings when a
// file does not exist. The two paths are returned in their
// precedence order (M14.3); a non-empty value means "this file
// exists and should be loaded."
//
// Project config: walk from cwd toward the filesystem root,
// stopping at the first directory that either contains
// `.distill-ai.toml` or `.git/`. The walk is bounded by
// discoverDepthCap to protect against symlink loops.
//
// User config: `$XDG_CONFIG_HOME/distill-ai/config.toml` if
// `XDG_CONFIG_HOME` is set and non-empty, else
// `<home>/.config/distill-ai/config.toml`. Both arms honour the
// supplied home argument so tests can override; production callers
// pass `os.UserHomeDir()`.
//
// Discover does not touch the filesystem for files it does not
// find — callers can safely call Load on the empty string and get
// a no-op Config back via LoadAll.
//
// An empty cwd is treated as "no project walk"; an empty home is
// treated as "no user config." Both arms are independent: a CWD
// outside a project may still have a user config, and vice versa.
func Discover(cwd, home string) (project, user string, err error) {
	project, err = discoverProjectConfig(cwd)
	if err != nil {
		return "", "", err
	}
	// discoverUserConfig never surfaces an error today (a missing
	// file is "absent", not an error), but the err return is
	// reserved so a future XDG-permission check or symlink-
	// validation arm can flag a malformed home without changing
	// every caller.
	user = discoverUserConfig(home)
	return project, user, nil
}

// LoadAll discovers the two configs, loads each non-empty path,
// and returns the precedence-merged result. The merge precedence
// is project > user > built-in default, implemented by M14.3's
// Merge function.
//
// Both Load steps may fail. Errors are returned as-is so the
// caller can distinguish a malformed config (typo, missing
// required field) from a missing config file. Discover does not
// surface a missing file as an error — only a present-but-broken
// file does.
//
// When both configs are absent LoadAll returns an empty *Config
// and a nil error. Callers can pass the result straight to
// ApplyToOptions without nil-checking the individual sub-configs.
func LoadAll(cwd, home string) (*Config, error) {
	projectPath, userPath, err := Discover(cwd, home)
	if err != nil {
		return nil, err
	}
	var project, user *Config
	if projectPath != "" {
		project, err = Load(projectPath)
		if err != nil {
			return nil, err
		}
	}
	if userPath != "" {
		user, err = Load(userPath)
		if err != nil {
			return nil, err
		}
	}
	return Merge(user, project), nil
}

// discoverProjectConfig is the CWD-walking arm of Discover. The
// walk stops on the first hit (a directory containing the project
// config), at the first git root (whether or not it carries a
// config), at the filesystem root, or when discoverDepthCap is
// exhausted.
func discoverProjectConfig(cwd string) (string, error) {
	if cwd == "" {
		return "", nil
	}
	// Resolve to absolute so the loop's parent-directory walk
	// terminates predictably (filepath.Dir("foo") returns ".",
	// not the user's actual parent; the absolute form gives us
	// filepath.Dir("/") = "/" which is the canonical stop).
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	dir := abs
	for depth := 0; depth < discoverDepthCap; depth++ {
		configPath := filepath.Join(dir, projectConfigFilename)
		if fileExists(configPath) {
			return configPath, nil
		}
		gitPath := filepath.Join(dir, gitRootMarker)
		if pathExists(gitPath) {
			// At a git root with no config: stop. We do not
			// want to walk past a project's boundary into the
			// user's home or a sibling repo.
			return "", nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root.
			return "", nil
		}
		dir = parent
	}
	return "", nil
}

// discoverUserConfig is the XDG / home arm of Discover. Honours
// XDG_CONFIG_HOME when non-empty; otherwise falls back to
// <home>/.config. Returns the path when the file exists, or the
// empty string when it does not.
func discoverUserConfig(home string) string {
	candidate := userConfigCandidatePath(home)
	if candidate == "" || !fileExists(candidate) {
		return ""
	}
	return candidate
}

// userConfigCandidatePath resolves the user config location
// without checking the filesystem. Split out from
// discoverUserConfig so tests can assert the path-derivation rule
// directly.
func userConfigCandidatePath(home string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, userConfigSubpath)
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", userConfigSubpath)
}

// fileExists reports whether path resolves to an existing regular
// file. Symlinks pointing at regular files count; symlinks pointing
// at directories do not. The function suppresses all errors —
// "permission denied" on a parent directory is indistinguishable
// from "absent" for Discover's purposes.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// pathExists reports whether path resolves to an existing
// filesystem entry of any kind (file, directory, symlink). Used by
// the git-root check, which cares only about the presence of the
// `.git/` directory marker.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
