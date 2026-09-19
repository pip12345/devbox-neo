package migration

import (
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/fsutil"
)

// Both source layouts describe the same stores. Resolve each moved directory
// independently because the old layout migration moved entries one at a time.
// Only exact aliases to verified canonical directories are accepted; two real
// copies are ambiguous and must never be silently merged or preferred.
type sourceSessionLayout struct {
	stores      map[string]string
	projection  string
	aliases     map[string]string
	directories map[string][]string
}

// Names from the old registry. Recognition is not import support: retained
// stores for other harnesses are reported, never interpreted as Pi/OpenCode.
var sourceHarnessNames = [...]string{"pi", "opencode", "claude", "codex", "copilot"}

func sourceLeasePaths(root string) []string {
	return []string{filepath.Join(root, ".active"), filepath.Join(root, "active"), filepath.Join(root, ".internal/leases/attached")}
}

func inspectSourceLayout(root string) (sourceSessionLayout, error) {
	l := sourceSessionLayout{stores: map[string]string{}, aliases: map[string]string{}, directories: map[string][]string{}}
	for _, namespace := range []struct {
		path    string
		allowed []string
	}{
		{root, append([]string{"session.json", "metadata.json", "harnesses", ".internal", "active", ".active", ".fallback-defaults", ".staged-harness", "proxy-ca"}, sourceHarnessNames[:]...)},
		{filepath.Join(root, "harnesses"), sourceHarnessNames[:]},
		{filepath.Join(root, ".internal"), []string{"harness-config", "leases", "proxy-ca"}},
		{filepath.Join(root, ".internal/harness-config"), []string{"fallback", "staged"}},
		{filepath.Join(root, ".internal/leases"), []string{"attached"}},
	} {
		if err := l.namespace(namespace.path, namespace.allowed); err != nil {
			return l, err
		}
	}
	for _, harness := range sourceHarnessNames {
		path, exists, err := l.directoryPair(filepath.Join(root, harness), filepath.Join(root, "harnesses", harness))
		if err != nil {
			return l, err
		}
		if exists {
			l.stores[harness] = path
		}
	}
	stage, _, err := l.directoryPair(filepath.Join(root, ".staged-harness"), filepath.Join(root, ".internal/harness-config/staged"))
	if err != nil {
		return l, err
	}
	l.projection = stage
	for _, pair := range [][2]string{
		{".fallback-defaults", ".internal/harness-config/fallback"},
		{"proxy-ca", ".internal/proxy-ca"},
	} {
		if _, _, err := l.directoryPair(filepath.Join(root, pair[0]), filepath.Join(root, pair[1])); err != nil {
			return l, err
		}
	}
	for _, path := range sourceLeasePaths(root) {
		if _, err := sourceRealDirectory(path); err != nil {
			return l, err
		}
	}
	return l, nil
}

// Inspect only directory membership of layout namespaces, not payload trees.
// Membership and alias hashes join the ordinary source snapshot, so changing a
// layout after review cannot redirect copying or container-config capture.
func (l *sourceSessionLayout) namespace(path string, allowed []string) error {
	exists, err := sourceRealDirectory(path)
	if err != nil || !exists {
		return err
	}
	entries, err := readEntries(path)
	if err != nil {
		return err
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name()+":"+e.Type().String())
		if !contains(allowed, e.Name()) {
			kind := "file"
			if e.IsDir() {
				kind = "directory"
			} else if e.Type()&os.ModeSymlink != 0 {
				kind = "symlink"
			} else if e.Type() != 0 {
				kind = "special file"
			}
			return fmt.Errorf("Unmapped session entry %q (%s) under %q; no data from this entry was mapped", e.Name(), kind, path)
		}
	}
	l.directories[path] = names
	return nil
}

func sourceRealDirectory(path string) (bool, error) {
	if _, err := fsutil.Path(path, "."); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("source layout path is not a directory: %q", path)
	}
	return true, nil
}

func (l *sourceSessionLayout) directoryPair(flat, nested string) (string, bool, error) {
	nestedExists, err := sourceRealDirectory(nested)
	if err != nil {
		return "", false, err
	}
	info, err := os.Lstat(flat)
	if os.IsNotExist(err) {
		if nestedExists {
			return nested, true, nil
		}
		// No projection exists yet. Use the namespace already present for an
		// actionable missing-path diagnostic, not a fallback to current profiles.
		if _, ok := l.directories[filepath.Dir(nested)]; ok {
			return nested, false, nil
		}
		return flat, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(flat)
		if err != nil {
			return "", false, err
		}
		target := link
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(flat), target)
		}
		if !nestedExists || filepath.Clean(target) != nested {
			return "", false, fmt.Errorf("Unexpected source layout alias %q -> %q; expected a real canonical directory at %q", flat, link, nested)
		}
		l.aliases[flat] = link
		return nested, true, nil
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("source layout path is not a directory: %q", flat)
	}
	if nestedExists {
		return "", false, fmt.Errorf("Ambiguous source layout: both %q and %q contain real directories; no data was merged", flat, nested)
	}
	return flat, true, nil
}
