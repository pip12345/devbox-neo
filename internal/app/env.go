package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func (e *Engine) validateEnvSource(r store.Record, source config.EnvSource) error {
	if source.Field == "global_env" && source.Path == filepath.Join(e.Store.Home, "config.json") {
		_, err := fsutil.Path(e.Store.Home, "config.json")
		return err
	}
	if source.Field != "extra_env" {
		return fmt.Errorf("env field does not match its owner")
	}
	if source.Path == filepath.Join(r.Identity.Workspace, ".devbox/config.json") {
		_, err := fsutil.Path(r.Identity.Workspace, ".devbox/config.json")
		return err
	}
	root := filepath.Join(e.Store.Home, "profiles")
	rel, err := filepath.Rel(root, source.Path)
	if err != nil {
		return err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || !config.Name.MatchString(parts[0]) || parts[1] != "config.json" {
		return fmt.Errorf("env source is outside configuration owners")
	}
	_, err = fsutil.Path(root, rel)
	return err
}
