package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devbox/internal/assets"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func (e *Engine) installRuntime(ctx context.Context, r store.Record) error {
	return e.writeRuntime(ctx, r, true)
}

// Network facts describe live attachments, not desired configuration. Refreshing
// them on access must not also apply a new version of the documentation bundle.
func (e *Engine) refreshNetwork(ctx context.Context, r store.Record) error {
	return e.writeRuntime(ctx, r, false)
}

func (e *Engine) writeRuntime(ctx context.Context, r store.Record, includeDocs bool) error {
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists || !c.State.Running {
		return fmt.Errorf("runtime preparation requires the verified running container")
	}
	files := map[string][]byte{}
	if includeDocs {
		files, err = assets.Files()
		if err != nil {
			return err
		}
	}
	facts := networkFacts(r, c)
	b, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return err
	}
	files["network/inspect.json"] = append(b, '\n')
	var env strings.Builder
	values := facts.Env()
	for _, key := range sortedEnv(values) {
		fmt.Fprintf(&env, "export %s='%s'\n", key, strings.ReplaceAll(values[key], "'", "'\"'\"'"))
	}
	files["network/env"] = []byte(env.String())
	dir, err := os.MkdirTemp(e.Store.Home, ".runtime-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if _, err = fsutil.Dir(dir, filepath.Dir(name), 0755); err != nil {
			return err
		}
		if err = fsutil.Write(filepath.Join(dir, name), files[name], 0644); err != nil {
			return err
		}
	}
	return e.Docker.InstallRuntime(ctx, c, e.owner(r), dir)
}
