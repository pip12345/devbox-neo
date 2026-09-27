package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var localNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func ValidateLocalName(name string) error {
	if !localNamePattern.MatchString(name) {
		return fmt.Errorf("session name must be 1–64 ASCII letters, digits, dashes, or underscores, beginning with a letter or digit")
	}
	return nil
}

func CanonicalWorkspace(workspace string) (string, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace must be a directory")
	}
	return canonical, nil
}

func Identify(workspace, localName string) (Identity, error) {
	if err := ValidateLocalName(localName); err != nil {
		return Identity{}, err
	}
	canonical, err := CanonicalWorkspace(workspace)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Binding: Binding{Workspace: canonical, LocalName: localName}}, nil
}

// Validate uses saved canonical paths without reopening the workspace. Exact
// session lookup and cleanup must work after a directory has moved or vanished.
func (id Binding) Validate() error {
	if err := ValidateLocalName(id.LocalName); err != nil {
		return err
	}
	if !filepath.IsAbs(id.Workspace) || filepath.Clean(id.Workspace) != id.Workspace || strings.ContainsRune(id.Workspace, '\x00') {
		return fmt.Errorf("invalid recorded workspace identity")
	}
	return nil
}

func (id Identity) Validate() error {
	if err := id.Binding.Validate(); err != nil {
		return err
	}
	if !ValidResourceName(id.Name) {
		return fmt.Errorf("invalid resource name")
	}
	return nil
}

func ValidResourceName(name string) bool {
	return strings.HasPrefix(name, ContainerPrefix) && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}

var sessionIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func IsSessionTarget(target string) bool { return sessionIDPattern.MatchString(target) }
