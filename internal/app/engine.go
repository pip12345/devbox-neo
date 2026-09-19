// Package app orders lifecycle transitions. Resolution finishes before Docker mutation.
package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/filesync"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/sshshare"
	"devbox/internal/store"
)

type Engine struct {
	Store         *store.Store
	Docker        docker.Runtime
	Streams       docker.Streams
	TerminalEnv   []string
	IgnoreProject bool
	UID           int
	GID           int
}
type Request struct {
	Workspace     string
	Profile       string
	Overrides     config.Layer
	IgnoreProject bool
	Recorded      *environment.Identity
	Continue      bool
	Args          []string
	Host          config.Host
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
	return environment.Resolve(environment.Request{Home: e.Store.Home, Workspace: q.Workspace, Profile: q.Profile, Overrides: q.Overrides, IgnoreProject: q.IgnoreProject || e.IgnoreProject, Recorded: q.Recorded, UID: e.UID, GID: e.GID, Salt: e.Store.Installation, Host: q.Host})
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
	if e.Streams.Err != nil {
		fmt.Fprintf(e.Streams.Err, "Warning: %s\n", diagnostic.Message)
		for _, inputChange := range diagnostic.PendingInputChanges {
			fmt.Fprintf(e.Streams.Err, "  - %s\n", inputChange)
		}
		if diagnostic.Code == "creation_drift" {
			fmt.Fprintln(e.Streams.Err, "\nUsing the existing container without applying these creation changes.")
			if diagnostic.Change == environment.RebuildAndRecreate {
				fmt.Fprintln(e.Streams.Err, "Rebuild image and recreate:")
			} else {
				fmt.Fprintln(e.Streams.Err, "Recreate to apply changes:")
			}
		}
		fmt.Fprintf(e.Streams.Err, "  %s\n", strings.Join(diagnostic.Command, " "))
	}
}
func (e *Engine) owner(r store.Record) docker.Owner {
	return docker.Owner{Installation: e.Store.Installation, Session: r.ID, Workspace: r.Identity.Workspace, Slot: r.Identity.Slot}
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

// Create prepares a new environment without attaching a harness. Preparation
// requires a running container, but successful standalone creation leaves it stopped.
func (e *Engine) Create(ctx context.Context, q Request) (Result, error) {
	spec, err := e.Resolve(q)
	if err != nil {
		return Result{}, err
	}
	result := Result{Name: spec.Identity.Name}
	lock, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if _, err = lock.Load(); err == nil {
		return result, commanderror.New("session_exists", "Environment already exists.", result.Name, nil,
			commanderror.Next("Open", "open", result.Name),
			commanderror.Next("Or recreate with current configuration", "recreate", result.Name))
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err = e.requireNew(ctx, lock); err != nil {
		return result, err
	}
	record, c, err := e.create(ctx, lock, spec, nil, false)
	if err != nil {
		return result, err
	}
	if err = e.Docker.Stop(ctx, c, e.owner(record)); err != nil {
		return result, commanderror.New("create_stop_failed", "Environment created, but stopping it failed.", result.Name, err,
			commanderror.Next("Stop", "stop", result.Name))
	}
	return result, nil
}

func creationRequired(workspace, profile string, cause error) error {
	message := "No environment exists."
	if profile != "" {
		message = fmt.Sprintf("No environment exists (profile: %s).", profile)
	}
	return commanderror.New("session_missing", message, workspace, cause,
		commanderror.Next("Create", "create", workspace))
}

// Open keeps the operation lock through stopped-only synchronization, startup,
// and lease creation. The long foreground command runs after releasing it.
func (e *Engine) Open(ctx context.Context, q Request) (result Result, err error) {
	invocationArgs := append([]string(nil), q.Overrides.HarnessArgs...)
	q.Overrides = config.Layer{}
	if strings.HasPrefix(q.Workspace, environment.ContainerPrefix) && !strings.ContainsAny(q.Workspace, "/\\") {
		r, loadErr := e.readSession(ctx, q.Workspace)
		if loadErr != nil {
			return result, loadErr
		}
		if q.Profile != "" && q.Profile != r.Identity.Profile {
			return result, fmt.Errorf("profile does not match the recorded target")
		}
		q.Workspace = r.Identity.Workspace
		q.Profile = r.Identity.Profile
		if (e.IgnoreProject || q.IgnoreProject) && r.Identity.Project {
			return result, fmt.Errorf("project selection does not match the recorded target")
		}
		q.Recorded = &r.Identity
	}
	// Defer resolution warnings so creation drift is visible before any other
	// open output, especially before entrypoint or harness output can scroll it away.
	spec, err := e.resolveSpec(q)
	if err != nil {
		e.resolutionWarnings(spec)
		return result, err
	}
	result.Name = spec.Identity.Name
	if _, err = store.ProcessIdentity(os.Getpid()); err != nil {
		return result, err
	}
	lock, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	record, err := lock.Load()
	if errors.Is(err, os.ErrNotExist) {
		return result, creationRequired(q.Workspace, spec.Identity.Profile, err)
	}
	if err != nil {
		return result, err
	}
	e.creationDrift(&result, record, spec)
	e.resolutionWarnings(spec)
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
	wasRunning := exists && c.State.Running
	c, started, err = e.startAccess(ctx, lock, c, exists, &record, &spec, &result)
	if err == nil && wasRunning {
		compatible := record.Definition.Hash == spec.Harness.Hash
		if compatible && record.Applied.Runtime != spec.Fingerprints.Runtime {
			manifest, pathErr := lock.Path(filepath.Join("harnesses", record.Definition.Name, "managed-config.json"))
			if pathErr != nil {
				return result, pathErr
			}
			current, checkErr := filesync.Current(manifest, spec.Files, spec.Harness.Definition.Merge)
			if checkErr != nil {
				return result, checkErr
			}
			if current {
				record.ApplyRuntime(spec.Inputs.Runtime)
			} else {
				e.diagnose(&result, Diagnostic{Code: "runtime_deferred", Message: "managed configuration is deferred while running; it will apply at the next startup", Command: []string{"devbox-neo", "stop", record.Identity.Name}})
			}
		}
		applyLaunch(&record, spec)
	}
	if err != nil {
		return result, err
	}
	if err = e.installRuntime(ctx, record); err != nil {
		return result, err
	}
	if err = e.runHook(ctx, c, record, spec.Entrypoint); err != nil {
		return result, err
	}
	record.Activity = time.Now().UTC()
	record.Action = "open"
	if err = lock.Save(record); err != nil {
		return result, err
	}
	argv := append([]string{record.Launch.Binary}, record.Launch.Args...)
	if q.Continue {
		argv = append(argv, record.Launch.Continue...)
	}
	argv = append(argv, invocationArgs...)
	argv = append(argv, q.Args...)
	err = e.attach(ctx, lock, c, record, "open", argv)
	return result, err
}
func (e *Engine) requireNew(ctx context.Context, l *store.Locked) error {
	_, exists, err := e.Docker.Inspect(ctx, l.Name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("container exists without a valid durable contract; refusing adoption")
	}
	dir, err := l.Path(".")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("uncommitted session state exists at %s.\nInspect this directory before retrying creation.", dir)
	}
	return nil
}
func (e *Engine) sync(l *store.Locked, s environment.Spec) error {
	d := s.Harness.Definition
	base := filepath.Join("harnesses", d.Name)
	root, err := l.Dir(filepath.Join(base, "stores", d.Config.Store, d.Config.Path))
	if err != nil {
		return err
	}
	manifest, err := l.Path(filepath.Join(base, "managed-config.json"))
	if err != nil {
		return err
	}
	return filesync.Sync(root, manifest, d.Config.Store, s.Files, d.Merge)
}
func (e *Engine) mountPlan(l *store.Locked, s environment.Spec) ([]docker.Mount, error) {
	sshRoot, err := l.Dir(sshshare.RelativeRoot)
	if err != nil {
		return nil, err
	}
	mounts := []docker.Mount{{Source: s.Identity.Workspace, Target: "/workspace"}, {Source: sshRoot, Target: sshshare.Mount}}
	d := s.Harness.Definition
	for _, storeDef := range d.Stores {
		var source string
		var err error
		if storeDef.Scope == "environment" {
			source, err = l.Dir(filepath.Join("harnesses", d.Name, "stores", storeDef.Name))
		} else {
			source, err = fsutil.Dir(e.Store.Home, filepath.Join("cache/harnesses", d.Name, storeDef.Name), 0700)
		}
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, docker.Mount{Source: source, Target: storeDef.Target})
	}
	for _, auth := range d.Auth {
		rel := filepath.Join("auth", d.Name, auth.Source)
		source, err := fsutil.Path(e.Store.Home, rel)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(source)
		if os.IsNotExist(err) && auth.Create {
			if _, err = fsutil.Dir(e.Store.Home, filepath.Dir(rel), 0700); err != nil {
				return nil, err
			}
			if auth.Kind == "directory" {
				err = os.Mkdir(source, 0700)
			} else {
				var f *os.File
				f, err = os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err == nil {
					if strings.HasSuffix(source, ".json") {
						_, err = f.Write([]byte("{}\n"))
					}
					err = errors.Join(err, f.Close())
				}
			}
			if err != nil && !os.IsExist(err) {
				return nil, err
			}
			info, err = os.Stat(source)
		}
		if err != nil {
			return nil, commanderror.New("auth_unavailable", "Cannot access authentication "+auth.Kind+".", source, err)
		}
		if (auth.Kind == "directory" && !info.IsDir()) || (auth.Kind == "file" && !info.Mode().IsRegular()) {
			return nil, commanderror.New("invalid_auth_path", "Expected an authentication "+auth.Kind+".", source, nil)
		}
		mounts = append(mounts, docker.Mount{Source: source, Target: auth.Target})
	}
	return append(mounts, s.ExtraMounts...), nil
}
func (e *Engine) build(ctx context.Context, s environment.Spec, id string, force bool) (image docker.Image, err error) {
	dir, err := os.MkdirTemp(e.Store.Home, ".build-*")
	if err != nil {
		return image, err
	}
	defer os.RemoveAll(dir)
	contextDir, err := fsutil.Dir(dir, "context", 0700)
	if err != nil {
		return image, err
	}
	names := make([]string, 0, len(s.Build.Context))
	for name := range s.Build.Context {
		names = append(names, name)
	}
	sort.Strings(names)
	// Restore owner access before removing staging directories whose source
	// permissions were read-only. Their original modes belong in the build.
	defer func() {
		for _, name := range names {
			if s.Build.Context[name].Directory {
				p, pathErr := fsutil.Path(contextDir, name)
				if pathErr == nil {
					_ = os.Chmod(p, 0700)
				}
			}
		}
	}()
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return image, err
		}
		file := s.Build.Context[name]
		if file.Directory {
			if _, err = fsutil.Dir(contextDir, name, 0700); err != nil {
				return image, err
			}
			continue
		}
		if _, err = fsutil.Dir(contextDir, filepath.Dir(name), 0700); err != nil {
			return image, err
		}
		p, pathErr := fsutil.Path(contextDir, name)
		if pathErr != nil {
			return image, pathErr
		}
		if err = fsutil.Write(p, file.Data, file.Mode); err != nil {
			return image, err
		}
	}
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		file := s.Build.Context[name]
		if file.Directory {
			if err = os.Chmod(filepath.Join(contextDir, name), file.Mode); err != nil {
				return image, err
			}
		}
	}
	build := func(name, tag string, data []byte, arguments map[string]string) (docker.Image, error) {
		path := filepath.Join(dir, name)
		if err := fsutil.Write(path, data, 0600); err != nil {
			return docker.Image{}, err
		}
		if err := fsutil.Write(path+".dockerignore", s.Build.Ignore, 0600); err != nil {
			return docker.Image{}, err
		}
		return e.Docker.Build(ctx, docker.BuildPlan{Directory: contextDir, Dockerfile: path, Tag: tag, NoCache: force, Installation: e.Store.Installation, Arguments: arguments}, e.Streams.Err)
	}
	baseRef := ""
	if s.Build.Mode == "normal" {
		nonce, idErr := fsutil.ID()
		if idErr != nil {
			return image, idErr
		}
		tag := docker.Namespace + "/build:" + nonce
		base, buildErr := build("base.Dockerfile", tag, s.Build.Dockerfile, s.Build.Arguments)
		if buildErr != nil {
			return image, buildErr
		}
		// BuildKit needs an image reference in FROM; a bare ID is parsed as a registry name.
		baseRef = tag
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = errors.Join(err, e.Docker.Untag(cleanup, tag, base.ID, e.Store.Installation))
		}()
	}
	return build("runtime.Dockerfile", docker.Namespace+"/session:"+id, s.Build.FinalDockerfile(baseRef), s.Build.Arguments)
}
func (e *Engine) create(ctx context.Context, l *store.Locked, s environment.Spec, previous *store.Record, force bool) (store.Record, docker.Container, error) {
	return e.createAs(ctx, l, s, previous, force, CreationIdentity{})
}

