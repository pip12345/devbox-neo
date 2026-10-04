package app

import (
	"context"
	"fmt"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// AbortTransfer abandons only source-authoritative preparation. It never reads
// desired configuration: an unbuildable destination is a reason to abort, not a
// prerequisite for doing so. The journal stays until every cleanup step succeeds.
func (e *Engine) AbortTransfer(ctx context.Context, sourceID string) (TransferResult, error) {
	var result TransferResult
	if !environment.IsSessionTarget(sourceID) && !environment.IsSessionID(sourceID) {
		return result, fmt.Errorf("--abort requires the exact source session directory name")
	}
	names, err := e.Store.LockNames(ctx)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(names)
	journals, err := e.Store.Transfers()
	if err != nil {
		return result, err
	}
	var selected *store.Transfer
	for _, j := range journals {
		if j.Source.Name != sourceID && j.SourceID != sourceID {
			continue
		}
		if selected != nil {
			return result, fmt.Errorf("conflicting transfer journals for source session")
		}
		selected = &j
	}
	if selected == nil {
		return result, commanderror.New("transfer_missing", "No pending transfer for this source session.", sourceID, nil)
	}
	j := *selected
	if j.Phase != "prepare" {
		return result, commanderror.New("transfer_committed", "The destination is already committed; finish cleanup instead of aborting.", j.Source.Name, nil, j.RetryStep())
	}
	locks, err := e.Store.LockAll(ctx, []string{j.Source.Name, j.Destination.Name}, map[string]string{j.Source.Name: j.SourceID, j.Destination.Name: j.DestinationID})
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	source, destination := locks[0], locks[1]
	if source.Name != j.Source.Name {
		source, destination = destination, source
	}
	current, err := e.Store.ReadTransfer(j.Source.Name)
	if err != nil {
		return result, err
	}
	if current == nil || *current != j {
		return result, fmt.Errorf("transfer changed; inspect it before aborting")
	}
	for _, lock := range locks {
		if err := lock.RequireIdle(); err != nil {
			return result, err
		}
	}
	r, err := source.ReadRecord(ctx)
	if err != nil {
		return result, err
	}
	if r.ID != j.SourceID || r.Settings.Binding != j.Source.Binding || r.Applied.SetupContainer != j.SourceContainerID {
		return result, fmt.Errorf("source identity differs from the pending transfer")
	}
	if err := checkDurableStores(source, r); err != nil {
		return result, err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return result, err
	}
	if err := e.clearTransferAttempt(ctx, destination, j); err != nil {
		return result, transferFailure(j, err)
	}
	if j.Mode == "relocate" {
		available, err := e.Docker.ImageAvailable(ctx, r.Applied.ImageID)
		if err != nil {
			return result, transferFailure(j, err)
		}
		if available {
			if err := e.Docker.Tag(ctx, r.Applied.ImageID, r.Applied.ImageTag, e.Store.Installation); err != nil {
				return result, transferFailure(j, err)
			}
		}
	}
	if j.Running && exists && !c.State.Running {
		if err := e.start(ctx, c, r); err != nil {
			return result, transferFailure(j, err)
		}
	}
	if err := source.AbortTransfer(destination, j); err != nil {
		return result, transferFailure(j, err)
	}
	result = transferResult(j, false)
	result.Aborted, result.SessionID = true, j.SourceID
	return result, nil
}
