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
	"devbox/internal/sshshare"
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
	Warnings        []string           `json:"-"`
}

func (e *Engine) transferSource(ctx context.Context, q TransferOptions) (string, error) {
	if q.LocalName != "" {
		if err := environment.ValidateLocalName(q.LocalName); err != nil {
			return "", err
		}
	}
	workspace, err := config.CanonicalPath(q.Source)
	if err != nil {
		return "", err
	}
	journals, err := e.Store.Transfers()
	if err != nil {
		return "", err
	}
	selected := ""
	for _, j := range journals {
		if q.Source != j.SourceName() && q.Source != j.SourceID &&
			!(workspace == j.Source.Workspace && (q.LocalName == "" || q.LocalName == j.Source.LocalName)) {
			continue
		}
		if selected != "" {
			return "", fmt.Errorf("multiple pending transfers; use the exact public source name")
		}
		selected = j.SourceID
	}
	if selected != "" {
		return selected, nil
	}
	r, err := e.Locate(ctx, q.Source, q.LocalName)
	return r.ID, err
}

func (e *Engine) transferDefinitions(l *store.Locked, source store.Record, mode string) ([]harness.Definition, error) {
	if err := checkDurableStores(l, source); err != nil {
		return nil, err
	}
	root, err := l.TreePath(source.StateTree, "harnesses")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var definitions []harness.Definition
	for _, entry := range entries {
		if !entry.IsDir() || !config.Name.MatchString(entry.Name()) {
			return nil, fmt.Errorf("invalid harness state directory")
		}
		effective, err := harness.Load(e.Store.Home, entry.Name())
		if err != nil {
			return nil, err
		}
		d := effective.Definition
		if mode == "clone" && !d.Session.Clone || mode == "relocate" && !d.Session.Relocate {
			return nil, fmt.Errorf("harness %s does not support %s", d.Name, store.TransferCommand(mode))
		}
		if d.Name == source.Applied.Definition.Name && environment.Fingerprint(e.Store.Installation, effective.Hash) != source.Applied.Definition.Hash {
			return nil, commanderror.New("harness_definition_changed", "Harness definition changed. Recreate before transferring.", source.PublicName(), nil,
				commanderror.Next("Recreate with current harness definition", "recreate", source.PublicName()))
		}
		path, err := l.TreePath(source.StateTree, filepath.Join("harnesses", d.Name, "stores"))
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
		steps = append(steps, commanderror.Next("Or abandon the uncommitted transfer", "copy", j.SourceName(), "--abort"))
	}
	return commanderror.New("transfer_failed", fmt.Sprintf("Session %s failed: %v", store.TransferCommand(j.Mode), err), j.SourceName(), err, steps...)
}

