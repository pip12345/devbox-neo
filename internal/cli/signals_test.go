package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

// Use subprocesses because an unhandled hangup must fail the regression without
// terminating the test runner. The fake Docker attachment still uses real leases
// and the engine's normal cancellation cleanup.
func TestHangupCleansUpAttachment(t *testing.T) {
	for _, tt := range []struct {
		name   string
		scoped bool
		manual bool
	}{
		{name: "direct_automatic"},
		{name: "foreground_automatic", scoped: true},
		{name: "direct_manual", manual: true},
		{name: "foreground_manual", scoped: true, manual: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getenv("DEVBOX_TEST_HANGUP_HELPER") != "1" {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHangupCleansUpAttachment$/^"+tt.name+"$", "-test.timeout=15s")
				cmd.Env = append(os.Environ(), "DEVBOX_TEST_HANGUP_HELPER=1")
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("hangup helper failed: %v\n%s", err, output)
				}
				return
			}

			e, q, name := namedCLIFixture(t)
			if tt.manual {
				if _, err := e.Start(context.Background(), name, ""); err != nil {
					t.Fatal(err)
				}
			}
			parent, timeout := context.WithTimeout(context.Background(), 10*time.Second)
			defer timeout()
			root, stop := SignalContext(parent)
			defer stop()
			ctx := root
			if tt.scoped {
				var finish func()
				ctx, finish = operationContext(root)
				defer finish()
			}
			e.Streams.In = strings.NewReader("")
			attached := false
			e.Docker.Runner.(*dockertest.Daemon).Attached = func(ctx context.Context, c docker.Command) error {
				if c.Stdin == nil {
					return nil
				}
				attached = true
				if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
					return err
				}
				<-ctx.Done()
				return ctx.Err()
			}
			_, err := e.Open(ctx, q)
			if !attached || !errors.Is(err, context.Canceled) || root.Err() != context.Canceled || parent.Err() != nil {
				t.Fatal("hangup did not cancel the whole command", attached, err, root.Err(), parent.Err())
			}
			details, err := e.Status(context.Background(), name, "")
			if err != nil || len(details.Active) != 0 || details.Running != tt.manual || details.ManualStart != tt.manual {
				t.Fatal("hangup changed attachment cleanup or manual-start intent", details, err)
			}
		})
	}
}
