package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/store"
)

type TransferOptions struct {
	Mode        string
	Source      string
	Destination string
	Profile     string
	From        string
	To          string
	DryRun      bool
}
type TransferResult struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	SessionID   string `json:"session_id"`
	DryRun      bool   `json:"dry_run"`
}

func transferSlot(workspace, slot string) (environment.Identity, error) {
	if slot != ".project" && !config.Name.MatchString(slot) {
		return environment.Identity{}, fmt.Errorf("slot must be a profile name or .project")
	}
	return environment.Identify(workspace, slot, slot == ".project")
}
func (e *Engine) transferSource(ctx context.Context, q TransferOptions) (string, error) {
	if q.From != "" {
		id, err := transferSlot(q.Source, q.From)
		return id.Name, err
	}
	if strings.HasPrefix(q.Source, docker.Namespace+"-") && !strings.ContainsAny(q.Source, "/\\") {
		return q.Source, nil
	}
	r, err := e.Locate(ctx, q.Source, "")
	if err == nil {
		return r.Identity.Name, nil
	}
	// After committed source cleanup, its external journal is still retryable.
	if !os.IsNotExist(err) {
		return "", err
	}
	workspace, pathErr := filepath.Abs(q.Source)
	if pathErr != nil {
		return "", pathErr
	}
	if canonical, e := filepath.EvalSymlinks(workspace); e == nil {
		workspace = canonical
	}
	journals, listErr := e.Store.Transfers()
	if listErr != nil {
		return "", listErr
	}
	name := ""
	for _, j := range journals {
		if j.Source.Workspace == workspace {
			if name != "" {
				return "", fmt.Errorf("multiple pending transfers.\nUse the exact source name.")
			}
			name = j.Source.Name
		}
	}
	if name != "" {
		return name, nil
	}
	return "", err
}
func (e *Engine) transferDestination(q TransferOptions, source environment.Identity) (environment.Identity, error) {
	if q.To != "" {
		return transferSlot(q.Source, q.To)
	}
	profile := source.Profile
	if q.Profile != "" {
		profile = q.Profile
	}
	id, err := environment.Identify(q.Destination, profile, profile == "")
	if err != nil {
		return id, err
	}
	return id, nil
}
func (e *Engine) transferDefinitions(l *store.Locked, source store.Record, mode string) ([]harness.Definition, error) {
	root, err := l.Path("harnesses")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	definitions := []harness.Definition{}
	for _, entry := range entries {
		if !entry.IsDir() || !config.Name.MatchString(entry.Name()) {
			return nil, fmt.Errorf("invalid harness state directory")
		}
		effective, err := harness.Load(e.Store.Home, entry.Name())
		if err != nil {
			return nil, err
		}
		d := effective.Definition
		if (mode == "clone" && !d.Session.Clone) || (mode == "relocate" && !d.Session.Relocate) {
			return nil, fmt.Errorf("harness %s does not support %s", d.Name, mode)
		}
		if d.Name == source.Definition.Name && environment.Fingerprint(e.Store.Installation, effective.Hash) != source.Definition.Hash {
			return nil, fmt.Errorf("source harness definition changed.\n\nNext:\n  devbox-neo recreate %s\nThen retry the transfer.", source.Identity.Name)
		}
		path, err := l.Path(filepath.Join("harnesses", d.Name, "stores"))
		if err != nil {
			return nil, err
		}
		stores, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		known := map[string]bool{}
		for _, s := range d.Stores {
			if s.Scope == "environment" {
				known[s.Name] = true
			}
		}
		for _, s := range stores {
			if !s.IsDir() || !known[s.Name()] {
				return nil, fmt.Errorf("unrecognized environment store for %s", d.Name)
			}
		}
		definitions = append(definitions, d)
	}
	return definitions, nil
}

