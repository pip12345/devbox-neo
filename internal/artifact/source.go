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

var ArtifactNames = []string{"Dockerfile", "setup.sh", "before-open.sh"}

// SourceTree captures the config artifact layout without injecting defaults.
// Import staging uses it independently of any session's selected source chain.
func SourceTree(root string, harnessNames map[string]bool) (harness.Tree, error) {
	result := harness.Tree{Files: map[string]harness.File{}}
	if _, err := fsutil.Path(root, "."); err != nil {
		return result, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return result, err
	}
	p, err := fsutil.Path(root, "Dockerfile")
	if err != nil {
		return result, err
	}
	if _, err = os.Lstat(p); err != nil && !os.IsNotExist(err) {
		return result, err
	} else if err == nil {
		context, err := ReadBuildContext(p)
		if err != nil {
			return result, err
		}
		for name, file := range context.Files {
			mode := file.Mode
			if file.Directory {
				mode |= os.ModeDir
			}
			result.Files[name] = harness.File{Data: file.Data, Mode: mode}
		}
		result.Files["Dockerfile"] = harness.File{Data: context.Dockerfile, Mode: 0600}
		if context.IgnoreName != "" {
			result.Files[context.IgnoreName] = harness.File{Data: context.Ignore, Mode: 0600}
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "config.json" || slices.Contains(ArtifactNames, name) {
			if _, captured := result.Files[name]; captured {
				continue
			}
			p, err := fsutil.Path(root, name)
			if err != nil {
				return result, err
			}
			info, err := os.Lstat(p)
			if err != nil {
				return result, err
			}
			if !info.Mode().IsRegular() {
				return result, fmt.Errorf("artifact must be a regular file: %s", p)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return result, err
			}
			result.Files[name] = harness.File{Data: b, Mode: info.Mode().Perm()}
			continue
		}
		if harnessNames[name] && (entry.IsDir() || entry.Type()&os.ModeSymlink != 0) {
			tree, err := harness.ReadTree(filepath.Join(root, name))
			result.Warnings = append(result.Warnings, tree.Warnings...)
			if err != nil {
				return result, err
			}
			for p, file := range tree.Files {
				result.Files[filepath.Join(name, p)] = file
			}
		}
	}
	if _, ok := result.Files["config.json"]; !ok {
		return result, fmt.Errorf("config.json is missing: %s", filepath.Join(root, "config.json"))
	}
	_, err = config.ParseLayer(result.Files["config.json"].Data)
	if err != nil {
		return result, fmt.Errorf("invalid source config: %w", err)
	}
	return result, nil
}