// CreationIdentity supplies durable identity/activity for a new destination.
// Zero activity means this creation is its first recorded activity.
type CreationIdentity struct {
	ID          string
	Created     time.Time
	Activity    time.Time
	Action      string
	ManualStart bool
}

// CreatePrepared materializes current-format state prepared under the supplied
// operation lock. The caller owns publication and recovery of the prepared
// directory; this method never adopts an existing record or container.
func (e *Engine) CreatePrepared(ctx context.Context, l *store.Locked, s environment.Spec, identity CreationIdentity) (store.Record, docker.Container, error) {
	if !l.Held() || l.Name != s.Identity.Name || identity.Created.IsZero() || len(identity.ID) != 32 {
		return store.Record{}, docker.Container{}, fmt.Errorf("invalid prepared destination identity or lock")
	}
	if _, err := hex.DecodeString(identity.ID); err != nil || strings.ToLower(identity.ID) != identity.ID {
		return store.Record{}, docker.Container{}, fmt.Errorf("invalid prepared session ID")
	}
	if _, err := l.ReadRecord(ctx); err == nil {
		return store.Record{}, docker.Container{}, fmt.Errorf("prepared destination already has a session record")
	} else if !errors.Is(err, os.ErrNotExist) {
		return store.Record{}, docker.Container{}, err
	}
	if _, exists, err := e.Docker.Inspect(ctx, l.Name); err != nil {
		return store.Record{}, docker.Container{}, err
	} else if exists {
		return store.Record{}, docker.Container{}, fmt.Errorf("prepared destination already has a Docker container")
	}
	return e.createAs(ctx, l, s, nil, false, identity)
}

