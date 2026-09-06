package store

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

// CopyTransferState copies only declared environment stores and their ownership
// manifests. Leases, records, auth overlays and shared caches are not portable
// session contents. The caller has stopped the source before entering here.
func (l *Locked) CopyTransferState(destination *Locked, j Transfer, definitions []harness.Definition) error {
	if err := transferLocks(l, destination, j); err != nil {
		return err
	}
	for _, d := range definitions {
		for _, s := range d.Stores {
			if s.Scope != "environment" {
				continue
			}
			rel := filepath.Join("harnesses", d.Name, "stores", s.Name)
			source, err := l.Path(rel)
			if err != nil {
				return err
			}
			target, err := destination.Path(rel)
			if err != nil {
				return err
			}
			skip := map[string]bool{}
			for _, a := range d.Auth {
				if strings.HasPrefix(a.Target, s.Target+"/") {
					skip[strings.TrimPrefix(a.Target, s.Target+"/")] = true
				}
			}
			if err = copyState(l.ctx, source, target, ".", skip); err != nil {
				return err
			}
		}
		rel := filepath.Join("harnesses", d.Name, "managed-config.json")
		source, err := l.Path(rel)
		if err != nil {
			return err
		}
		if _, err = os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		target, err := destination.Path(rel)
		if err != nil {
			return err
		}
		if err = copyState(l.ctx, source, target, ".", nil); err != nil {
			return err
		}
	}
	root, err := destination.Path(".")
	if err != nil {
		return err
	}
	dirs := []string{filepath.Dir(root)}
	if err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := l.ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	}); err != nil {
		return err
	}
	// Store copies sync their files and immediate directories. Also persist the
	// newly created harness/store ancestors before publishing a destination record.
	for i := len(dirs) - 1; i >= 0; i-- {
		f, err := os.Open(dirs[i])
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func copyState(ctx context.Context, source, target, rel string, skip map[string]bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if skip[rel] {
		return nil
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if _, err = fsutil.Dir(filepath.Dir(target), ".", 0700); err != nil {
		return err
	}
	switch {
	case info.IsDir():
		if err = os.Mkdir(target, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err = copyState(ctx, filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name()), filepath.Join(rel, entry.Name()), skip); err != nil {
				return err
			}
		}
		// Leave destination owner access intact for preparation and rollback even
		// when a tool made a source subtree read-only.
		if err = os.Chmod(target, info.Mode().Perm()|0700); err != nil {
			return err
		}
		f, err := os.Open(target)
		if err != nil {
			return err
		}
		defer f.Close()
		return f.Sync()
	case info.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(link, target)
	case info.Mode().IsRegular():
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		if _, err = io.Copy(out, contextReader{ctx, in}); err != nil {
			return err
		}
		if err = out.Chmod(info.Mode().Perm()); err != nil {
			return err
		}
		return out.Sync()
	default:
		return fmt.Errorf("unsupported session state entry: %s", source)
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
