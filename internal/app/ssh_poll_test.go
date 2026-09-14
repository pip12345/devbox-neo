package app

import (
	"context"
	"testing"
	"time"

	"devbox/internal/docker"
)

type sshRunnerFunc func(context.Context, docker.Command) error

func (f sshRunnerFunc) Run(ctx context.Context, cmd docker.Command) error { return f(ctx, cmd) }

func TestSSHCancelDuringInspectionIsStillNormalDisconnect(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Attached = fakeContainerMaster(t, e, result.Name)
	ready := make(chan struct{})
	probing := make(chan context.Context, 1)
	resume := make(chan struct{})
	first := true
	e.Docker.Runner = sshRunnerFunc(func(ctx context.Context, cmd docker.Command) error {
		if len(cmd.Args) > 1 && cmd.Args[0] == "container" && cmd.Args[1] == "inspect" && first {
			select {
			case <-ready:
				first = false
				probing <- ctx
				<-resume
			default:
			}
		}
		return d.Run(ctx, cmd)
	})
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- e.SSH(cctx, result.Name, "", "staging", SSHOptions{Connected: func(_, _ string) { close(ready) }})
	}()
	var pollCtx context.Context
	select {
	case pollCtx = <-probing:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("no probe")
	}
	cancel()
	if pollCtx.Err() != nil {
		t.Error("terminal cancellation leaked into Docker inspection", pollCtx.Err())
	}
	close(resume)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("cancellation reported as failure", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("SSH did not finish")
	}
}
