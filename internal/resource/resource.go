// Package resource owns config-directory creation and source edits, not runtime state.
package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
)

type Service struct{ Home string }

// Name retains the entered reference for command hints; Root is canonical and
// supplies the shared lock identity for every spelling of the same directory.
type Owner struct{ Kind, Name, Root string }
type Result struct {
	Path     string              `json:"path"`
	Harness  string              `json:"harness,omitempty"`
	Created  []string            `json:"created,omitempty"`
	Updated  []string            `json:"updated,omitempty"`
	Skipped  []string            `json:"skipped,omitempty"`
	Warnings []string            `json:"warnings,omitempty"`
	Next     []commanderror.Step `json:"next_steps,omitempty"`
}

func (o Owner) Command(action string) []string {
	return []string{"devbox-neo", "config", action, o.Name}
}
func (o Owner) step(action, reason string) commanderror.Step {
	return commanderror.Step{Command: o.Command(action), Reason: reason}
}

func (s Service) lock(ctx context.Context, root string) (*os.File, error) {
	dir, err := fsutil.Dir(s.Home, "state/locks/config", 0700)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(root))
	return fsutil.Lock(ctx, filepath.Join(dir, hex.EncodeToString(sum[:])+".lock"))
}
func readLayer(o Owner) ([]byte, config.Layer, error) {
	path, err := fsutil.Path(o.Root, "config.json")
	if err != nil {
		return nil, config.Layer{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, config.Layer{}, commanderror.New("config_missing", "Missing config directory or config.json.", path, err, o.step("create", "Create the config"))
	}
	if err != nil {
		return nil, config.Layer{}, err
	}
	layer, err := config.ParseLayer(data)
	if err != nil {
		err = fmt.Errorf("%s: %w", path, err)
	}
	return data, layer, err
}
func patch(data []byte, key string, value any, remove bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := config.Decode(data, &fields); err != nil {
		return nil, err
	}
	if remove {
		delete(fields, key)
	} else {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = encoded
	}
	encoded, err := json.MarshalIndent(fields, "", "  ")
	return append(encoded, '\n'), err
}
func privateMode(mode os.FileMode) os.FileMode {
	if mode&0111 != 0 {
		return 0700
	}
	return 0600
}
