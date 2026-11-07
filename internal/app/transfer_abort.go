package app

import (
	"context"
	"fmt"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// Abort abandons source-authoritative preparation without resolving configs.
// The journal is released only after candidate cleanup and source restoration.
func (e *Engine) AbortTransfer(ctx context.Context, target string) (TransferResult, error) {
	var result TransferResult
	if !environment.IsSessionTarget(target) && !environment.IsSessionID(target) {
		return result, fmt.Errorf("--abort requires the exact public source session name")
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
	for i := range journals {
		j := &journals[i]
		if j.SourceName() != target && j.SourceID != target {
			continue
		}
		if selected != nil {
			return result, fmt.Errorf("conflicting transfer journals for source session")
		}
		selected = j
	}
	if selected == nil {
		return result, commanderror.New("transfer_missing", "No pending transfer for this source session.", target, nil)
	}
	j := *selected
	if j.Phase != "prepare" {
		return result, commanderror.New("transfer_committed", "The destination is already committed; finish cleanup instead of aborting.", j.SourceName(), nil, j.RetryStep())
	}
	locks, source, destination, err := e.lockTransfer(ctx, j.SourceID, j.DestinationID)
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	current, err := e.Store.ReadTransfer(j.SourceID)
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
	if r.ID != j.SourceID || r.Settings.Binding != j.Source || r.StateTree != j.SourceTree || r.Applied.ContainerID != j.SourceContainerID {
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
	if j.Mode == "clone" {
		if err := destination.DeleteContext(ctx); err != nil {
			return result, transferFailure(j, err)
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
