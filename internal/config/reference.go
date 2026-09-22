package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reference retains the user's choice of portability. A fixed path does not
// follow a copied workspace, even when it originally pointed inside that folder.
// Source is the absolute directory input used only at the resolver boundary.
type Reference struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Path  string `json:"path"`
}

const (
	ReferenceRelative = "relative"
	ReferenceFixed    = "fixed"
)

func cleanAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, '\x00')
}

func (r Reference) Validate() error {
	if r.Label == "" || strings.ContainsRune(r.Label, '\x00') || r.Path == "" || filepath.Clean(r.Path) != r.Path || strings.ContainsRune(r.Path, '\x00') {
		return fmt.Errorf("invalid configuration reference")
	}
	switch r.Kind {
	case ReferenceRelative:
		if filepath.IsAbs(r.Path) {
			return fmt.Errorf("workspace-relative configuration reference must be relative")
		}
	case ReferenceFixed:
		if !cleanAbsolute(r.Path) {
			return fmt.Errorf("fixed configuration reference must be absolute")
		}
	default:
		return fmt.Errorf("invalid configuration reference kind %q", r.Kind)
	}
	return nil
}

// ConfigPath applies the same reference syntax to creation, editing and source
// selection. It does not inspect the filesystem: creation accepts absent paths,
// and source-chain repair must remain possible while directories are missing.
func ConfigPath(home, cwd, userHome, reference string) (string, error) {
	if reference == "" || strings.ContainsRune(reference, '\x00') {
		return "", fmt.Errorf("configuration reference must be a nonempty name or directory path")
	}
	switch {
	case filepath.IsAbs(reference):
		return filepath.Clean(reference), nil
	case reference == "~" || strings.HasPrefix(reference, "~/"):
		if !cleanAbsolute(userHome) {
			return "", fmt.Errorf("user home must be a clean absolute path")
		}
		return filepath.Join(userHome, strings.TrimPrefix(strings.TrimPrefix(reference, "~"), "/")), nil
	case relativeReference(reference):
		if !cleanAbsolute(cwd) {
			return "", fmt.Errorf("invoking directory must be a clean absolute path")
		}
		return filepath.Join(cwd, reference), nil
	default:
		if !cleanAbsolute(home) {
			return "", fmt.Errorf("Devbox home must be a clean absolute path")
		}
		return filepath.Join(home, "configs", reference), nil
	}
}

func relativeReference(reference string) bool {
	return !filepath.IsAbs(reference) && reference != "~" && !strings.HasPrefix(reference, "~/") && (strings.Contains(reference, "/") || reference == "." || reference == "..")
}

// CaptureReference resolves CLI paths against the invoking directory, then
// stores relative selections against the canonical workspace supplied by the
// session owner. Later invocations must not reinterpret them using their cwd.
func CaptureReference(home, workspace, cwd, userHome, input string) (Reference, error) {
	if !cleanAbsolute(workspace) {
		return Reference{}, fmt.Errorf("workspace must be a clean absolute path")
	}
	// Resolve an aliased invoking directory against the same physical workspace
	// identity used by sessions, but keep symlinks in the entered suffix so a
	// workspace-relative source can still follow its link after a transfer.
	if relativeReference(input) && cleanAbsolute(cwd) {
		var err error
		cwd, err = CanonicalPath(cwd)
		if err != nil {
			return Reference{}, err
		}
	}
	path, err := ConfigPath(home, cwd, userHome, input)
	if err != nil {
		return Reference{}, err
	}
	r := Reference{Label: input, Kind: ReferenceFixed, Path: path}
	if relativeReference(input) {
		r.Kind = ReferenceRelative
		r.Path, err = filepath.Rel(workspace, path)
		if err != nil {
			return Reference{}, err
		}
	}
	return r, r.Validate()
}

// Expand is purely structural. Saved identity and repair operations can use it
// without requiring a source to exist or contain runnable configuration.
func (r Reference) Expand(workspace string) (Source, error) {
	if err := r.Validate(); err != nil {
		return Source{}, err
	}
	if !cleanAbsolute(workspace) {
		return Source{}, fmt.Errorf("workspace must be a clean absolute path")
	}
	path := r.Path
	if r.Kind == ReferenceRelative {
		path = filepath.Join(workspace, path)
	}
	return Source{Label: r.Label, Path: path}, nil
}

// ValidateReferenceChain permits unavailable sources while checking structure
// and duplicate directory identities. It is used by pending creation choices
// and saved-source edits; runtime resolution separately requires every source.
func ValidateReferenceChain(workspace string, references []Reference) error {
	seen := map[string]bool{}
	for _, reference := range references {
		source, err := reference.Expand(workspace)
		if err != nil {
			return err
		}
		key := source.Path
		if canonical, err := filepath.EvalSymlinks(key); err == nil {
			key = canonical
		}
		if seen[key] {
			return fmt.Errorf("config directory selected more than once: %s", source.Path)
		}
		seen[key] = true
	}
	return nil
}

// ResolveReferences validates runtime directory inputs, including aliases of a
// directory selected more than once. Each source must contribute at most once,
// particularly because setup and before-open scripts have observable effects.
func ResolveReferences(workspace string, references []Reference) ([]Source, error) {
	if len(references) == 0 {
		return nil, fmt.Errorf("at least one configuration source is required")
	}
	sources := make([]Source, 0, len(references))
	seen := map[string]string{}
	for _, reference := range references {
		source, err := reference.Expand(workspace)
		if err != nil {
			return nil, err
		}
		canonical, err := filepath.EvalSymlinks(source.Path)
		if err != nil {
			return nil, fmt.Errorf("configuration source %q (%s): %w", source.Label, source.Path, err)
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, fmt.Errorf("configuration source %q (%s): %w", source.Label, source.Path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("configuration source %q (%s) is not a directory", source.Label, source.Path)
		}
		if previous, exists := seen[canonical]; exists {
			return nil, fmt.Errorf("configuration sources %q and %q select the same directory: %s", previous, source.Label, canonical)
		}
		seen[canonical] = source.Label
		source.Path = canonical
		sources = append(sources, source)
	}
	return sources, nil
}
