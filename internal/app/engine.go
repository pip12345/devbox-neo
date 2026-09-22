// Package app orders lifecycle transitions. Resolution finishes before Docker mutation.
package app

import (
	"context"
	"fmt"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

type Engine struct {
	Store       *store.Store
	Docker      docker.Runtime
	Streams     docker.Streams
	TerminalEnv []string
	UID         int
	GID         int
	// OnDiagnostic runs synchronously at the reporting point, possibly under the
	// operation lock. It must not reenter session operations or mutate diagnostic
	// slices. Nil suppresses delivery, not collection in Result.Diagnostics.
	OnDiagnostic func(Diagnostic)
}
type Request struct {
	Workspace   string
	LocalName   string
	HarnessArgs []string
	Sources     []config.Reference
	Recorded    *environment.Identity
	Continue    bool
	Args        []string
	Host        config.Host
}
type Diagnostic struct {
	Code                string
	Message             string
	Command             []string
	Change              environment.Change
	PendingInputChanges []environment.InputChange
}
type Result struct {
	Name        string
	Diagnostics []Diagnostic
}

func (e *Engine) resolveSpec(q Request) (environment.Spec, error) {
	return environment.Resolve(environment.Request{Home: e.Store.Home, Workspace: q.Workspace, LocalName: q.LocalName, Sources: q.Sources, Recorded: q.Recorded, UID: e.UID, GID: e.GID, Salt: e.Store.Installation, Host: q.Host})
}
func (e *Engine) Resolve(q Request) (environment.Spec, error) {
	spec, err := e.resolveSpec(q)
	e.resolutionWarnings(spec)
	return spec, err
}
func (e *Engine) resolutionWarnings(spec environment.Spec) {
	if e.Streams.Err != nil {
		for _, warning := range spec.Warnings {
			fmt.Fprintf(e.Streams.Err, "Warning: %s\n", warning)
		}
	}
}
func (e *Engine) diagnose(result *Result, diagnostic Diagnostic) {
	result.Diagnostics = append(result.Diagnostics, diagnostic)
	if e.OnDiagnostic != nil {
		e.OnDiagnostic(diagnostic)
	}
}
func (e *Engine) owner(r store.Record) docker.Owner {
	return docker.Owner{Installation: e.Store.Installation, Session: r.ID, Workspace: r.Identity.Workspace, LocalName: r.Identity.LocalName}
}
func (e *Engine) inspect(ctx context.Context, r store.Record) (docker.Container, bool, error) {
	c, exists, err := e.Docker.Inspect(ctx, r.Identity.Name)
	if err == nil && exists {
		err = c.Verify(e.owner(r))
		if err == nil && (c.Image != r.ImageID || (r.SetupContainer != "" && c.ID != r.SetupContainer)) {
			err = commanderror.New("container_mismatch", "Container identity does not match this session.", r.Identity.Name, nil)
		}
	}
	return c, exists, err
}
