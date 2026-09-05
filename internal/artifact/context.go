package artifact

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/fsutil"
	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

type ContextFile struct {
	Data      []byte
	Mode      os.FileMode
	Directory bool
}
type BuildContext struct {
	Dockerfile []byte
	Files      map[string]ContextFile
	Ignore     []byte
	IgnoreName string
}

// ReadBuildContext captures the exact source-side input policy. The executor
// stages this snapshot; neither compilation nor execution rereads source files.
func ReadBuildContext(source string) (BuildContext, error) {
	var result BuildContext
	root := filepath.Dir(source)
	if _, err := fsutil.Path(root, filepath.Base(source)); err != nil {
		return result, err
	}
	b, err := os.ReadFile(source)
	if err != nil {
		return result, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return result, fmt.Errorf("selected Dockerfile is empty")
	}
	result.Dockerfile = b
	ignorePath, err := fsutil.Path(root, filepath.Base(source)+".dockerignore")
	if err != nil {
		return result, err
	}
	ignoreData, err := os.ReadFile(ignorePath)
	if os.IsNotExist(err) {
		ignorePath, err = fsutil.Path(root, ".dockerignore")
		if err != nil {
			return result, err
		}
		ignoreData, err = os.ReadFile(ignorePath)
	}
	if err != nil && !os.IsNotExist(err) {
		return result, err
	}
	if err == nil {
		result.IgnoreName = filepath.Base(ignorePath)
	}
	patterns, err := ignorefile.ReadAll(bytes.NewReader(ignoreData))
	if err != nil {
		return result, err
	}
	matcher, err := patternmatcher.New(patterns)
	if err != nil {
		return result, fmt.Errorf("invalid Docker ignore patterns: %w", err)
	}
	result.Files = map[string]ContextFile{}
	err = filepath.WalkDir(root, func(p string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		excluded, err := matcher.MatchesOrParentMatches(rel)
		if err != nil {
			return err
		}
		// A negation may include a child of an excluded directory.
		if excluded {
			if entry.IsDir() && !matcher.Exclusions() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			result.Files[rel] = ContextFile{Mode: info.Mode().Perm(), Directory: true}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("build context supports only regular files: %s", p)
		}
		var data []byte
		if p == source {
			data = b
		} else if p == ignorePath {
			data = ignoreData
		} else {
			data, err = os.ReadFile(p)
			if err != nil {
				return err
			}
		}
		result.Files[rel] = ContextFile{Data: data, Mode: info.Mode().Perm()}
		return nil
	})
	result.Ignore = ignoreData
	return result, err
}
