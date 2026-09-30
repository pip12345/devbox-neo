package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Inventory batches live inspection. Names alone never select unrelated Docker
// resources, and callers still verify each session association before mutation.
func (r Runtime) Inventory(ctx context.Context, installation string) ([]Container, error) {
	if installation == "" {
		return nil, fmt.Errorf("installation identity is required")
	}
	containers, err := r.containerInventory(ctx, "--filter", "label="+Namespace+".managed=true", "--filter", "label="+Namespace+".installation="+installation)
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		if c.Config.Labels[Namespace+".installation"] != installation || c.Config.Labels[Namespace+".managed"] != "true" {
			return nil, fmt.Errorf("container inventory changed during inspection")
		}
	}
	return containers, nil
}

// AllContainers is read-only discovery for backing-path checks. An unmanaged
// container can bind the same files, so installation-filtered inventory cannot
// prove that an incomplete directory is unused. This grants no mutation rights.
func (r Runtime) AllContainers(ctx context.Context) ([]Container, error) {
	return r.containerInventory(ctx)
}

func (r Runtime) containerInventory(ctx context.Context, filters ...string) ([]Container, error) {
	args := append([]string{"container", "ls", "--all", "--no-trunc"}, filters...)
	args = append(args, "--format", "{{.ID}}")
	b, err := r.capture(ctx, args...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(b))
	if len(ids) == 0 {
		return []Container{}, nil
	}
	sort.Strings(ids)
	b, err = r.capture(ctx, append([]string{"container", "inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	var containers []Container
	if err = json.Unmarshal(b, &containers); err != nil {
		return nil, fmt.Errorf("invalid container inventory")
	}
	expected := map[string]bool{}
	for _, id := range ids {
		expected[id] = true
	}
	for _, c := range containers {
		if !expected[c.ID] {
			return nil, fmt.Errorf("container inventory changed during inspection")
		}
		delete(expected, c.ID)
	}
	if len(expected) != 0 {
		return nil, fmt.Errorf("incomplete container inventory")
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return containers, nil
}
func (r Runtime) TaggedImage(ctx context.Context, tag string) (Image, bool, error) {
	b, err := r.capture(ctx, "image", "ls", "--no-trunc", "--filter", "reference="+tag, "--format", "{{.ID}}")
	if err != nil {
		return Image{}, false, err
	}
	ids := strings.Fields(string(b))
	if len(ids) == 0 {
		return Image{}, false, nil
	}
	if len(ids) != 1 {
		return Image{}, false, fmt.Errorf("ambiguous image tag")
	}
	image, err := r.InspectImage(ctx, tag)
	if err != nil {
		return image, false, err
	}
	if image.ID != ids[0] {
		return image, false, fmt.Errorf("image tag changed during inspection")
	}
	return image, true, nil
}

func (r Runtime) Logs(ctx context.Context, c Container, o Owner, follow bool, tail string, out, stderr io.Writer) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	args := []string{"logs", "--tail", tail}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, c.ID)
	return r.Runner.Run(ctx, Command{Args: args, Stdout: out, Stderr: stderr})
}
func (r Runtime) AttachNetwork(ctx context.Context, c Container, o Owner, name string, connect bool) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	verb := "disconnect"
	if connect {
		verb = "connect"
	}
	_, err := r.capture(ctx, "network", verb, name, c.ID)
	return err
}
