package cli

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type interruptKey struct{}
type interrupts struct {
	mu        sync.Mutex
	operation context.CancelFunc
}

// SignalContext routes SIGINT to the current foreground operation when a menu
// owns the command. SIGTERM always cancels the whole command. Direct CLI calls
// have no operation scope and retain their existing process-cancellation rule.
func SignalContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	state := &interrupts{}
	ctx = context.WithValue(ctx, interruptKey{}, state)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case sig := <-signals:
				state.mu.Lock()
				if sig == os.Interrupt && state.operation != nil {
					state.operation()
				} else {
					cancel()
				}
				state.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	return ctx, func() { signal.Stop(signals); cancel(); <-done }
}
func operationContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	state, _ := parent.Value(interruptKey{}).(*interrupts)
	if state == nil {
		return ctx, cancel
	}
	state.mu.Lock()
	state.operation = cancel
	state.mu.Unlock()
	return ctx, func() { state.mu.Lock(); state.operation = nil; state.mu.Unlock(); cancel() }
}
