package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/filesync"
	"devbox/internal/fsutil"
	"devbox/internal/sshshare"
	"devbox/internal/store"
)

func (e *Engine) sync(l *store.Locked, s environment.Spec) error {
	d := s.Harness.Definition
	base := filepath.Join("harnesses", d.Name)
	root, err := l.Dir(filepath.Join(base, "stores", d.Config.Store, d.Config.Path))
	if err != nil {
		return err
	}
	manifest, err := l.Path(filepath.Join(base, "managed-config.json"))
	if err != nil {
		return err
	}
	return filesync.Sync(root, manifest, d.Config.Store, s.Files, d.Merge)
}
func (e *Engine) mountPlan(l *store.Locked, s environment.Spec) ([]docker.Mount, error) {
	sshRoot, err := l.Dir(sshshare.RelativeRoot)
	if err != nil {
		return nil, err
	}
	mounts := []docker.Mount{{Source: s.Identity.Workspace, Target: "/workspace"}, {Source: sshRoot, Target: sshshare.Mount}}
	d := s.Harness.Definition
	for _, storeDef := range d.Stores {
		var source string
		var err error
		if storeDef.Scope == "environment" {
			source, err = l.Dir(filepath.Join("harnesses", d.Name, "stores", storeDef.Name))
		} else {
			source, err = fsutil.Dir(e.Store.Home, filepath.Join("cache/harnesses", d.Name, storeDef.Name), 0700)
		}
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, docker.Mount{Source: source, Target: storeDef.Target})
	}
	for _, auth := range d.Auth {
		rel := filepath.Join("auth", d.Name, auth.Source)
		source, err := fsutil.Path(e.Store.Home, rel)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(source)
		if os.IsNotExist(err) && auth.Create {
			if _, err = fsutil.Dir(e.Store.Home, filepath.Dir(rel), 0700); err != nil {
				return nil, err
			}
			if auth.Kind == "directory" {
				err = os.Mkdir(source, 0700)
			} else {
				var f *os.File
				f, err = os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err == nil {
					if strings.HasSuffix(source, ".json") {
						_, err = f.Write([]byte("{}\n"))
					}
					err = errors.Join(err, f.Close())
				}
			}
			if err != nil && !os.IsExist(err) {
				return nil, err
			}
			info, err = os.Stat(source)
		}
		if err != nil {
			return nil, commanderror.New("auth_unavailable", "Cannot access authentication "+auth.Kind+".", source, err)
		}
		if (auth.Kind == "directory" && !info.IsDir()) || (auth.Kind == "file" && !info.Mode().IsRegular()) {
			return nil, commanderror.New("invalid_auth_path", "Expected an authentication "+auth.Kind+".", source, nil)
		}
		mounts = append(mounts, docker.Mount{Source: source, Target: auth.Target})
	}
	return append(mounts, s.ExtraMounts...), nil
}