func (e *Engine) lockTransfer(ctx context.Context, sourceID, destinationID string) ([]*store.Locked, *store.Locked, *store.Locked, error) {
	ids := []string{sourceID}
	if sourceID != destinationID {
		ids = append(ids, destinationID)
	}
	sort.Strings(ids)
	locks, err := e.Store.LockAll(ctx, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	var source, destination *store.Locked
	for _, lock := range locks {
		if lock.ID == sourceID {
			source = lock
		}
		if lock.ID == destinationID {
			destination = lock
		}
	}
	return locks, source, destination, nil
}

// Preparation never changes the source record or its data tree. Commitment
// publishes journal authority before session.json; a committed retry only
// publishes the prepared record and finishes cleanup, never copies again.
func (e *Engine) Transfer(ctx context.Context, q TransferOptions) (result TransferResult, err error) {
	names, err := e.Store.LockNames(ctx)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(names)
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
	sourceID, err := e.transferSource(ctx, q)
	if err != nil {
		return result, err
	}
	journal, err := e.Store.ReadTransfer(sourceID)
	if err != nil {
		return result, err
	}
	var source store.Record
	var sourceBinding environment.Binding
	if journal != nil {
		sourceBinding = journal.Source
	} else {
		source, err = e.Store.Read(ctx, sourceID)
		if err != nil {
			return result, err
		}
		sourceBinding = source.Settings.Binding
	}
	if q.LocalName != "" && q.LocalName != sourceBinding.LocalName {
		return result, fmt.Errorf("selection does not match the source session")
	}
	workspace := q.Destination
	if workspace == "" {
		workspace = sourceBinding.Workspace
	}
	localName := sourceBinding.LocalName
	if q.As != "" {
		localName = q.As
	}
	var destination environment.Binding
	if journal != nil {
		canonical, err := config.CanonicalPath(workspace)
		if err != nil {
			return result, err
		}
		destination = environment.Binding{Workspace: canonical, LocalName: localName}
		if journal.Mode != q.Mode || destination != journal.Destination {
			return result, fmt.Errorf("pending transfer has a different mode or destination")
		}
	} else {
		identity, err := environment.Identify(workspace, localName)
		if err != nil {
			return result, err
		}
		destination = identity.Binding
		if destination == sourceBinding {
			return result, fmt.Errorf("source and destination are the same session; choose --as NAME or another destination folder")
		}
		if err := e.Store.RequireUnusedBinding(ctx, destination, ""); err != nil {
			return result, err
		}
	}
	destinationID := sourceID
	if journal != nil {
		destinationID = journal.DestinationID
	} else if q.Mode == "clone" {
		destinationID, err = fsutil.SessionID()
		if err != nil {
			return result, err
		}
	}
	locks, sourceLock, destLock, err := e.lockTransfer(ctx, sourceID, destinationID)
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	for _, lock := range locks {
		if err := lock.RequireIdle(); err != nil {
			return result, err
		}
	}
	if journal != nil && journal.Phase == "committed" {
		result = transferResult(*journal, q.DryRun)
		r, err := e.Store.ReadTransferDestination(ctx, *journal)
		if err != nil {
			return result, err
		}
		result.Sources, result.ResolvedSources = r.Settings.Sources, r.Applied.Inputs.Sources
		if q.DryRun {
			return result, nil
		}
		if err := e.finishTransfer(ctx, sourceLock, destLock, *journal); err != nil {
			return result, transferFailure(*journal, err)
		}
		return result, nil
	}
	source, err = sourceLock.ReadRecord(ctx)
	if err != nil {
		return result, err
	}
	if source.ID != sourceID || source.Settings.Binding != sourceBinding || journal != nil &&
		(source.StateTree != journal.SourceTree || source.Applied.ContainerID != journal.SourceContainerID || source.Applied.ImageID != journal.SourceImageID) {
		return result, fmt.Errorf("source identity changed during transfer selection")
	}
	c, exists, err := e.inspect(ctx, source)
	if err != nil {
		return result, err
	}
	if q.Mode == "clone" && exists && c.State.Running {
		return result, commanderror.New("container_running", "Stop the source container before copying.", source.PublicName(), nil,
			commanderror.Next("Stop, then retry copy", "stop", source.PublicName()))
	}
	definitions, err := e.transferDefinitions(sourceLock, source, q.Mode)
	if err != nil {
		return result, err
	}
	spec, err := e.Resolve(ResolveRequest{Workspace: destination.Workspace, LocalName: destination.LocalName, Sources: source.Settings.Sources})
	result.Warnings = spec.Warnings
	if !q.DryRun {
		e.reportWarnings(spec.Warnings)
	}
	if err != nil {
		return result, err
	}
	if err := e.Docker.Network(ctx, spec.Settings.Network); err != nil {
		return result, err
	}
	if journal == nil {
		op, err := fsutil.ID()
		if err != nil {
			return result, err
		}
		tree := store.InitialStateTree
		if q.Mode == "relocate" {
			tree = "data/" + op
		}
		journal = &store.Transfer{Version: 5, ID: op, Mode: q.Mode, Phase: "prepare", Source: sourceBinding, Destination: destination,
			SourceID: sourceID, DestinationID: destinationID, SourceTree: source.StateTree, DestinationTree: tree,
			SourceContainerID: source.Applied.ContainerID, SourceImageID: source.Applied.ImageID, SourceImageTag: source.Applied.ImageTag,
			ContainerName: environment.ResourceName(destination, destinationID, op),
			Running:       q.Mode == "relocate" && (source.Settings.ManualStart || exists && c.State.Running),
			ManualStart:   q.Mode == "relocate" && source.Settings.ManualStart, Started: time.Now().UTC()}
	}
	result = transferResult(*journal, q.DryRun)
	result.Warnings, result.Sources, result.ResolvedSources = spec.Warnings, spec.Sources, spec.ResolvedSources
	if q.DryRun {
		return result, nil
	}
	current, err := e.Store.ReadTransfer(sourceID)
	if err != nil {
		return result, err
	}
	if current == nil {
		if q.Mode == "clone" {
			if err := destLock.Claim(); err != nil {
				return result, fmt.Errorf("cannot allocate destination directory: %w", err)
			}
		} else {
			if _, err := destLock.Dir("data"); err != nil {
				return result, err
			}
			path, err := destLock.TreePath(journal.DestinationTree, ".")
			if err != nil {
				return result, err
			}
			if err := os.Mkdir(path, 0700); err != nil {
				return result, fmt.Errorf("cannot allocate destination tree: %w", err)
			}
		}
		if err := sourceLock.SaveTransfer(destLock, *journal); err != nil {
			return result, err
		}
	} else if *current != *journal {
		return result, fmt.Errorf("transfer changed; retry selection")
	}
	defer func() {
		if err != nil && journal.Phase == "prepare" {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = errors.Join(err, e.clearTransferAttempt(cleanup, destLock, *journal))
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
	if journal.Mode == "relocate" {
		// The fixed SSH mount is transient, not portable state. Idleness excludes
		// live sharing; clear stale entries in place without changing its inode.
		root, rootErr := destLock.Path(sshshare.RelativeRoot)
		if rootErr != nil {
			return result, rootErr
		}
		entries, readErr := os.ReadDir(root)
		if readErr != nil && !os.IsNotExist(readErr) {
			return result, readErr
		}
		for _, entry := range entries {
			if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return result, err
			}
		}
	}
	if _, err = destLock.TreeDir(journal.DestinationTree, "."); err != nil {
		return result, err
	}
	if err = sourceLock.CopyTransferState(destLock, *journal, definitions); err != nil {
		return result, err
	}
	created := journal.Started
	if q.Mode == "relocate" {
		created = source.Created
	}
	r, _, err := e.CreatePrepared(ctx, destLock, spec, CreationIdentity{ID: destinationID, Created: created, ManualStart: journal.ManualStart,
		Attempt: journal.ID, StateTree: journal.DestinationTree, Transfer: journal})
	if err != nil {
		return result, err
	}
	if err = e.readyTransfer(ctx, sourceLock, destLock, journal, r); err != nil {
		return result, err
	}
	return result, e.finishTransfer(ctx, sourceLock, destLock, *journal)
}

func transferResult(j store.Transfer, dryRun bool) TransferResult {
	return TransferResult{Source: j.SourceName(), Destination: j.DestinationName(), Mode: j.Mode, SessionID: j.DestinationID,
		DryRun: dryRun, Workspace: j.Destination.Workspace, LocalName: j.Destination.LocalName}
}

func (e *Engine) verifyReservation(destination *store.Locked, j store.Transfer) error {
	current, err := e.Store.ReadTransfer(j.SourceID)
	if err != nil {
		return err
	}
	if current == nil || *current != j || destination.ID != j.DestinationID {
		return fmt.Errorf("destination reservation differs from transfer")
	}
	return nil
}

func (e *Engine) clearTransferAttempt(ctx context.Context, destination *store.Locked, j store.Transfer) error {
	if err := e.verifyReservation(destination, j); err != nil {
		return err
	}
	if j.Phase != "prepare" {
		return fmt.Errorf("cannot clear committed destination")
	}
	if current, err := destination.ReadRecord(ctx); err == nil {
		if j.Mode == "clone" || current.StateTree != j.SourceTree || current.Settings.Binding != j.Source || current.Applied.ContainerID != j.SourceContainerID {
			return fmt.Errorf("canonical session differs from source; refusing attempt cleanup")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	r, readErr := e.Store.ReadTransferDestination(ctx, j)
	var c docker.Container
	var exists bool
	var err error
	if readErr == nil {
		c, exists, err = e.inspect(ctx, r)
	} else if os.IsNotExist(readErr) {
		c, exists, err = e.Docker.Inspect(ctx, j.ContainerName)
	} else {
		return readErr
	}
	if err != nil {
		return err
	}
	live, err := e.Docker.Inventory(ctx, e.Store.Installation)
	if err != nil {
		return err
	}
	for _, other := range live {
		if other.Config.Labels[docker.Namespace+".session"] == j.DestinationID && other.ID != j.SourceContainerID && (!exists || other.ID != c.ID) {
			return fmt.Errorf("unrecorded destination container exists; refusing backing-state cleanup")
		}
	}
	if exists {
		owner := docker.Owner{Installation: e.Store.Installation, Session: j.DestinationID}
		if c.ID == j.SourceContainerID {
			return fmt.Errorf("destination name refers to the source instance")
		}
		if err := c.Verify(owner); err != nil {
			return err
		}
		if c.State.Running {
			if err := e.Docker.Stop(ctx, c, owner); err != nil {
				return err
			}
		}
		if err := e.Docker.Remove(ctx, c, owner); err != nil {
			return err
		}
	}
	if readErr == nil && r.Applied.ImageTag != j.SourceImageTag {
		image, tagged, err := e.Docker.TaggedImage(ctx, r.Applied.ImageTag)
		if err != nil {
			return err
		}
		if tagged {
			if image.ID != r.Applied.ImageID {
				return fmt.Errorf("candidate image alias changed before cleanup")
			}
			if err := e.Docker.Untag(ctx, r.Applied.ImageTag, image.ID, e.Store.Installation); err != nil {
				return err
			}
		}
	}
	path, err := destination.TreePath(j.DestinationTree, ".")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return destination.RemoveTransferCandidate(j)
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
		if err := e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return err
		}
	}
	if !c.State.Running && j.Running {
		if err := e.start(ctx, c, r); err != nil {
			return err
		}
	}
	r.Action, r.Activity = store.TransferCommand(j.Mode), time.Now().UTC()
	if err := destination.SaveTransferDestination(*j, r); err != nil {
		return err
	}
	// A sync error may follow a successful journal rename. Once publication is
	// attempted, keep the candidate and let recovery determine durable authority.
	j.Phase, j.DestinationContainerID = "committed", r.Applied.ContainerID
	return source.SaveTransfer(destination, *j)
}

func (e *Engine) finishTransfer(ctx context.Context, source, destination *store.Locked, j store.Transfer) error {
	if err := e.verifyReservation(destination, j); err != nil {
		return err
	}
	prepared, err := e.Store.ReadTransferDestination(ctx, j)
	if err != nil {
		return err
	}
	if err := checkDurableStores(destination, prepared); err != nil {
		return err
	}
	if _, _, err := e.inspect(ctx, prepared); err != nil {
		return err
	}
	current, readErr := destination.ReadRecord(ctx)
	published := readErr == nil && current.ID == j.DestinationID && current.StateTree == j.DestinationTree &&
		current.Settings.Binding == j.Destination && current.Applied.ContainerID == j.DestinationContainerID
	if !published {
		if readErr != nil && !os.IsNotExist(readErr) || readErr == nil && (j.Mode == "clone" || current.StateTree != j.SourceTree || current.Settings.Binding != j.Source || current.Applied.ContainerID != j.SourceContainerID) {
			return fmt.Errorf("canonical session changed before destination publication")
		}
		if err := destination.Save(prepared); err != nil {
			return err
		}
	}
	if j.Mode == "relocate" {
		old, exists, err := e.Docker.InspectID(ctx, j.SourceContainerID)
		if err != nil {
			return err
		}
		if exists {
			owner := docker.Owner{Installation: e.Store.Installation, Session: j.SourceID}
			if old.Image != j.SourceImageID {
				return fmt.Errorf("source image differs from pinned instance")
			}
			if err := old.Verify(owner); err != nil {
				return err
			}
			if old.State.Running {
				if err := e.Docker.Stop(ctx, old, owner); err != nil {
					return err
				}
			}
			if err := e.Docker.Remove(ctx, old, owner); err != nil {
				return err
			}
		}
		if err := source.ClearMatchingDefault(ctx, j.Source.Workspace, j.SourceID); err != nil {
			return err
		}
		oldTree, err := source.TreePath(j.SourceTree, ".")
		if err != nil {
			return err
		}
		if err := os.RemoveAll(oldTree); err != nil {
			return err
		}
		if j.SourceImageTag != prepared.Applied.ImageTag {
			image, tagged, err := e.Docker.TaggedImage(ctx, j.SourceImageTag)
			if err != nil {
				return err
			}
			if tagged {
				if image.ID != j.SourceImageID {
					return fmt.Errorf("source image alias changed")
				}
				if err := e.Docker.Untag(ctx, j.SourceImageTag, j.SourceImageID, e.Store.Installation); err != nil {
					return err
				}
			}
		}
	}
	return source.FinishTransfer(destination, j)
}
