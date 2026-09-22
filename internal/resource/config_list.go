package resource

import (
	"os"
	"path/filepath"

	"devbox/internal/config"
	"devbox/internal/fsutil"
)

// ConfigEntry describes a named directory even when its config needs repair.
// Arbitrary path-based configs are not registered under the selected home.
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
		item := ConfigEntry{Name: entry.Name(), Path: filepath.Join(root, entry.Name())}
		canonical, err := config.CanonicalPath(item.Path)
		if err == nil {
			var layer config.Layer
			_, layer, err = readLayer(Owner{Kind: "config", Name: item.Name, Root: canonical})
			if layer.Harness != nil {
				item.Harness = *layer.Harness
			}
		}
		if err != nil {
			item.Error = err.Error()
		}
		configs = append(configs, item)
	}
	return configs, nil
}
