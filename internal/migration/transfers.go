package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
)

// Endpoint identity changes must not rewrite an unfinished transaction. Finish
// or abort it with the build which owns its format before converting sessions.
func requireResolvedTransfers(home string) error {
	root, err := fsutil.Path(home, "state/transfers")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path, err := fsutil.Path(root, entry.Name())
		if err != nil {
			return err
		}
		if entry.IsDir() {
			path, err = fsutil.Path(path, "journal.json")
			if err != nil {
				return err
			}
			if _, err := os.Lstat(path); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return err
			}
		}
		return commanderror.New("migration_pending_transfer", "Finish or abort pending transfers with the previous build before converting session identity and storage.", path, nil)
	}
	return nil
}

func transferMigration(ctx context.Context, home string) (*Requirement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := fsutil.Path(home, "state/transfers")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, commanderror.New("migration_pending_transfer", "Finish or abort preceding-format transfers with the previous build before converting this home.", filepath.Join(root, entry.Name()), fmt.Errorf("preceding transfer format"))
		}
	}
	return nil, nil
}
