package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"devbox/internal/commanderror"
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
	LocalName   string
	As          string
	DryRun      bool
}
type TransferResult struct {
	Source          string             `json:"source"`
	Destination     string             `json:"destination"`
	Mode            string             `json:"mode"`
	SessionID       string             `json:"session_id"`
	DryRun          bool               `json:"dry_run"`
	Aborted         bool               `json:"aborted,omitempty"`
	Workspace       string             `json:"workspace"`
	LocalName       string             `json:"local_name"`
	Sources         []config.Reference `json:"sources"`
	ResolvedSources []config.Source    `json:"resolved_sources"`
}

func (e *Engine) transferSource(ctx context.Context, q TransferOptions) (name, id string, err error) {
	if q.LocalName != "" {
		if err := environment.ValidateLocalName(q.LocalName); err != nil {
			return "", "", err
		}
	}
	// A journal pins source identity even after source deletion or config edits.
	// Folder/default lookup must retain the ID too, not adopt a reused name.
	workspace, pathErr := filepath.Abs(q.Source)
	if pathErr != nil {
		return "", "", pathErr
	}
	if canonical, e := filepath.EvalSymlinks(workspace); e == nil {
		workspace = canonical
	}
	journals, listErr := e.Store.Transfers()
	if listErr != nil {
		return "", "", listErr
	}
	for _, j := range journals {
		if j.Source.Name == q.Source || j.SourceID == q.Source || (j.Source.Workspace == workspace && (q.LocalName == "" || j.Source.LocalName == q.LocalName)) {
			if name != "" {
				return "", "", fmt.Errorf("multiple pending transfers.\nUse the exact source name.")
			}
			name, id = j.Source.Name, j.SourceID
		}
	}
	if name != "" {
		return name, id, nil
	}
	r, err := e.Locate(ctx, q.Source, q.LocalName)
	return r.Directory, r.ID, err
}
func (e *Engine) transferDestination(q TransferOptions, source environment.Identity) (environment.Identity, error) {
	workspace := q.Destination
	if workspace == "" {
		workspace = source.Workspace
	}
	name := source.LocalName
	if q.As != "" {
		name = q.As
	}
	identity, err := environment.Identify(workspace, name)
	if err != nil {
		return identity, err
	}
	identity.Name, err = store.AllocateDirectory(identity.Binding)
	return identity, err
}
func (e *Engine) transferDefinitions(l *store.Locked, source store.Record, mode string) ([]harness.Definition, error) {
	if err := checkDurableStores(l, source); err != nil {
		return nil, err
	}
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
			return nil, fmt.Errorf("harness %s does not support %s", d.Name, store.TransferCommand(mode))
		}
		if d.Name == source.Applied.Definition.Name && environment.Fingerprint(e.Store.Installation, effective.Hash) != source.Applied.Definition.Hash {
			return nil, commanderror.New("harness_definition_changed", "Harness definition changed. Recreate before transferring.", source.Directory, nil,
				commanderror.Next("Recreate with current harness definition", "recreate", source.Directory))
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

func transferFailure(j store.Transfer, err error) error {
	steps := []commanderror.Step{j.RetryStep()}
	if j.Phase == "prepare" {
		steps = append(steps, commanderror.Next("Or abandon the uncommitted transfer", "copy", j.Source.Name, "--abort"))
	}
	return commanderror.New("transfer_failed", fmt.Sprintf("Session %s failed: %v", store.TransferCommand(j.Mode), err), j.Source.Name, err, steps...)
}

// Transfer has two durable phases: source-authoritative preparation, then
// destination-authoritative cleanup. A crash before the phase switch can only
// cause preparation to be retried; a crash after it can never repeat the copy.
func (e *Engine) Transfer(ctx context.Context, q TransferOptions) (result TransferResult, err error) {
	namesLock, err := e.Store.LockNames(ctx)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(namesLock)
	if q.Mode != "clone" && q.Mode != "relocate" {
		return result, fmt.Errorf("select clone or relocate")
	}
	if q.Destination == "" && q.As == "" {
		return result, fmt.Errorf("provide a destination folder or --as NAME")
	}
	if q.As != "" {
		if err := environment.ValidateLocalName(q.As); err != nil {
			return result, err
		}
	}
	sourceName, selectedID, err := e.transferSource(ctx, q)
	if err != nil {
		return result, err
	}
	journal, err := e.Store.ReadTransfer(sourceName)
	if err != nil {
		return result, err
	}
	var sourceIdentity environment.Identity
	var sourceID string
	if journal != nil {
		sourceIdentity, sourceID = journal.Source, journal.SourceID
	} else {
		source, err := e.readSession(ctx, sourceName)
		if err != nil {
			return result, err
		}
		sourceIdentity, sourceID = environment.Identity{Binding: source.Settings.Binding, Name: source.Directory}, source.ID
	}
	if selectedID != "" && sourceID != selectedID {
		return result, commanderror.New("session_changed", "Selected source session was replaced; select it again.", sourceName, nil)
	}
	if q.LocalName != "" && sourceIdentity.LocalName != q.LocalName {
		return result, fmt.Errorf("selection does not match the source session")
	}
	var destinationIdentity environment.Identity
	if journal != nil {
		destinationIdentity = journal.Destination
		workspace := sourceIdentity.Workspace
		if q.Destination != "" {
			workspace = q.Destination
		}
		canonical, pathErr := config.CanonicalPath(workspace)
		if pathErr != nil {
			return result, pathErr
		}
		name := sourceIdentity.LocalName
		if q.As != "" {
			name = q.As
		}
		if canonical != destinationIdentity.Workspace || name != destinationIdentity.LocalName {
			return result, fmt.Errorf("pending transfer has a different destination or selection")
		}
	} else {
		destinationIdentity, err = e.transferDestination(q, sourceIdentity)
		if err != nil {
			return result, err
		}
	}
	if sourceIdentity.Binding == destinationIdentity.Binding {
		return result, fmt.Errorf("source and destination are the same session; choose --as NAME or another destination folder")
	}
	if journal == nil {
		if err := e.Store.RequireUnusedBinding(ctx, destinationIdentity.Binding, ""); err != nil {
			return result, err
		}
	}
	destinationID := sourceID
	if journal != nil {
		destinationID = journal.DestinationID
	} else if q.Mode == "clone" {
		destinationID, err = fsutil.ID()
		if err != nil {
			return result, err
		}
	}
	names := []string{sourceName, destinationIdentity.Name}
	sort.Strings(names)
	locks, err := e.Store.LockAll(ctx, names, map[string]string{sourceName: sourceID, destinationIdentity.Name: destinationID})
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
		committed, readErr := destLock.ReadRecord(ctx)
		if readErr != nil {
			return result, readErr
		}
		if committed.ID != journal.DestinationID || committed.Settings.Binding != journal.Destination.Binding {
			return result, fmt.Errorf("committed destination identity differs from journal")
		}
		result.Sources, result.ResolvedSources = committed.Settings.Sources, committed.Applied.Inputs.Sources
		if q.DryRun {
			return result, nil
		}
		err = e.finishTransfer(ctx, sourceLock, destLock, *journal)
		if err != nil {
			err = transferFailure(*journal, err)
		}
		return result, err
	}
	source, err := sourceLock.ReadRecord(ctx)
	if err != nil {
		return result, err
	}
	if source.Settings.Binding != sourceIdentity.Binding || source.ID != sourceID || (journal != nil && source.Applied.SetupContainer != journal.SourceContainerID) {
		return result, fmt.Errorf("source identity changed during transfer selection")
	}
	c, exists, err := e.inspect(ctx, source)
	if err != nil {
		return result, err
	}
	if q.Mode == "clone" && exists && c.State.Running {
		return result, commanderror.New("container_running", "Stop the source container before copying.", source.Directory, nil,
			commanderror.Next("Stop, then retry copy", "stop", source.Directory))
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
	// Before commitment the source remains authoritative. Retry resolves current
	// config and recopies the source after clearing the owned destination attempt;
	// only endpoints and intent stay pinned, not the inputs being repaired.
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
	spec, err := e.Resolve(Request{Workspace: destinationIdentity.Workspace, LocalName: destinationIdentity.LocalName, Sources: source.Settings.Sources})
	if err != nil {
		return result, err
	}
	if spec.Identity.Binding != destinationIdentity.Binding {
		return result, fmt.Errorf("destination selection changed during transfer")
	}
	if err = e.Docker.Network(ctx, spec.Settings.Network); err != nil {
		return result, err
	}
	if journal == nil {
		nonce, idErr := fsutil.ID()
		if idErr != nil {
			return result, idErr
		}
		journal = &store.Transfer{Version: 4, ContainerName: environment.ResourceName(destinationIdentity.Workspace, destinationIdentity.LocalName, nonce), ID: nonce, Mode: q.Mode, Phase: "prepare", Source: sourceIdentity, SourceContainerID: source.Applied.SetupContainer, Destination: destinationIdentity, SourceID: source.ID, DestinationID: destinationID, Running: q.Mode == "relocate" && (source.Settings.ManualStart || (exists && c.State.Running)), ManualStart: q.Mode == "relocate" && source.Settings.ManualStart, Started: time.Now().UTC()}
	}
	result = transferResult(*journal, q.DryRun)
	result.Sources, result.ResolvedSources = spec.Sources, spec.ResolvedSources
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
				available, imageErr := e.Docker.ImageAvailable(cleanup, source.Applied.ImageID)
				err = errors.Join(err, imageErr)
				// Restore a moved tag only while its image still exists. Pruning
				// runtime does not invalidate source-authoritative saved stores.
				if imageErr == nil && available {
					err = errors.Join(err, e.Docker.Tag(cleanup, source.Applied.ImageID, source.Applied.ImageTag, e.Store.Installation))
				}
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
			err = transferFailure(*journal, err)
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
	destination, _, err := e.CreatePrepared(ctx, destLock, spec, CreationIdentity{ContainerName: journal.ContainerName, ID: journal.DestinationID, Created: created, ManualStart: journal.ManualStart})
	if err != nil {
		return result, err
	}
	if err = e.readyTransfer(ctx, sourceLock, destLock, journal, destination); err != nil {
		return result, err
	}
	return result, e.finishTransfer(ctx, sourceLock, destLock, *journal)
}
func transferResult(j store.Transfer, dryRun bool) TransferResult {
	return TransferResult{Source: j.Source.Name, Destination: j.Destination.Name, Mode: j.Mode, SessionID: j.DestinationID, DryRun: dryRun, Workspace: j.Destination.Workspace, LocalName: j.Destination.LocalName}
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
	record, readErr := destination.ReadRecord(ctx)
	var c docker.Container
	var exists bool
	var err error
	if readErr == nil {
		if record.ID != j.DestinationID {
			return fmt.Errorf("destination identity differs from transfer")
		}
		// A recorded instance remains authoritative after a Docker rename.
		c, exists, err = e.inspect(ctx, record)
	} else if os.IsNotExist(readErr) {
		c, exists, err = e.Docker.Inspect(ctx, j.ContainerName)
		if err != nil {
			return err
		}
		live, inventoryErr := e.Docker.Inventory(ctx, e.Store.Installation)
		if inventoryErr != nil {
			return inventoryErr
		}
		for _, other := range live {
			if other.Config.Labels[docker.Namespace+".session"] == j.DestinationID && other.ID != j.SourceContainerID && other.ID != c.ID {
				return fmt.Errorf("unrecorded destination container exists under another name; refusing to remove its backing state")
			}
		}
	} else {
		return readErr
	}
	if err != nil {
		return err
	}
	if exists {
		owner := docker.Owner{Installation: e.Store.Installation, Session: j.DestinationID, Workspace: j.Destination.Workspace, LocalName: j.Destination.LocalName}
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
	r.Action = store.TransferCommand(j.Mode)
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
	if r.ID != j.DestinationID || r.Settings.Binding != j.Destination.Binding {
		return fmt.Errorf("committed destination identity differs from journal")
	}
	if err := checkDurableStores(destination, r); err != nil {
		return err
	}
	// Commitment makes destination stores authoritative, not its disposable
	// container. Finish cleanup even if runtime was removed; only an explicit
	// recreate may replace it after the journal releases the endpoints.
	if _, _, err := e.inspect(ctx, r); err != nil {
		return err
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
			_, exists, inspectErr := e.Docker.InspectID(ctx, j.SourceContainerID)
			if inspectErr != nil {
				return inspectErr
			}
			if exists {
				return fmt.Errorf("source container exists without its transfer record; refusing state cleanup")
			}
		}
		if err = removeSavedSession(ctx, source, j.Source.Workspace, j.SourceID); err != nil {
			return err
		}
	}
	return source.FinishTransfer(destination, j)
}