// Transfer and prepared creation supply identity before creating resources.
// Ordinary create/recreate still allocate or preserve their own identity.
func (e *Engine) createAs(ctx context.Context, l *store.Locked, s environment.Spec, previous *store.Record, force bool, seed CreationIdentity) (record store.Record, c docker.Container, err error) {
	id, created := seed.ID, seed.Created
	if err = l.RequireIdle(); err != nil {
		return record, c, err
	}
	if err = e.Docker.Network(ctx, s.Settings.Network); err != nil {
		return record, c, err
	}
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if previous != nil {
		id = previous.ID
		created = previous.Created
	} else if id == "" {
		id, err = fsutil.ID()
		if err != nil {
			return record, c, err
		}
	}
	var image docker.Image
	if previous != nil && !force && previous.Applied.Image == s.Fingerprints.Image {
		available, inspectErr := e.Docker.ImageAvailable(ctx, previous.ImageID)
		if inspectErr != nil {
			return record, c, inspectErr
		}
		if available {
			image, err = e.Docker.InspectImage(ctx, previous.ImageID)
			if err == nil {
				err = image.Verify(e.Store.Installation)
			}
		} else {
			image, err = e.build(ctx, s, id, false)
		}
	} else {
		image, err = e.build(ctx, s, id, force)
	}
	if err != nil {
		return record, c, err
	}
	var old docker.Container
	var existed, removed bool
	if previous != nil {
		old, existed, err = e.inspect(ctx, *previous)
		if err != nil {
			return record, c, err
		}
	}
	// Before removal, a failed sync must restore a previously running original.
	// After removal, the old record and image remain the recovery authority.
	defer func() {
		if err != nil && existed && !removed && old.State.Running {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = errors.Join(err, e.start(cleanup, old, *previous))
		}
	}()
	if existed && old.State.Running {
		if err = e.Docker.Stop(ctx, old, e.owner(*previous)); err != nil {
			return record, c, err
		}
	}
	mounts, err := e.mountPlan(l, s)
	if err != nil {
		return record, c, err
	}
	if err = e.sync(l, s); err != nil {
		return record, c, err
	}
	if existed {
		if err = e.Docker.Remove(ctx, old, e.owner(*previous)); err != nil {
			return record, c, err
		}
		removed = true
	}
	record = store.Record{Version: store.RecordVersion, ID: id, Identity: s.Identity, Created: created, Activity: time.Now().UTC(), Action: "create", Applied: s.FingerprintsFor(image.ID), Inputs: s.Inputs, ImageTag: docker.Namespace + "/session:" + id, ImageID: image.ID, Creation: docker.CreatePlan{Name: s.Identity.Name, Image: image.ID, Network: s.Settings.Network, Mounts: mounts, Env: s.Env(), Ports: s.Settings.Ports, RawArgs: s.Settings.DockerArgs, Metadata: s.Metadata}, EnvSources: s.EnvSources, Definition: store.DefinitionInput{Name: s.Harness.Definition.Name, Origin: s.Harness.Origin, Hash: s.Harness.Hash}, Stores: s.Harness.Definition.Stores, Auth: s.Harness.Definition.Auth, Config: s.Harness.Definition.Config, Merge: s.Harness.Definition.Merge, Prepare: s.Harness.Definition.Prepare, Launch: store.Launch{Binary: s.Harness.Definition.Binary, Args: append(append([]string(nil), s.Harness.Definition.Launch.Args...), s.Settings.HarnessArgs...), Continue: s.Harness.Definition.Launch.Continue, Shell: s.Settings.Shell}, Setup: s.Setup, Ownership: 1, ManifestVersion: 1}
	record.ManualStart = seed.ManualStart
	if previous != nil {
		record.ManualStart = previous.ManualStart
		record.Activity = previous.Activity
		record.Action = "recreate"
	} else {
		if !seed.Activity.IsZero() {
			record.Activity = seed.Activity
		}
		if seed.Action != "" {
			record.Action = seed.Action
		}
	}
	c, err = e.materialize(ctx, record)
	if err != nil {
		return record, c, err
	}
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			stopErr := e.Docker.Stop(cleanup, c, e.owner(record))
			err = errors.Join(err, stopErr)
			if stopErr == nil {
				err = errors.Join(err, e.Docker.Remove(cleanup, c, e.owner(record)))
			}
		}
	}()
	record.SetupContainer = c.ID
	if err = l.Save(record); err != nil {
		return record, c, err
	}
	committed = true
	return record, c, nil
}
func (e *Engine) materialize(ctx context.Context, record store.Record) (c docker.Container, err error) {
	// This attempt prepares a new instance; only its successful caller commits
	// the new setup-container ID. Existing-instance access never clears it.
	record.SetupContainer = ""
	if err = prepareMountParents(record); err != nil {
		return c, err
	}
	// Terminal defaults are invocation-local; configured env takes precedence at
	// creation. Recovery uses today's terminal without changing the saved contract.
	plan := record.Creation
	plan.RestartPolicy = restartPolicy(record.ManualStart)
	plan.Env = append(append([]string(nil), e.TerminalEnv...), plan.Env...)
	id, err := e.Docker.Create(ctx, plan, e.owner(record))
	if err != nil {
		return c, err
	}
	c, exists, err := e.inspect(ctx, record)
	if err != nil {
		return c, err
	}
	if !exists || c.ID != id {
		return c, fmt.Errorf("new container identity could not be verified")
	}
	ok := false
	defer func() {
		if !ok {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			stopErr := e.Docker.Stop(cleanup, c, e.owner(record))
			err = errors.Join(err, stopErr)
			if stopErr == nil {
				err = errors.Join(err, e.Docker.Remove(cleanup, c, e.owner(record)))
			}
		}
	}()
	if err = e.start(ctx, c, record); err != nil {
		return c, err
	}
	c.State.Running = true
	if err = e.installRuntime(ctx, record); err != nil {
		return c, err
	}
	for _, argv := range record.Prepare {
		if err = e.Docker.Exec(ctx, c, e.owner(record), argv, nil, docker.Streams{Out: e.Streams.Out, Err: e.Streams.Err}); err != nil {
			return c, err
		}
	}
	if err = e.runHook(ctx, c, record, record.Setup); err != nil {
		return c, err
	}
	if err = e.Docker.Exec(ctx, c, e.owner(record), []string{"sh", "-c", `command -v "$1" >/dev/null`, "--", record.Launch.Binary}, nil, docker.Streams{Err: e.Streams.Err}); err != nil {
		return c, err
	}
	ok = true
	return c, nil
}
func (e *Engine) runHook(ctx context.Context, c docker.Container, r store.Record, hook environment.Hook) error {
	if hook.Path == "" {
		return nil
	}
	return e.Docker.Exec(ctx, c, e.owner(r), []string{"bash", "-s"}, nil, docker.Streams{In: bytes.NewReader(hook.Data), Out: e.Streams.Out, Err: e.Streams.Err})
}
func (e *Engine) stopUnattached(l *store.Locked, r store.Record) error {
	if r.ID == "" || r.ManualStart {
		return nil
	}
	active, err := l.Active()
	if err != nil || len(active) != 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, exists, err := e.inspect(ctx, r)
	if err != nil || !exists || !c.State.Running {
		return err
	}
	return e.Docker.Stop(ctx, c, e.owner(r))
}

