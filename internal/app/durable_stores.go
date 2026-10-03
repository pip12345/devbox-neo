package app

import (
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/store"
)

// Directory creation is appropriate for a new harness store, not a missing
// previously committed store: silently replacing it would conceal lost history.
func checkDurableStores(l *store.Locked, r store.Record) error {
	for _, s := range r.Applied.Stores {
		if s.Scope != "environment" {
			continue
		}
		root, err := l.Path(filepath.Join("harnesses", r.Applied.Definition.Name, "stores", s.Name))
		if err != nil {
			return err
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("saved harness store is unavailable at %s: %w", root, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("saved harness store is not a directory: %s", root)
		}
	}
	return nil
}