// Transfer has two durable phases: source-authoritative preparation, then
// destination-authoritative cleanup. A crash before the phase switch can only
// cause preparation to be retried; a crash after it can never repeat the copy.
func (e *Engine) Transfer(ctx context.Context, q TransferOptions) (result TransferResult, err error) {
	if q.Mode != "clone" && q.Mode != "relocate" {
		return result, fmt.Errorf("select clone or relocate")
	}
	slots := q.From != "" || q.To != ""
	if slots && (q.From == "" || q.To == "" || q.Destination != "" || q.Profile != "") {
		return result, fmt.Errorf("slot transfer requires --from and --to without destination folder or --profile")
	}
	if !slots && q.Destination == "" {
		return result, fmt.Errorf("provide a destination folder")
	}
	if q.Mode == "relocate" && q.Profile != "" {
		return result, fmt.Errorf("use --from/--to to change relocation slots")
	}
	sourceName, err := e.transferSource(ctx, q)
	if err != nil {
		return result, err
	}
	journal, err := e.Store.ReadTransfer(sourceName)
	if err != nil {
		return result, err
	}
	var sourceIdentity environment.Identity
	if journal != nil {
		sourceIdentity = journal.Source
	} else {
		source, err := e.Store.Read(ctx, sourceName)
		if err != nil {
			return result, err
		}
		sourceIdentity = source.Identity
	}
	destinationIdentity, err := e.transferDestination(q, sourceIdentity)
	if err != nil {
		return result, err
	}
	if sourceName == destinationIdentity.Name {
		return result, fmt.Errorf("source and destination are the same session")
	}
	names := []string{sourceName, destinationIdentity.Name}
	sort.Strings(names)
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	sourceLock, destLock := locks[0], locks[1]
	if sourceLock.Name != sourceName {
		sourceLock, destLock = destLock, sourceLock
	}
	journal, err = e.Store.ReadTransfer(sourceName)
	if err != nil {
		return result, err
	}
	if journal != nil && (journal.Mode != q.Mode || journal.Destination != destinationIdentity) {
		return result, fmt.Errorf("pending transfer has a different destination or mode")
	}
	if err = sourceLock.RequireIdle(); err != nil {
		return result, err
	}
	if err = destLock.RequireIdle(); err != nil {
		return result, err
	}
	if journal != nil && journal.Phase == "committed" {
		result = transferResult(*journal, q.DryRun)
		if q.DryRun {
			return result, nil
		}
		return result, e.finishTransfer(ctx, sourceLock, destLock, *journal)
	}
	source, err := sourceLock.ReadRecord(ctx)
	if err != nil {
		return result, err
	}
	if source.Identity != sourceIdentity {
		return result, fmt.Errorf("source identity changed during transfer selection")
	}
	c, exists, err := e.inspect(ctx, source)
	if err != nil {
		return result, err
	}
	if q.Mode == "clone" && exists && c.State.Running {
		return result, fmt.Errorf("clone requires a stopped or absent source container")
	}
	definitions, err := e.transferDefinitions(sourceLock, source, q.Mode)
	if err != nil {
		return result, err
	}
	if journal == nil {
		if err = sourceLock.RequireAvailable(); err != nil {
			return result, err
		}
		if err = destLock.RequireAvailable(); err != nil {
			return result, err
		}
		if _, err = destLock.ReadRecord(ctx); !os.IsNotExist(err) {
			if err == nil {
				err = fmt.Errorf("destination session already exists")
			}
			return result, err
		}
		if err = e.requireNew(ctx, destLock); err != nil {
			return result, err
		}
	} else if source.ID != journal.SourceID {
		return result, fmt.Errorf("source session identity differs from journal")
	}
	// Before the authority switch, retry always recopies the source. A bounded
	// rollback may have restarted it, so an earlier snapshot can no longer win.
	if journal != nil {
		if err = e.verifyReservation(destLock, *journal); err != nil {
			return result, err
		}
		destination, readErr := destLock.ReadRecord(ctx)
		if readErr == nil && destination.ID != journal.DestinationID {
			return result, fmt.Errorf("destination identity differs from journal")
		}
		if readErr != nil && !os.IsNotExist(readErr) {
			return result, readErr
		}
	}
	if destinationIdentity.Slot == "project" {
		p, pathErr := fsutil.Path(destinationIdentity.Workspace, ".devbox/config.json")
		if pathErr != nil {
			return result, pathErr
		}
		if _, pathErr = os.Stat(p); pathErr != nil {
			return result, fmt.Errorf("project destination must be initialized: %w", pathErr)
		}
	}
	spec, err := e.Resolve(Request{Workspace: destinationIdentity.Workspace, Profile: destinationIdentity.Profile, ExpectedName: destinationIdentity.Name})
	if err != nil {
		return result, err
	}
	if err = e.Docker.Network(ctx, spec.Settings.Network); err != nil {
		return result, err
	}
	if journal != nil && journal.Desired != spec.Fingerprints {
		return result, fmt.Errorf("destination inputs changed during pending transfer.\nRestore them before retrying the same transfer command.")
	}
	if journal == nil {
		nonce, idErr := fsutil.ID()
		if idErr != nil {
			return result, idErr
		}
		id := source.ID
		if q.Mode == "clone" {
			id, idErr = fsutil.ID()
			if idErr != nil {
				return result, idErr
			}
		}
		journal = &store.Transfer{Version: 1, ID: nonce, Mode: q.Mode, Phase: "prepare", Source: source.Identity, Destination: destinationIdentity, SourceID: source.ID, DestinationID: id, Running: q.Mode == "relocate" && exists && c.State.Running, Started: time.Now().UTC(), Desired: spec.Fingerprints}
	}
	result = transferResult(*journal, q.DryRun)
	if q.DryRun {
		return result, nil
	}
	if err = sourceLock.SaveTransfer(destLock, *journal); err != nil {
		return result, err
	}
	// Bounded failure handling restores availability of the original running
	// container, but keeps both endpoints guarded until the recorded retry.
	defer func() {
		if err != nil && journal.Phase == "prepare" {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = errors.Join(err, e.clearTransferAttempt(cleanup, destLock, *journal))
			if journal.Mode == "relocate" {
				err = errors.Join(err, e.Docker.Tag(cleanup, source.ImageID, source.ImageTag, e.Store.Installation))
			}
			if journal.Running && exists {
				live, found, inspectErr := e.inspect(cleanup, source)
				err = errors.Join(err, inspectErr)
				if inspectErr == nil && found && !live.State.Running {
					err = errors.Join(err, e.start(cleanup, live, source))
				}
			}
		}
		if err != nil {
			err = fmt.Errorf("%w\n\nRetry the same session %s command.\nSource: %s\nDestination: %s", err, journal.Mode, journal.Source.Name, journal.Destination.Name)
		}
	}()
	if exists && c.State.Running {
		if err = e.Docker.Stop(ctx, c, e.owner(source)); err != nil {
			return result, err
		}
	}
	if err = e.clearTransferAttempt(ctx, destLock, *journal); err != nil {
		return result, err
	}
	if err = sourceLock.CopyTransferState(destLock, *journal, definitions); err != nil {
		return result, err
	}
	created := journal.Started
	if q.Mode == "relocate" {
		created = source.Created
	}
	destination, _, err := e.createAs(ctx, destLock, spec, nil, false, journal.DestinationID, created)
	if err != nil {
		return result, err
	}
	if err = e.readyTransfer(ctx, sourceLock, destLock, journal, destination); err != nil {
		return result, err
	}
	return result, e.finishTransfer(ctx, sourceLock, destLock, *journal)
}
func transferResult(j store.Transfer, dryRun bool) TransferResult {
	return TransferResult{j.Source.Name, j.Destination.Name, j.Mode, j.DestinationID, dryRun}
}
func (e *Engine) verifyReservation(destination *store.Locked, j store.Transfer) error {
	pending, err := e.Store.Pending(destination.Name)
	if err != nil {
		return err
	}
	if pending == nil {
		return fmt.Errorf("destination reservation is missing; refusing to adopt destination state")
	}
	if pending.ID != j.ID || pending.Source != j.Source.Name {
		return fmt.Errorf("destination reservation differs from transfer")
	}
	return nil
}
func (e *Engine) clearTransferAttempt(ctx context.Context, destination *store.Locked, j store.Transfer) error {
	if err := e.verifyReservation(destination, j); err != nil {
		return err
	}
	c, exists, err := e.Docker.Inspect(ctx, j.Destination.Name)
	if err != nil {
		return err
	}
	record, readErr := destination.ReadRecord(ctx)
	if readErr == nil {
		if record.ID != j.DestinationID {
			return fmt.Errorf("destination identity differs from transfer")
		}
		if exists && (c.Image != record.ImageID || c.ID != record.SetupContainer) {
			return fmt.Errorf("destination instance differs from its record")
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	if exists {
		owner := docker.Owner{Installation: e.Store.Installation, Session: j.DestinationID, Workspace: j.Destination.Workspace, Slot: j.Destination.Slot}
		if err = c.Verify(owner); err != nil {
			return err
		}
		if c.State.Running {
			if err = e.Docker.Stop(ctx, c, owner); err != nil {
				return err
			}
		}
		if err = e.Docker.Remove(ctx, c, owner); err != nil {
			return err
		}
	}
	return destination.DeleteContext(ctx)
}
func (e *Engine) readyTransfer(ctx context.Context, source, destination *store.Locked, j *store.Transfer, r store.Record) error {
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("prepared destination container is missing")
	}
	if c.State.Running && !j.Running {
		if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return err
		}
	}
	if !c.State.Running && j.Running {
		if err = e.start(ctx, c, r); err != nil {
			return err
		}
	}
	r.Action = j.Mode
	r.Activity = time.Now().UTC()
	if err = destination.Save(r); err != nil {
		return err
	}
	// Publication can succeed even if the following directory sync fails.
	// After attempting this write, rollback must never destroy the destination.
	j.Phase = "committed"
	if err = source.SaveTransfer(destination, *j); err != nil {
		return err
	}
	return nil
}
func (e *Engine) finishTransfer(ctx context.Context, source, destination *store.Locked, j store.Transfer) error {
	r, err := destination.ReadRecord(ctx)
	if err != nil {
		return err
	}
	if r.ID != j.DestinationID || r.Identity != j.Destination {
		return fmt.Errorf("committed destination identity differs from journal")
	}
	live, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		live, err = e.recover(ctx, destination, &r, nil)
		if err != nil {
			return err
		}
		if !j.Running {
			if err = e.Docker.Stop(ctx, live, e.owner(r)); err != nil {
				return err
			}
		}
	}
	if j.Mode == "relocate" {
		original, readErr := source.ReadRecord(ctx)
		if readErr == nil {
			if original.ID != j.SourceID {
				return fmt.Errorf("source identity differs from journal")
			}
			c, exists, err := e.inspect(ctx, original)
			if err != nil {
				return err
			}
			if exists {
				if c.State.Running {
					if err = e.Docker.Stop(ctx, c, e.owner(original)); err != nil {
						return err
					}
				}
				if err = e.Docker.Remove(ctx, c, e.owner(original)); err != nil {
					return err
				}
			}
		} else if !os.IsNotExist(readErr) {
			return readErr
		} else {
			_, exists, inspectErr := e.Docker.Inspect(ctx, j.Source.Name)
			if inspectErr != nil {
				return inspectErr
			}
			if exists {
				return fmt.Errorf("source container exists without its transfer record; refusing state cleanup")
			}
		}
		if err = source.Delete(); err != nil {
			return err
		}
	}
	return source.FinishTransfer(destination, j)
}