func (e *Engine) attach(ctx context.Context, l *store.Locked, c docker.Container, r store.Record, action string, argv []string) error {
	return e.attachRun(l, r, action, func() error {
		return e.Docker.Exec(ctx, c, e.owner(r), argv, e.TerminalEnv, e.Streams)
	})
}

// attachRun owns the lease for both container commands and foreground SSH
// controllers. A host-side master must protect the environment just as an exec does.
func (e *Engine) attachRun(l *store.Locked, r store.Record, action string, run func() error) (err error) {
	lease, err := l.Lease(action)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		lock, cleanupErr := e.Store.Lock(cleanup, r.Identity.Name)
		if cleanupErr == nil {
			defer lock.Close()
			cleanupErr = lock.Release(lease.ID)
			if cleanupErr == nil {
				current, touchErr := lock.Touch(r.ID, action)
				active, aerr := lock.Active()
				cleanupErr = errors.Join(touchErr, aerr)
				if current.ID != "" && aerr == nil && len(active) == 0 && !current.ManualStart {
					live, exists, ierr := e.inspect(cleanup, current)
					cleanupErr = errors.Join(cleanupErr, ierr)
					if ierr == nil && exists && live.State.Running {
						cleanupErr = errors.Join(cleanupErr, e.Docker.Stop(cleanup, live, e.owner(current)))
					}
				}
			}
		}
		// Joining keeps errors.As able to find the foreground ExitError. Cleanup is
		// still visible instead of replacing the user's command status with success.
		err = errors.Join(err, cleanupErr)
	}()
	if err = l.Close(); err != nil {
		return err
	}
	return run()
}
func (e *Engine) Recreate(ctx context.Context, q Request, force bool) (Result, error) {
	s, err := e.Resolve(q)
	if err != nil {
		return Result{}, err
	}
	l, err := e.Store.Lock(ctx, s.Identity.Name)
	if err != nil {
		return Result{}, err
	}
	defer l.Close()
	old, err := l.Load()
	if err != nil {
		return Result{}, err
	}
	if err = l.RequireIdle(); err != nil {
		return Result{}, err
	}
	container, exists, err := e.inspect(ctx, old)
	if err != nil {
		return Result{}, err
	}
	running := old.ManualStart || (exists && container.State.Running)
	r, c, err := e.create(ctx, l, s, &old, force)
	if err != nil {
		return Result{}, err
	}
	if !running {
		if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return Result{}, err
		}
	}
	return Result{Name: s.Identity.Name}, nil
}

