package migration

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func needsOpenCodeConfig(j *Journal, key string) bool {
	for _, f := range j.Inventory.Files {
		if f.Item == key && strings.Contains(filepath.ToSlash(f.Relative), "/harnesses/opencode/stores/data") {
			return true
		}
	}
	return false
}

// CaptureConfigs reads known config homes from verified stopped containers.
// It changes only private work state, never either installation or a project.
func (m Merger) CaptureConfigs(ctx context.Context, p Paths, choices MergeChoices) (j *Journal, err error) {
	if _, err = Load(p); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(p.Work, "lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	j, err = Load(p)
	if err != nil {
		return nil, err
	}
	if j.Phase != "prepared" || j.Merge != nil {
		return j, fmt.Errorf("configuration capture requires a prepared, unmerged run")
	}
	excluded, err := excludedMerge(j, choices)
	if err != nil {
		return j, err
	}
	release, err := lockSource(ctx, &j.Inventory)
	if err != nil {
		return j, err
	}
	defer release()
	if err = checkLeases(ctx, &j.Inventory); err != nil {
		return j, err
	}
	if err = checkRuntime(ctx, DockerSource{m.Docker}, &j.Inventory, excluded); err != nil {
		return j, err
	}
	if err = verifyMergeSource(ctx, j); err != nil {
		return j, err
	}
	if j.Captures == nil {
		j.Captures = map[string]Capture{}
	}
	for _, item := range j.Inventory.Items {
		if excluded[item.Key] != "" || item.Kind != "session" || contains(choices.OmitConfig, item.Key) || !needsOpenCodeConfig(j, item.Key) {
			continue
		}
		captured, e := m.capture(ctx, j, item)
		if e != nil {
			return j, e
		}
		if previous, ok := j.Captures[item.Key]; ok {
			removePreview(captured.Root)
			if previous.Container != captured.Container || previous.Hash != captured.Hash {
				return j, fmt.Errorf("container configuration changed since capture; preserve this run and prepare a new reviewed snapshot")
			}
			continue
		}
		j.Captures[item.Key] = captured
		if err = saveJournal(j); err != nil {
			return j, err
		}
		m.note("Captured stopped-container configuration: " + display(item.Key))
	}
	return j, nil
}
func (m Merger) verifyCaptures(ctx context.Context, j *Journal, plan MergePlan) error {
	for _, job := range plan.Sessions {
		if contains(plan.Choices.OmitConfig, job.Item) || !needsOpenCodeConfig(j, job.Item) {
			continue
		}
		if j.Merge != nil {
			if a := j.Merge.Attempts[job.Item]; a != nil && a.Phase == "done" {
				continue
			}
			if b, e := readRegular(ctx, filepath.Join(j.Inventory.Paths.Destination, "sessions", job.Identity.Name, "session.json")); e == nil {
				var r store.Record
				if config.Decode(b, &r) == nil && r.Validate(job.Identity.Name) == nil && r.ID == job.ID && r.Identity == job.Identity {
					continue
				}
			}
		}
		previous, ok := plan.Captures[job.Item]
		if !ok {
			return fmt.Errorf("required container config was not captured")
		}
		if !within(j.Inventory.Paths.Work, previous.Root) {
			return fmt.Errorf("captured config is outside the work directory")
		}
		current, err := stateHash(ctx, previous.Root)
		if err != nil || current != previous.Hash {
			return fmt.Errorf("captured config changed")
		}
		item := j.Inventory.item(job.Item)
		if item == nil {
			return fmt.Errorf("unknown capture owner")
		}
		fresh, err := m.capture(ctx, j, *item)
		if err != nil {
			return err
		}
		removePreview(fresh.Root)
		if fresh.Container != previous.Container || fresh.Hash != previous.Hash {
			return fmt.Errorf("source container configuration changed since review")
		}
	}
	return nil
}
func (m Merger) capture(ctx context.Context, j *Journal, item Item) (result Capture, err error) {
	c, exists, err := m.Docker.Inspect(ctx, item.Name)
	if err != nil {
		return result, publicFailure("Cannot inspect config source container.", item.Name, err)
	}
	if !exists {
		return result, fmt.Errorf("container-only OpenCode config is unavailable for %s; explicitly omit it or skip the session", item.Name)
	}
	labels := c.Config.Labels
	if c.State.Running || labels["devbox.managed"] != "true" || labels["devbox.installation_id"] != j.Inventory.Installation || labels["devbox.session_id"] != item.SessionID {
		return result, fmt.Errorf("config source is running or its ownership does not match")
	}
	root, err := os.MkdirTemp(j.Inventory.Paths.Work, "capture-")
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			removePreview(root)
		}
	}()
	captureCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := m.Docker.Runner.Run(captureCtx, docker.Command{Args: []string{"cp", c.ID + ":/home/devuser/.config/opencode/.", "-"}, Stdout: writer, Stderr: io.Discard})
		_ = writer.CloseWithError(err)
		done <- err
	}()
	extractErr := extractConfig(captureCtx, tar.NewReader(reader), root, filepath.Join(item.Path, ".staged-harness/opencode"))
	if extractErr != nil {
		cancel()
	}
	_ = reader.CloseWithError(extractErr)
	commandErr := <-done
	if extractErr != nil || commandErr != nil {
		return result, publicFailure("Cannot capture OpenCode config; inspect its files/links or explicitly omit this config.", item.Name, errors.Join(extractErr, commandErr))
	}
	// Same immutable container and stopped state must still hold after the read.
	after, present, err := m.Docker.Inspect(ctx, item.Name)
	if err != nil || !present || after.ID != c.ID || after.State.Running {
		return result, fmt.Errorf("config source changed during capture")
	}
	if err = syncStagedDirectories(ctx, root); err != nil {
		return result, err
	}
	hash, err := stateHash(ctx, root)
	if err != nil {
		return result, err
	}
	return Capture{Root: root, Container: c.ID, Hash: hash}, nil
}
func extractConfig(ctx context.Context, tr *tar.Reader, root, generated string) error {
	type link struct{ relative, target string }
	links := []link{}
	seen := map[string]bool{}
	prefix := ""
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if first {
			if h.Typeflag != tar.TypeDir || (name != "." && name != "opencode") {
				return fmt.Errorf("unexpected Docker config archive root")
			}
			if name == "opencode" {
				prefix = "opencode"
			}
			first = false
		}
		if prefix != "" {
			if name == prefix {
				name = "."
			} else if strings.HasPrefix(name, prefix+"/") {
				name = strings.TrimPrefix(name, prefix+"/")
			} else {
				return fmt.Errorf("archive entry outside its declared config root")
			}
		}
		if !filepath.IsLocal(name) || seen[name] {
			return fmt.Errorf("unsafe or duplicate config archive path")
		}
		seen[name] = true
		target, err := fsutil.Path(root, name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if _, err = fsutil.Dir(root, name, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if name == "." {
				return fmt.Errorf("config archive root is not a directory")
			}
			if _, err = fsutil.Dir(filepath.Dir(target), ".", 0700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateMode(os.FileMode(h.Mode)))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, contextReader{ctx, tr})
			syncErr := out.Sync()
			closeErr := out.Close()
			if err = errors.Join(copyErr, syncErr, closeErr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			links = append(links, link{name, h.Linkname})
		default:
			return fmt.Errorf("unsupported special/hard-linked config archive entry")
		}
	}
	if first {
		return fmt.Errorf("empty config archive")
	}
	for _, l := range links {
		target, err := fsutil.Path(root, l.relative)
		if err != nil {
			return err
		}
		if _, err = fsutil.Dir(filepath.Dir(target), ".", 0700); err != nil {
			return err
		}
		if l.target == "/devbox/harness-config" || strings.HasPrefix(l.target, "/devbox/harness-config/") {
			rel := strings.TrimPrefix(l.target, "/devbox/harness-config")
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				rel = "."
			}
			source, err := fsutil.Path(generated, rel)
			if err != nil {
				return err
			}
			info, err := os.Lstat(source)
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err = copyTree(ctx, source, target); err != nil {
					return err
				}
			} else {
				b, err := readRegular(ctx, source)
				if err != nil {
					return err
				}
				if err = fsutil.WriteNew(target, b, privateMode(info.Mode())); err != nil {
					return err
				}
			}
		} else {
			if filepath.IsAbs(l.target) || !filepath.IsLocal(filepath.Join(filepath.Dir(l.relative), l.target)) {
				return fmt.Errorf("config link escapes approved roots")
			}
			if err = os.Symlink(l.target, target); err != nil {
				return err
			}
		}
	}
	for _, l := range links {
		target := filepath.Join(root, l.relative)
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil || !within(root, resolved) {
			return fmt.Errorf("unresolved or escaping config link")
		}
	}
	return nil
}
