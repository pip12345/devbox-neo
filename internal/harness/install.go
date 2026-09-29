package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
)

func readInstall(d Definition, origin string) (Tree, error) {
	if d.Install.Script == "" {
		return Tree{}, nil
	}
	var tree Tree
	var err error
	if origin == "builtin" {
		root := "builtin/" + d.Name + "/install"
		var source fs.FS
		source, err = fs.Sub(builtins, root)
		if err == nil {
			tree, err = readTree(source, root)
		}
	} else {
		tree, err = ReadTree(filepath.Join(filepath.Dir(origin), "install"))
	}
	if err != nil {
		return tree, err
	}
	if _, ok := tree.Files[d.Install.Script]; !ok {
		return tree, fmt.Errorf("install.script %q must name a regular file under install/: %w", d.Install.Script, fs.ErrNotExist)
	}
	return tree, nil
}

// The recorded definition contract includes its installation files, not just
// the JSON. Recovery must reject changed scripts just as it rejects changed env.
func definitionHash(definition []byte, install map[string]File) (string, error) {
	data := definition
	if len(install) != 0 {
		var err error
		data, err = json.Marshal(struct {
			Definition []byte
			Install    map[string]File
		}{definition, install})
		if err != nil {
			return "", err
		}
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
