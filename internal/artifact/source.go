package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

var SingletonNames = []string{"Dockerfile", "setup.sh", "entrypoint.sh"}

func hasProjectArtifacts(root string) (bool, error) {
	if _, err := fsutil.Path(root, "."); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if slices.Contains(SingletonNames, entry.Name()) || (config.Name.MatchString(entry.Name()) && (entry.IsDir() || entry.Type()&os.ModeSymlink != 0)) {
			return true, nil
		}
	}
	return false, nil
}

// SourceTree copies only the profile artifact layout, without injecting global
// settings or harness defaults. Named config directories need not be selected.
func SourceTree(root string, harnessNames map[string]bool) (map[string]harness.File, error) {
	if _, err := fsutil.Path(root, "."); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	result := map[string]harness.File{}
	p, err := fsutil.Path(root, "Dockerfile")
	if err != nil {
		return nil, err
	}
	if _, err = os.Lstat(p); err != nil && !os.IsNotExist(err) {
		return nil, err
	} else if err == nil {
		context, err := ReadBuildContext(p)
		if err != nil {
			return nil, err
		}
		for name, file := range context.Files {
			mode := file.Mode
			if file.Directory {
				mode |= os.ModeDir
			}
			result[name] = harness.File{Data: file.Data, Mode: mode}
		}
		result["Dockerfile"] = harness.File{Data: context.Dockerfile, Mode: 0600}
		if context.IgnoreName != "" {
			result[context.IgnoreName] = harness.File{Data: context.Ignore, Mode: 0600}
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "config.json" || slices.Contains(SingletonNames, name) {
			if _, captured := result[name]; captured {
				continue
			}
			p, err := fsutil.Path(root, name)
			if err != nil {
				return nil, err
			}
			info, err := os.Lstat(p)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("artifact must be a regular file: %s", p)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			result[name] = harness.File{Data: b, Mode: info.Mode().Perm()}
			continue
		}
		if harnessNames[name] && (entry.IsDir() || entry.Type()&os.ModeSymlink != 0) {
			tree, err := harness.ReadTree(filepath.Join(root, name))
			if err != nil {
				return nil, err
			}
			for p, file := range tree {
				result[filepath.Join(name, p)] = file
			}
		}
	}
	if _, ok := result["config.json"]; !ok {
		return nil, fmt.Errorf("profile config is missing: %s", filepath.Join(root, "config.json"))
	}
	if _, err := config.ParseLayer(result["config.json"].Data, false); err != nil {
		return nil, fmt.Errorf("invalid source profile: %w", err)
	}
	return result, nil
}
