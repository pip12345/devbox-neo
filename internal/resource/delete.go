package resource

import (
	"context"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
)

// DeleteProfile removes configuration only. Defaults, containers, and sessions
// remain separate owners; a selected missing profile is then an explicit error.
func (s Service) DeleteProfile(ctx context.Context, name string) (Result, error) {
	owner, err := s.Profile(name)
	if err != nil {
		return Result{}, err
	}
	result := Result{Path: owner.Root}
	lock, err := s.lock(ctx, owner.Root)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(lock)
	info, err := os.Stat(owner.Root)
	if os.IsNotExist(err) {
		return result, commanderror.New("owner_missing", "Profile does not exist", owner.Root, err, owner.step("create", "Create this profile"))
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, os.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = os.RemoveAll(owner.Root); err != nil {
		return result, err
	}
	parent, err := os.Open(filepath.Dir(owner.Root))
	if err != nil {
		return result, err
	}
	defer parent.Close()
	if err = parent.Sync(); err != nil {
		return result, err
	}
	result.Next = []commanderror.Step{{Command: []string{"devbox-neo", "profile", "list"}, Reason: "Inspect remaining profiles"}, {Command: []string{"devbox-neo", "profile", "set", "--clear"}, Reason: "Clear the default if it referred to the deleted profile"}}
	return result, nil
}
