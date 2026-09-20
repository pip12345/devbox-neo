package app

import (
	"fmt"
	"path/filepath"

	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func (e *Engine) validateEnvSource(r store.Record, source config.EnvSource) error {
	if source.Field == "global_env" && source.Path == filepath.Join(e.Store.Home, "config.json") {
		_, err := fsutil.Path(e.Store.Home, "config.json")
		return err
	}
	if source.Field != "env" {
		return fmt.Errorf("env field does not match its owner")
	}
	for _, selected := range r.Sources {
		if source.Path == filepath.Join(selected.Path, "config.json") {
			_, err := fsutil.Path(selected.Path, "config.json")
			return err
		}
	}
	return fmt.Errorf("env source is outside recorded configuration sources")
}
