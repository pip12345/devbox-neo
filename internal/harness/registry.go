package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"devbox/internal/fsutil"
)

type Issue struct {
	Name   string
	Origin string
	Err    error
}
type Registry struct {
	Valid   []Effective
	Invalid []Issue
}

// Enumerate reports invalid overrides instead of replacing them with built-ins.
// A selected harness still uses Load, so unrelated invalid entries cannot block it.
func Enumerate(home string) (Registry, error) {
	var registry Registry
	entries, err := builtins.ReadDir("builtin")
	if err != nil {
		return registry, err
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	root, err := fsutil.Path(home, "harnesses")
	if err != nil {
		return registry, err
	}
	entries, err = os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return registry, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		p, pathErr := fsutil.Path(root, filepath.Join(entry.Name(), "harness.json"))
		if pathErr != nil {
			names[entry.Name()] = true
			continue
		}
		info, statErr := os.Lstat(p)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return registry, statErr
		}
		if !info.Mode().IsRegular() {
			registry.Invalid = append(registry.Invalid, Issue{entry.Name(), p, fmt.Errorf("definition must be a regular file")})
			delete(names, entry.Name())
			continue
		}
		names[entry.Name()] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		h, err := Load(home, name)
		if err != nil {
			registry.Invalid = append(registry.Invalid, Issue{name, filepath.Join(root, name, "harness.json"), err})
			continue
		}
		registry.Valid = append(registry.Valid, h)
	}
	sort.Slice(registry.Invalid, func(i, j int) bool { return registry.Invalid[i].Name < registry.Invalid[j].Name })
	return registry, nil
}
