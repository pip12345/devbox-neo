package dockertest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"devbox/internal/docker"
)

// Daemon is a stateful command-boundary fake. It deliberately rejects operations
// not covered by the lifecycle contract instead of returning generic success.
type Daemon struct {
	mu          sync.Mutex
	Calls       [][]string
	Containers  map[string]docker.Container
	Images      map[string]docker.Image
	Volumes     map[string]bool
	Hooks       map[string]map[string][]byte
	stagedHooks map[string]map[string][]byte
	Sequence    int
	Fail        func([]string) error
	Attached    func(context.Context, docker.Command) error
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
	}
	if d.Images == nil {
		d.Images = map[string]docker.Image{}
	}
	if d.Volumes == nil {
		d.Volumes = map[string]bool{}
	}
	a := c.Args
	if len(a) > 0 && a[0] == "exec" && d.Attached != nil {
		handler := d.Attached
		_, err := d.run(a)
		d.mu.Unlock()
		if err != nil {
			return err
		}
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
			var ids []string
			for _, c := range d.Containers {
				match := true
				for i, arg := range a {
					if arg != "--filter" {
						continue
					}
					filter := a[i+1]
					if pattern, ok := strings.CutPrefix(filter, "name="); ok {
						re, err := regexp.Compile(pattern)
						if err != nil {
							return "", err
						}
						match = match && re.MatchString(c.Name)
					}
					if id, ok := strings.CutPrefix(filter, "id="); ok {
						match = match && strings.HasPrefix(c.ID, id)
					}
					if label, ok := strings.CutPrefix(filter, "label="); ok {
						key, value, _ := strings.Cut(label, "=")
						match = match && c.Config.Labels[key] == value
					}
				}
				if match {
					ids = append(ids, c.ID)
				}
			}
			slices.Sort(ids)
			return strings.Join(ids, "\n"), nil
		}
		if a[1] == "inspect" {
			result := []docker.Container{}
			for _, id := range a[2:] {
				found := false
				for _, c := range d.Containers {
					if c.ID == id {
						result = append(result, c)
						found = true
						break
					}
				}
				if !found {
					return "", fmt.Errorf("container missing")
				}
			}
			return encode(result)
		}
	case "image":
		if a[1] == "tag" {
			image, ok := d.Images[a[2]]
			if !ok {
				return "", fmt.Errorf("image missing")
			}
			d.Images[a[3]] = image
			return "", nil
		}
		if a[1] == "ls" {
			if ref, ok := strings.CutPrefix(flag("--filter"), "reference="); ok {
				image, exists := d.Images[ref]
				if !exists {
					return "", nil
				}
				return image.ID, nil
			}
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
		if a[1] == "rm" {
			if _, ok := d.Images[a[2]]; !ok {
				return "", fmt.Errorf("image missing")
			}
			delete(d.Images, a[2])
			return a[2], nil
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
		data, err := os.ReadFile(flag("--file"))
		if err != nil {
			return "", err
		}
		arguments := map[string]string{}
		for i, arg := range a {
			if arg == "--build-arg" {
				key, value, _ := strings.Cut(a[i+1], "=")
				arguments[key] = value
			}
		}
		stages := map[string][]string{}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
				continue
			}
			base := fields[1]
			for key, value := range arguments {
				base = strings.ReplaceAll(base, "${"+key+"}", value)
				base = strings.ReplaceAll(base, "$"+key, value)
			}
			image.RootFS.Layers = []string{"upstream:" + base}
			if parent, ok := d.Images[base]; ok {
				image.RootFS.Layers = slices.Clone(parent.RootFS.Layers)
			}
			if parent, ok := stages[strings.ToLower(base)]; ok {
				image.RootFS.Layers = slices.Clone(parent)
			}
			if len(fields) >= 4 && strings.EqualFold(fields[2], "AS") {
				stages[strings.ToLower(fields[3])] = slices.Clone(image.RootFS.Layers)
			}
		}
		image.RootFS.Layers = append(image.RootFS.Layers, image.ID)
		d.Images[flag("--tag")] = image
		d.Images[image.ID] = image
		return "", nil
	case "volume":
		if len(a) == 3 && a[1] == "inspect" && d.Volumes[a[2]] {
			return "[]", nil
		}
		return "", fmt.Errorf("volume missing")
	case "create":
		for i, arg := range a {
			if arg == "--volume" {
				source, _, _ := strings.Cut(a[i+1], ":")
				if !strings.HasPrefix(source, "/") {
					d.Volumes[source] = true
				}
			}
		}
		name := flag("--name")
		if _, exists := d.Containers[name]; exists {
			return "", fmt.Errorf("name collision")
		}
		d.Sequence++
		c := docker.Container{ID: fmt.Sprintf("%064x", d.Sequence), Name: "/" + name, Image: a[len(a)-2]}
		c.Config.Labels = labels()
		for i, arg := range a {
			switch arg {
			case "--mount":
				fields, err := csv.NewReader(strings.NewReader(a[i+1])).Read()
				if err != nil {
					return "", err
				}
				var mount docker.ContainerMount
				for _, field := range fields {
					key, value, _ := strings.Cut(field, "=")
					switch key {
					case "type":
						mount.Type = value
					case "src":
						mount.Source = value
					case "dst":
						mount.Destination = value
					}
				}
				c.Mounts = append(c.Mounts, mount)
			case "--volume":
				fields := strings.Split(a[i+1], ":")
				kind := "volume"
				if strings.HasPrefix(fields[0], "/") {
					kind = "bind"
				}
				c.Mounts = append(c.Mounts, docker.ContainerMount{Type: kind, Source: fields[0], Destination: fields[1]})
			}
		}
		c.State.Status = "created"
		c.HostConfig.RestartPolicy.Name = flag("--restart")
		c.HostConfig.NetworkMode = flag("--network")
		if c.HostConfig.NetworkMode == "" {
			c.HostConfig.NetworkMode = "default"
		}
		primary := c.HostConfig.NetworkMode
		if primary == "default" {
			primary = "bridge"
		}
		c.NetworkSettings.Networks = map[string]docker.Endpoint{primary: {NetworkID: primary, IPAddress: "172.20.0.2", Gateway: "172.20.0.1"}}
		d.Containers[name] = c
		return c.ID, nil
	case "update":
		id := a[len(a)-1]
		for name, c := range d.Containers {
			if c.ID == id {
				for _, arg := range a {
					if policy, ok := strings.CutPrefix(arg, "--restart="); ok {
						c.HostConfig.RestartPolicy.Name = policy
					}
				}
				d.Containers[name] = c
				return "", nil
			}
		}
		return "", fmt.Errorf("container missing")
	case "start", "stop", "rm":
		id := a[len(a)-1]
		for name, c := range d.Containers {
			if c.ID == id {
				if a[0] == "rm" {
					if c.State.Running {
						return "", fmt.Errorf("cannot remove running container")
					}
					delete(d.Containers, name)
					delete(d.Hooks, id)
					delete(d.stagedHooks, id)
				} else {
					c.State.Running = a[0] == "start"
					c.State.Status = "exited"
					if c.State.Running {
						c.State.Status = "running"
					}
					d.Containers[name] = c
				}
				return "", nil
			}
		}
		return "", fmt.Errorf("container missing")
	case "exec":
		id := ""
		for _, c := range d.Containers {
			if slices.Contains(a, c.ID) {
				id = c.ID
				break
			}
		}
		if a[len(a)-1] == "dbx-stage-hooks" {
			if d.stagedHooks == nil {
				d.stagedHooks = map[string]map[string][]byte{}
			}
			d.stagedHooks[id] = map[string][]byte{}
		}
		if a[len(a)-1] == "dbx-publish-hooks" {
			if d.Hooks == nil {
				d.Hooks = map[string]map[string][]byte{}
			}
			if d.Hooks[id] == nil {
				d.Hooks[id] = map[string][]byte{}
			}
			for path, data := range d.stagedHooks[id] {
				d.Hooks[id][path] = slices.Clone(data)
			}
			delete(d.stagedHooks, id)
		}
		if index := slices.Index(a, "dbx-hooks"); index >= 0 {
			for _, path := range a[index+1:] {
				if _, ok := d.Hooks[id][path]; !ok {
					return "", fmt.Errorf("applied hook missing: %s", path)
				}
			}
		}
		return "", nil
	case "cp":
		id, destination, _ := strings.Cut(a[2], ":")
		if destination == "/devbox/hooks/.incoming" {
			source := strings.TrimSuffix(a[1], "/.")
			entries, err := os.ReadDir(source)
			if err != nil {
				return "", err
			}
			for _, entry := range entries {
				data, err := os.ReadFile(filepath.Join(source, entry.Name()))
				if err != nil {
					return "", err
				}
				d.stagedHooks[id]["/devbox/hooks/"+entry.Name()] = data
			}
		}
		return "", nil
	case "logs":
		return "container logs\n", nil
	case "network":
		if len(a) == 3 && a[1] == "inspect" {
			return "[]", nil
		}
		if len(a) == 4 && (a[1] == "connect" || a[1] == "disconnect") {
			for name, c := range d.Containers {
				if c.ID == a[3] {
					c.NetworkSettings.Networks = maps.Clone(c.NetworkSettings.Networks)
					if a[1] == "connect" {
						c.NetworkSettings.Networks[a[2]] = docker.Endpoint{NetworkID: a[2], IPAddress: "172.21.0.2", Gateway: "172.21.0.1"}
					} else {
						delete(c.NetworkSettings.Networks, a[2])
					}
					d.Containers[name] = c
					return "", nil
				}
			}
			return "", fmt.Errorf("container missing")
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
	if d.Containers == nil {
		d.Containers = map[string]docker.Container{}
	}
	d.Containers[strings.TrimPrefix(c.Name, "/")] = c
}
