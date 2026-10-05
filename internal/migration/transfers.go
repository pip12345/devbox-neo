package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func transferMigration(ctx context.Context, home string) (*Requirement, error) {
	root, err := fsutil.Path(home, "state/transfers")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	needed := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path, err := fsutil.Path(root, entry.Name())
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var header struct {
			Version int `json:"version"`
		}
		// Ordinary readers diagnose corruption; detection must not make an
		// unrelated broken journal a prerequisite for editing source configs.
		if json.Unmarshal(data, &header) == nil && header.Version == 3 {
			needed = true
		}
	}
	if !needed {
		return nil, nil
	}
	return &Requirement{
		Name:        "Update pending transfer recovery",
		Description: "Pending transfers keep their identities, endpoints and commit phase. Uncommitted attempts can retry with corrected current config or be explicitly aborted. Committed transfers still finish cleanup only.\nContainers and saved session data are unchanged.",
		Warning:     "Close other dbx commands before migrating. Do not use older builds with this home afterward.",
		apply:       func(ctx context.Context, _ docker.Runtime) error { return migrateTransfers(ctx, home) },
	}, nil
}

func readTransferUpdate(home, name string) (store.Transfer, bool, error) {
	path, err := fsutil.Path(home, filepath.Join("state/transfers", name))
	if err != nil {
		return store.Transfer{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return store.Transfer{}, false, err
	}
	invalid := func(cause error) (store.Transfer, bool, error) {
		return store.Transfer{}, false, commanderror.New("invalid_transfer_journal", "Cannot migrate the transfer journal. Repair it or restore a verified backup; reserved endpoints must not be guessed or discarded.", path, cause)
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return invalid(err)
	}
	var j store.Transfer
	old := header.Version == 3
	if old {
		var preceding struct {
			store.Transfer
			Desired environment.Fingerprints `json:"desired"`
		}
		if err := config.Decode(data, &preceding); err != nil {
			return invalid(err)
		}
		for _, hash := range []string{preceding.Desired.Image, preceding.Desired.Container, preceding.Desired.Runtime} {
			if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
				return invalid(fmt.Errorf("invalid preceding transfer fingerprints"))
			}
		}
		j = preceding.Transfer
		j.Version = 4
	} else {
		if err := config.Decode(data, &j); err != nil {
			return invalid(err)
		}
	}
	if err := j.Validate(); err != nil {
		return invalid(err)
	}
	if name != j.Source.Name+".json" {
		return invalid(fmt.Errorf("transfer source differs from journal filename"))
	}
	return j, old, nil
}

func migrateTransfers(ctx context.Context, home string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fsutil.Dir(home, "state/locks", 0700); err != nil {
		return err
	}
	s := &store.Store{Home: home}
	names, err := s.LockNames(ctx)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(names)
	root, err := fsutil.Path(home, "state/transfers")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var pending []store.Transfer
	var directories []string
	ids, reservations := map[string]string{}, map[string]string{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		j, old, err := readTransferUpdate(home, entry.Name())
		if err != nil {
			return err
		}
		for _, id := range []string{j.SourceID, j.DestinationID} {
			if prior := reservations[id]; prior != "" && prior != j.ID {
				return fmt.Errorf("conflicting transfer reservations")
			}
			reservations[id] = j.ID
		}
		if !old {
			continue
		}
		pending = append(pending, j)
		for name, id := range map[string]string{j.Source.Name: j.SourceID, j.Destination.Name: j.DestinationID} {
			if prior := ids[name]; prior != "" && prior != id {
				return fmt.Errorf("conflicting transfer directories")
			}
			if ids[name] == "" {
				directories = append(directories, name)
			}
			ids[name] = id
		}
	}
	slices.Sort(directories)
	locks, err := s.LockAll(ctx, directories, ids)
	if err != nil {
		return err
	}
	defer store.CloseAll(locks)
	for _, lock := range locks {
		if err := lock.RequireIdle(); err != nil {
			return err
		}
	}
	for _, j := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := fsutil.Path(root, j.Source.Name+".json")
		if err != nil {
			return err
		}
		if err := fsutil.JSON(path, j); err != nil {
			return err
		}
	}
	return nil
}
