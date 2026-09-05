package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func publish(source, destination string, replace bool) error {
	var err error
	if replace {
		err = os.Rename(source, destination)
	} else {
		err = unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
		if err != nil {
			err = &os.LinkError{Op: "publish without replacement", Old: source, New: destination, Err: err}
		}
	}
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(destination))
}

// PublishDirectory makes a fully staged owner visible at once. Linux's
// no-replace rename protects even an empty destination created by another writer.
func PublishDirectory(source, destination string) error {
	if _, err := Path(source, "."); err != nil {
		return err
	}
	if _, err := Path(filepath.Dir(destination), filepath.Base(destination)); err != nil {
		return err
	}
	if err := filepath.WalkDir(source, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return syncDir(p)
		}
		return nil
	}); err != nil {
		return err
	}
	return publish(source, destination, false)
}
