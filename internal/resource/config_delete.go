package resource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
)

// A named config is a direct child of the selected home's configs directory.
// Path references and links are not deletion targets, even if they resolve to
// the same directory as a named config.
func (s Service) namedConfigPath(name string) (string, error) {
	if !filepath.IsLocal(name) || name == "." || name == "~" || strings.ContainsAny(name, "/\x00") {
		return "", fmt.Errorf("provide a named config from the selected home's configs directory")
	}
	return fsutil.Path(s.Home, filepath.Join("configs", name))
}

// CheckConfigDeletion is a pre-prompt snapshot. DeleteConfig repeats the check
// under the config owner lock; session creation and source edits do not acquire
// that lock, so this is not a guarantee against concurrent new references.
func (s Service) CheckConfigDeletion(ctx context.Context, name string) (string, error) {
	path, err := s.namedConfigPath(name)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return path, commanderror.New("config_missing", "Named config does not exist.", name, err,
			commanderror.Next("List named configs", "config", "list"))
	}
	if err != nil {
		return path, err
	}
	if !info.IsDir() {
		return path, fmt.Errorf("named config is not a directory: %s", path)
	}
	if err := s.checkConfigUsage(ctx, path); err != nil {
		return path, err
	}
	return path, nil
}

func (s Service) checkConfigUsage(ctx context.Context, target string) error {
	users, scanErr := s.ConfigUsers(ctx, Owner{Kind: "config", Root: target})
	if scanErr == nil && len(users) == 0 {
		return nil
	}
	steps := make([]commanderror.Step, 0, len(users)+1)
	for _, user := range users {
		steps = append(steps, commanderror.Next("Used by "+user.Session, "status", user.Session))
	}
	if scanErr != nil {
		steps = append(steps, commanderror.Next("Inspect incomplete session inventory", "list"))
		return commanderror.New("config_usage_unknown", "Cannot verify every saved session using this config; deletion was blocked.", target, scanErr, steps...)
	}
	return commanderror.New("config_in_use", "Config is in use by saved sessions.", target, nil, steps...)
}

// DeleteConfig removes the complete named directory, including optional files.
// The same owner lock excludes config create/edit while checking and removing it.
func (s Service) DeleteConfig(ctx context.Context, name string) (Result, error) {
	path, err := s.namedConfigPath(name)
	if err != nil {
		return Result{}, err
	}
	result := Result{Path: path}
	lock, err := s.lock(ctx, path)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(lock)
	if _, err = s.CheckConfigDeletion(ctx, name); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = os.RemoveAll(path); err != nil {
		return result, fmt.Errorf("remove config directory %s: %w", path, err)
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return result, err
	}
	defer parent.Close()
	if err = parent.Sync(); err != nil {
		return result, err
	}
	result.Deleted = []string{path}
	return result, nil
}
