package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// PendingPlan reviews new desired inputs only after shared publications have
// finished. It cannot change import scope, repeat publication, or reset any
// destination with an already committed normal record.
func (m Merger) PendingPlan(ctx context.Context, j *Journal, accept []string) (MergePlan, error) {
	if j.Merge == nil || j.Phase != "merging" {
		return MergePlan{}, fmt.Errorf("there is no incomplete merge to review")
	}
	for _, pub := range j.Merge.Plan.Publications {
		if !j.Merge.Published[pub.Key] {
			return MergePlan{}, fmt.Errorf("finish or restore approved shared-file publications before reviewing pending environments")
		}
	}
	p := j.Merge.Plan
	p.Sessions = append([]ImportSession(nil), p.Sessions...)
	p.Reviews = nil
	p.Choices.Accept = nil
	for _, r := range j.Merge.Plan.Reviews {
		if !strings.HasPrefix(r.Key, "pending:") {
			p.Reviews = append(p.Reviews, r)
		}
	}
	for _, key := range j.Merge.Plan.Choices.Accept {
		if !strings.HasPrefix(key, "pending:") {
			p.Choices.Accept = append(p.Choices.Accept, key)
		}
	}
	for _, key := range accept {
		if strings.HasPrefix(key, "pending:") {
			p.Choices.Accept = append(p.Choices.Accept, key)
		}
	}
	home := j.Inventory.Paths.Destination
	st := &store.Store{Home: home, Installation: p.Installation}
	e := &app.Engine{Store: st, Docker: m.Docker, UID: m.UID, GID: m.GID}
	for n, job := range p.Sessions {
		if a := j.Merge.Attempts[job.Item]; a != nil && a.Phase == "done" {
			continue
		}
		if b, err := readRegular(ctx, filepath.Join(home, "sessions", job.Identity.Name, "session.json")); err == nil {
			var r store.Record
			if config.Decode(b, &r) != nil || r.Validate(job.Identity.Name) != nil || r.ID != job.ID || r.Identity != job.Identity {
				return p, fmt.Errorf("committed session identity changed")
			}
			continue
		} else if !os.IsNotExist(err) {
			return p, err
		}
		spec, err := e.Resolve(app.Request{Workspace: job.Identity.Workspace, Profile: job.Identity.Profile, ExpectedName: job.Identity.Name, ReadOnly: job.ReadOnly, Host: m.host()})
		if err != nil {
			return p, publicFailure("Pending environment configuration does not resolve.", job.Identity.Name, err)
		}
		if err = checkMapping(home, spec.Harness); err != nil {
			return p, err
		}
		if spec.Harness.Definition.Name != j.Inventory.item(job.Item).Harness {
			return p, fmt.Errorf("pending review cannot switch harnesses")
		}
		for _, arg := range spec.Settings.DockerArgs {
			if strings.HasPrefix(arg, "--env=") {
				return p, fmt.Errorf("move raw Docker env entries to extra_env before importing")
			}
		}
		if spec.Fingerprints != job.Desired {
			p.Sessions[n].Desired = spec.Fingerprints
			p.change("pending:"+job.Item, job.Item, "Use the current final configuration for this unfinished environment, including changed build/setup/runtime inputs. Completed sessions and shared publications will not be reset.")
		}
	}
	return p, nil
}
func (m Merger) Reapprove(ctx context.Context, paths Paths, approved MergePlan) (j *Journal, err error) {
	if err = approved.Ready(); err != nil {
		return nil, err
	}
	if _, err = Load(paths); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(paths.Work, "lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	j, err = Load(paths)
	if err != nil {
		return nil, err
	}
	current, err := m.PendingPlan(ctx, j, approved.Choices.Accept)
	if err != nil {
		return j, err
	}
	if current.Fingerprint() != approved.Fingerprint() {
		return j, fmt.Errorf("pending inputs changed during review")
	}
	// The existing publication/attempt ledger remains authoritative. Only the
	// desired fingerprints of uncommitted sessions advance after approval.
	j.Merge.Plan.Sessions = approved.Sessions
	j.Merge.Plan.Reviews = approved.Reviews
	j.Merge.Plan.Choices.Accept = approved.Choices.Accept
	j.Failure = ""
	return j, saveJournal(j)
}
