// Package fsutil contains fail-closed filesystem primitives shared by state owners.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func ID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Path refuses symlink components, including the root. Callers must hold their
// owning lock; this is containment validation, not a sandbox against host writers.
func Path(root, rel string) (string, error) {
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path must remain inside its owner")
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("owner path must be absolute")
	}
	target := filepath.Join(root, rel)
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(target, string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink component is not supported: %s", current)
		}
	}
	return target, nil
}
func Dir(root, rel string, mode fs.FileMode) (string, error) {
	p, err := Path(root, rel)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(p, mode); err != nil {
		return "", err
	}
	return p, nil
}

func Write(path string, data []byte, mode fs.FileMode) error {
	return write(path, data, mode, true)
}

func WriteNew(path string, data []byte, mode fs.FileMode) error {
	return write(path, data, mode, false)
}

func write(path string, data []byte, mode fs.FileMode, replace bool) error {
	if _, err := Path(filepath.Dir(path), filepath.Base(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return publish(temp, path, replace)
}
func JSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, append(b, '\n'), 0600)
}
