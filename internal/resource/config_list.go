package resource

import (
	"os"
	"path/filepath"

	"devbox/internal/config"
	"devbox/internal/fsutil"
)

// ConfigEntry describes a config directory even when its config needs repair.
// Name is a selectable reference: a named config or a relative directory path.
type ConfigEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Harness string `json:"harness,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s Service) ListConfigs() ([]ConfigEntry, error) {
	root, err := fsutil.Path(s.Home, "configs")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	configs := []ConfigEntry{}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		configs = append(configs, inspectConfig(entry.Name(), filepath.Join(root, entry.Name())))
	}
	return configs, nil
}

func inspectConfig(name, path string) ConfigEntry {
	item := ConfigEntry{Name: name, Path: path}
	canonical, err := config.CanonicalPath(path)
	if err == nil {
		var layer config.Layer
		_, layer, err = readLayer(Owner{Kind: "config", Name: name, Root: canonical})
		if layer.Harness != nil {
			item.Harness = *layer.Harness
		}
	}
	if err != nil {
		item.Error = err.Error()
	}
	return item
}

// DiscoverConfigs only offers parseable config layers: a generic config.json
// filename alone does not identify a Devbox config. It does not resolve host
// expressions or require runnable settings, since layers may inherit values.
// Discovery neither registers sources nor changes a session's selection.
func DiscoverConfigs(directory string) ([]ConfigEntry, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	names := []string{"."}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			names = append(names, "./"+entry.Name())
		}
	}
	configs := []ConfigEntry{}
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Lstat(filepath.Join(path, "config.json"))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		item := inspectConfig(name, path)
		if item.Error == "" {
			configs = append(configs, item)
		}
	}
	return configs, nil
}
