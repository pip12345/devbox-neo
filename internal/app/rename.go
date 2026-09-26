package app

import (
	"context"

	"devbox/internal/environment"
)

// Rename is a same-workspace relocation, not an in-place Docker rename. The
// transfer transaction owns replacement, state preservation, and retry safety.
func (e *Engine) Rename(ctx context.Context, target, localName, to string, dryRun bool) (TransferResult, error) {
	if err := environment.ValidateLocalName(to); err != nil {
		return TransferResult{}, err
	}
	return e.Transfer(ctx, TransferOptions{Source: target, LocalName: localName, As: to, Mode: "relocate", DryRun: dryRun})
}
