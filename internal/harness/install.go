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
			tree, err = readTreeFiltered(source, root, false)
		}
	} else {
		tree, err = readTreeRoot(filepath.Join(filepath.Dir(origin), "install"), false)
	}
	if err != nil {
		return tree, err
	}
	if _, ok := tree.Files[d.Install.Script]; !ok {
		return tree, fmt.Errorf("install.script %q must name a regular file under install/: %w", d.Install.Script, fs.ErrNotExist)
	}
	return tree, nil
}

// Installation files participate in the definition identity so image reuse and
// runtime compatibility cannot overlook changed installed code.
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