func (e *Engine) recover(ctx context.Context, l *store.Locked, r *store.Record, desired *environment.Spec) (docker.Container, error) {
	unavailable := func(reason string, cause error) (docker.Container, error) {
		return docker.Container{}, commanderror.New("recovery_unavailable", "Cannot restore container: "+reason, r.Identity.Name, cause,
			commanderror.Next("Recreate with current configuration", "recreate", r.Identity.Name))
	}
	if err := l.RequireIdle(); err != nil {
		return docker.Container{}, err
	}
	image, err := e.Docker.InspectImage(ctx, r.ImageID)
	if err != nil {
		return unavailable("recorded image is unavailable", err)
	}
	if err = image.Verify(e.Store.Installation); err != nil {
		return docker.Container{}, err
	}
	if err = e.Docker.Network(ctx, r.Creation.Network); err != nil {
		return unavailable("recorded network is unavailable", err)
	}
	for _, m := range r.Creation.Mounts {
		if m.Kind == "volume" {
			if err = e.Docker.Volume(ctx, m.Source); err != nil {
				return unavailable("recorded volume is unavailable", err)
			}
			continue
		}
		if _, err = fsutil.Path(filepath.Dir(m.Source), filepath.Base(m.Source)); err != nil {
			return unavailable("recorded bind source is unsafe", err)
		}
		info, statErr := os.Stat(m.Source)
		if statErr != nil {
			return unavailable("recorded bind source is missing", statErr)
		}
		file := m.File
		for _, auth := range r.Auth {
			if auth.Target == m.Target && auth.Kind == "file" {
				file = true
			}
		}
		if (file && !info.Mode().IsRegular()) || (!file && !info.IsDir()) {
			return unavailable("recorded bind source has the wrong kind", nil)
		}
	}
	protected := []string{"/devbox"}
	for _, mount := range r.Creation.Mounts {
		protected = append(protected, mount.Target)
	}
	if err = docker.ValidateRaw(r.Creation.RawArgs, protected, r.Identity.Workspace, ""); err != nil {
		return unavailable("recorded raw Docker inputs are unavailable or invalid", err)
	}
	if err = e.Docker.CheckRawVolumes(ctx, r.Creation.RawArgs); err != nil {
		return unavailable("recorded raw Docker volume is unavailable", err)
	}
	if r.Definition.Origin != "builtin" {
		expected, err := fsutil.Path(e.Store.Home, filepath.Join("harnesses", r.Definition.Name, "harness.json"))
		if err != nil || expected != r.Definition.Origin {
			return unavailable("recorded definition source is unsafe", err)
		}
	}
	definition, hash, err := harness.Recorded(r.Definition.Name, r.Definition.Origin)
	if err != nil || environment.Fingerprint(e.Store.Installation, hash) != r.Definition.Hash {
		return unavailable("recorded environment source is missing or changed", err)
	}
	keys := make([]string, 0, len(definition.Env))
	for k := range definition.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r.Creation.Env = nil
	for _, k := range keys {
		r.Creation.Env = append(r.Creation.Env, k+"="+definition.Env[k])
	}
	var host config.Host
	if desired != nil {
		host = desired.Host
	} else {
		host = config.Snapshot()
	}
	sources := map[string][]byte{}
	for _, source := range r.EnvSources {
		if source.Kind == "file" {
			if err := e.validateEnvSource(*r, source); err != nil {
				return unavailable("recorded environment source path is unsafe", err)
			}
		}
		value, err := source.Restore(e.Store.Installation, host, sources)
		if err != nil {
			return unavailable(err.Error(), err)
		}
		r.Creation.Env = append(r.Creation.Env, value)
	}
	if r.Setup.Path != "" {
		data, err := os.ReadFile(r.Setup.Path)
		if err != nil || environment.Digest(data) != r.Setup.Hash {
			return unavailable("recorded setup input is missing or changed", err)
		}
		r.Setup.Data = data
	}
	if desired != nil {
		if err = e.syncRecordedConfig(l, r, *desired); err != nil {
			return docker.Container{}, err
		}
	}
	c, err := e.materialize(ctx, *r)
	if err != nil {
		return c, err
	}
	r.SetupContainer = c.ID
	if err = l.Save(*r); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		stopErr := e.Docker.Stop(cleanup, c, e.owner(*r))
		err = errors.Join(err, stopErr)
		if stopErr == nil {
			err = errors.Join(err, e.Docker.Remove(cleanup, c, e.owner(*r)))
		}
	}
	return c, err
}
