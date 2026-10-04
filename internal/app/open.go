package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/store"
)

func creationRequired(workspace, localName string, cause error) error {
	message := "No session exists."
	if localName != "" {
		message = fmt.Sprintf("No session named %q exists for this folder.", localName)
	}
	return commanderror.New("session_missing", message, workspace, cause,
		commanderror.Next("Create a session", "create", workspace))
}

// Open keeps the operation lock through recorded startup and lease creation.
// The long foreground command runs after releasing it.
func (e *Engine) Open(ctx context.Context, q Request) (result Result, err error) {
	invocationArgs := append([]string(nil), q.HarnessArgs...)
	r, err := e.Locate(ctx, q.Workspace, q.LocalName)
	if err != nil {
		return result, err
	}
	result.Session, result.SessionID = r.Directory, r.ID
	if _, err = store.ProcessIdentity(os.Getpid()); err != nil {
		return result, err
	}
	lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	record, err := loadSelected(lock, r)
	if err != nil {
		return result, err
	}
	var c docker.Container
	started := false
	defer func() {
		if err != nil && started && lock.Held() {
			err = errors.Join(err, e.stopUnattached(lock, record))
		}
	}()
	var exists bool
	c, exists, err = e.inspect(ctx, record)
	if err != nil {
		return result, err
	}
	c, started, err = e.startAccess(ctx, lock, c, exists, record)
	if err != nil {
		return result, err
	}
	if err = e.refreshNetwork(ctx, record); err != nil {
		return result, err
	}
	if err = e.runAppliedOpenHooks(ctx, c, record); err != nil {
		return result, err
	}
	record.Activity = time.Now().UTC()
	record.Action = "open"
	if err = lock.Save(record); err != nil {
		return result, err
	}
	argv := append([]string{record.Applied.Launch.Binary}, record.Applied.Launch.Args...)
	if q.Continue {
		argv = append(argv, record.Applied.Launch.Continue...)
	}
	argv = append(argv, invocationArgs...)
	argv = append(argv, q.Args...)
	err = e.attach(ctx, lock, c, record, "open", argv)
	return result, err
}
