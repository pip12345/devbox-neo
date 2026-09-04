package dockertest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"devbox/internal/docker"
)

// Daemon is a stateful command-boundary fake. It deliberately rejects operations
// not covered by the lifecycle contract instead of returning generic success.
type Daemon struct {
	mu         sync.Mutex
	Calls      [][]string
	Containers map[string]docker.Container
	Images     map[string]docker.Image
	Sequence   int
	Fail       func([]string) error
	Attached   func(context.Context, docker.Command) error
}

func (d *Daemon) Run(ctx context.Context, c docker.Command) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	d.Calls = append(d.Calls, slices.Clone(c.Args))
	if d.Fail != nil {
		if err := d.Fail(c.Args); err != nil {
			d.mu.Unlock()
			return err
		}
	}
	if d.Containers == nil {
		d.Containers = map[string]docker.Container{}
		d.Images = map[string]docker.Image{}
	}
	a := c.Args
	if len(a) > 0 && a[0] == "exec" && d.Attached != nil {
		handler := d.Attached
		d.mu.Unlock()
		return handler(ctx, c)
	}
	output, err := d.run(a)
	d.mu.Unlock()
	if err != nil {
		return err
	}
	if c.Stdout != nil {
		_, err = io.WriteString(c.Stdout, output)
	}
	return err
}
func (d *Daemon) run(a []string) (string, error) {
	if len(a) == 0 {
		return "", fmt.Errorf("empty command")
	}
	encode := func(v any) (string, error) { b, err := json.Marshal(v); return string(b), err }
	flag := func(name string) string {
		for i, v := range a {
			if v == name && i+1 < len(a) {
				return a[i+1]
			}
		}
		return ""
	}
	labels := func() map[string]string {
		m := map[string]string{}
		for i, v := range a {
			if v == "--label" && i+1 < len(a) {
				k, val, _ := strings.Cut(a[i+1], "=")
				m[k] = val
			}
		}
		return m
	}
	switch a[0] {
	case "container":
		if a[1] == "ls" {
			pattern, err := regexp.Compile(strings.TrimPrefix(flag("--filter"), "name="))
			if err != nil {
				return "", err
			}
			var ids []string
			for _, c := range d.Containers {
				if pattern.MatchString(c.Name) {
					ids = append(ids, c.ID)
				}
			}
			return strings.Join(ids, "\n"), nil
		}
		if a[1] == "inspect" {
			for _, c := range d.Containers {
				if c.ID == a[2] {
					return encode([]docker.Container{c})
				}
			}
			return "", fmt.Errorf("container missing")
		}
	case "image":
		if a[1] == "ls" {
			ids := map[string]bool{}
			for _, image := range d.Images {
				ids[image.ID] = true
			}
			var out []string
			for id := range ids {
				out = append(out, id)
			}
			return strings.Join(out, "\n"), nil
		}
		if a[1] == "inspect" {
			image, ok := d.Images[a[2]]
			if !ok {
				return "", fmt.Errorf("image missing")
			}
			return encode([]docker.Image{image})
		}
	case "build":
		d.Sequence++
		image := docker.Image{ID: fmt.Sprintf("sha256:%064x", d.Sequence)}
		image.Config.Labels = labels()
		d.Images[flag("--tag")] = image
		d.Images[image.ID] = image
		return "", nil
	case "create":
		name := flag("--name")
		if _, exists := d.Containers[name]; exists {
			return "", fmt.Errorf("name collision")
		}
		d.Sequence++
		c := docker.Container{ID: fmt.Sprintf("%064x", d.Sequence), Name: "/" + name, Image: a[len(a)-2]}
		c.Config.Labels = labels()
		d.Containers[name] = c
		return c.ID, nil
	case "start", "stop", "rm":
		id := a[len(a)-1]
		for name, c := range d.Containers {
			if c.ID == id {
				if a[0] == "rm" {
					if c.State.Running {
						return "", fmt.Errorf("cannot remove running container")
					}
					delete(d.Containers, name)
				} else {
					c.State.Running = a[0] == "start"
					d.Containers[name] = c
				}
				return "", nil
			}
		}
		return "", fmt.Errorf("container missing")
	case "exec":
		return "", nil
	case "network":
		if len(a) == 3 && a[1] == "inspect" {
			return "[]", nil
		}
	}
	return "", fmt.Errorf("unimplemented fake Docker operation %s", a[0])
}
func (d *Daemon) Snapshot(name string) (docker.Container, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.Containers[name]
	c.Config.Labels = maps.Clone(c.Config.Labels)
	return c, ok
}
func (d *Daemon) History() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]string, len(d.Calls))
	for i, a := range d.Calls {
		out[i] = slices.Clone(a)
	}
	return out
}
func (d *Daemon) Forget(name string) { d.mu.Lock(); defer d.mu.Unlock(); delete(d.Containers, name) }
func (d *Daemon) SetContainer(c docker.Container) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Containers[strings.TrimPrefix(c.Name, "/")] = c
}
