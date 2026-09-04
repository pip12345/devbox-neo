package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Home keeps the development installation separate even when an inherited
// DEVBOX_HOME points at the existing installation. Overrides still fail closed
// for the conventional old home; there is no automatic fallback after rejection.
func Home(explicit, environment, userHome string) (string, error) {
	if userHome == "" {
		return "", fmt.Errorf("cannot determine the user home directory")
	}
	selected := explicit
	if selected == "" {
		selected = environment
	}
	if selected == "" {
		selected = filepath.Join(userHome, ".devbox-neo")
	}
	if selected == "~" {
		selected = userHome
	} else if strings.HasPrefix(selected, "~/") {
		selected = filepath.Join(userHome, selected[2:])
	}
	absolute, err := canonicalHomePath(selected)
	if err != nil {
		return "", err
	}
	old, err := canonicalHomePath(filepath.Join(userHome, ".devbox"))
	if err != nil {
		return "", err
	}
	if absolute == old || strings.HasPrefix(absolute, old+string(filepath.Separator)) {
		return "", fmt.Errorf("the development rewrite cannot use ~/.devbox; use ~/.devbox-neo or an isolated --home")
	}
	return absolute, nil
}

// Resolve existing ancestors so a fresh home can be created without trusting
// symlink aliases. An existing but unresolvable link is an error, not absence.
func canonicalHomePath(selected string) (string, error) {
	current, err := filepath.Abs(selected)
	if err != nil {
		return "", err
	}
	suffix := ""
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(resolveErr) {
			return "", resolveErr
		}
		if _, err = os.Lstat(current); err == nil {
			return "", resolveErr
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", resolveErr
		}
		suffix = filepath.Join(filepath.Base(current), suffix)
		current = parent
	}
}
