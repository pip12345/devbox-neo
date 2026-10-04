package environment

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type Scope string

const (
	ImageScope     Scope = "image"
	ContainerScope Scope = "container"
	RuntimeScope   Scope = "runtime"
)

// InputChange contains only deliberately selected public values. Hashes and
// file contents never enter reports; secret-bearing changes identify names only.
type InputChange struct {
	Scope  Scope   `json:"scope"`
	Code   string  `json:"code"`
	Field  string  `json:"field"`
	Key    string  `json:"key,omitempty"`
	Path   string  `json:"path,omitempty"`
	Before *string `json:"before,omitempty"`
	After  *string `json:"after,omitempty"`
}

type Report struct {
	Change              Change        `json:"change"`
	PendingInputChanges []InputChange `json:"pending_input_changes"`
}

// CompareInputs explains public settings and content categories, while Compare
// owns the image > container > runtime action priority. The image hash feeding
// the container fingerprint is not another independent change reason.
func CompareInputs(before, after Inputs) Report {
	d := differ{inputChanges: []InputChange{}}
	d.scope = ImageScope
	a, b := before.Image, after.Image
	d.scalar("base_image", a.BaseImage, b.BaseImage)
	d.scalar("harness", a.Harness, b.Harness)
	d.file("harness_definition", "", a.Definition, b.Definition)
	for i := 0; i < max(len(a.Stages), len(b.Stages)); i++ {
		var old, next BuildInputs
		if i < len(a.Stages) {
			old = a.Stages[i]
		}
		if i < len(b.Stages) {
			next = b.Stages[i]
		}
		key := strconv.Itoa(i + 1)
		d.file("dockerfile", key, old.Dockerfile, next.Dockerfile)
		d.file("ignore_rules", key, old.Ignore, next.Ignore)
		if old.Context != next.Context {
			source := next.Dockerfile.Source
			if source == "" {
				source = old.Dockerfile.Source
			}
			if source != "" {
				source = filepath.Dir(source)
			}
			d.add(InputChange{Code: "input_changed", Field: "build_context", Key: key, Path: source})
		}
	}
	d.opaque("prepared_layer", a.Prepared, b.Prepared)
	d.opaque("boundary_layer", a.Boundary, b.Boundary)
	d.opaque("generated_layer", a.Layer, b.Layer)
	for _, key := range inputKeys(a.Arguments, b.Arguments) {
		old, had := a.Arguments[key]
		next, has := b.Arguments[key]
		d.entry("build_argument", key, old, next, had, has, false)
	}

	d.scope = ContainerScope
	c, n := before.Container, after.Container
	d.scalar("workspace", c.Workspace, n.Workspace)
	d.scalar("network", c.Network, n.Network)
	d.list("harness_stores", publicEntries(c.Stores), publicEntries(n.Stores))
	d.list("auth_mounts", publicEntries(c.Auth), publicEntries(n.Auth))
	envStart := len(d.inputChanges)
	for _, key := range inputKeys(c.Env, n.Env) {
		old, had := c.Env[key]
		next, has := n.Env[key]
		d.entry("env", key, old, next, had, has, true)
	}
	envChanged := len(d.inputChanges) != envStart
	d.hooks("setup", c.Setup, n.Setup)
	d.list("mounts", publicEntries(c.Mounts), publicEntries(n.Mounts))
	d.list("ports", c.Ports, n.Ports)
	d.list("docker_args", c.RawArgs, n.RawArgs)
	if c.RawArgsHash != n.RawArgsHash && reflect.DeepEqual(c.RawArgs, n.RawArgs) && !envChanged {
		// A shadowed/duplicate raw env assignment can change without changing the
		// final environment. The ordered argument contract still needs recreation.
		d.opaque("docker_env", c.RawArgsHash, n.RawArgsHash)
	}
	d.opaque("metadata", c.Metadata, n.Metadata)
	d.scalar("host_alias", c.HostAlias, n.HostAlias)

	d.scope = RuntimeScope
	r, s := before.Runtime, after.Runtime
	d.opaque("runtime_assets", r.Assets, s.Assets)
	d.opaque("managed_config", r.Files, s.Files)
	d.hooks("before_open", r.BeforeOpen, s.BeforeOpen)
	d.list("launch_args", r.Launch.Args, s.Launch.Args)
	d.list("continue_args", r.Launch.Continue, s.Launch.Continue)
	d.list("harness_args", r.Args, s.Args)
	d.list("shell", r.Shell, s.Shell)
	return Report{Compare(before.Fingerprints(), after.Fingerprints()), d.inputChanges}
}

type differ struct {
	scope        Scope
	inputChanges []InputChange
}

func (d *differ) add(r InputChange) {
	r.Scope = d.scope
	d.inputChanges = append(d.inputChanges, r)
}

func (d *differ) scalar(field, before, after string) {
	if before != after {
		d.add(InputChange{Code: "value_changed", Field: field, Before: &before, After: &after})
	}
}

func (d *differ) opaque(field, before, after string) {
	if before != after {
		d.add(InputChange{Code: "input_changed", Field: field})
	}
}

func (d *differ) entry(field, key, before, after string, had, has, secret bool) {
	if had && has && before == after {
		return
	}
	r := InputChange{Code: "value_changed", Field: field, Key: key}
	if !had {
		r.Code = "entry_added"
	} else if !has {
		r.Code = "entry_removed"
	}
	if !secret {
		if had {
			r.Before = &before
		}
		if has {
			r.After = &after
		}
	}
	d.add(r)
}

func (d *differ) list(field string, before, after []string) {
	if reflect.DeepEqual(before, after) {
		return
	}
	oldCounts, newCounts := map[string]int{}, map[string]int{}
	for _, value := range before {
		oldCounts[value]++
	}
	for _, value := range after {
		newCounts[value]++
	}
	start := len(d.inputChanges)
	for _, value := range inputKeys(oldCounts, newCounts) {
		for i := newCounts[value]; i < oldCounts[value]; i++ {
			d.add(InputChange{Code: "entry_removed", Field: field, Before: &value})
		}
		for i := oldCounts[value]; i < newCounts[value]; i++ {
			d.add(InputChange{Code: "entry_added", Field: field, After: &value})
		}
	}
	if len(d.inputChanges) == start {
		code := "order_changed"
		if len(before) == 0 && len(after) == 0 {
			code = "input_changed"
		}
		d.add(InputChange{Code: code, Field: field})
	}
}

func (d *differ) hooks(field string, before, after []FileInput) {
	for i := 0; i < max(len(before), len(after)); i++ {
		var old, next FileInput
		if i < len(before) {
			old = before[i]
		}
		if i < len(after) {
			next = after[i]
		}
		d.file(field, strconv.Itoa(i+1), old, next)
	}
}

func (d *differ) file(field, key string, before, after FileInput) {
	if before.FileState == after.FileState {
		return
	}
	p := after.Source
	if p == "" {
		p = before.Source
	}
	r := InputChange{Field: field, Key: key, Path: p}
	switch {
	case before.Hash == "":
		r.Code = "file_added"
		d.add(r)
	case after.Hash == "":
		r.Code = "file_removed"
		d.add(r)
	default:
		if before.Directory != after.Directory {
			r.Code = "file_kind_changed"
			d.add(r)
		}
		if before.Hash != after.Hash {
			r.Code = "file_content_changed"
			d.add(r)
		}
		if before.Mode != after.Mode {
			r.Code = "file_mode_changed"
			old, next := fmt.Sprintf("%04o", before.Mode), fmt.Sprintf("%04o", after.Mode)
			r.Before, r.After = &old, &next
			d.add(r)
		}
	}
}

func inputKeys[V any](before, after map[string]V) []string {
	keys := make([]string, 0, len(before)+len(after))
	for key := range before {
		keys = append(keys, key)
	}
	for key := range after {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return slices.Compact(keys)
}

func publicEntries[T any](values []T) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for i, value := range values {
		b, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		result[i] = string(b)
	}
	return result
}

func displayInputValue(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r < 32 || r >= 127 && r < 160 }) >= 0 || s == "" {
		return strconv.QuoteToASCII(s)
	}
	return s
}

func (r InputChange) String() string {
	labels := map[string]string{
		"image_mode": "image build mode", "harness": "harness", "harness_definition": "harness definition",
		"dockerfile": "Dockerfile", "ignore_rules": "Docker ignore rules", "build_context": "build context",
		"generated_layer": "generated Devbox image layer", "build_argument": "build argument",
		"workspace": "workspace", "slot": "slot", "network": "network",
		"harness_stores": "harness stores", "auth_mounts": "auth mounts", "env": "environment variable",
		"setup": "setup.sh", "mounts": "mounts", "ports": "ports", "docker_args": "Docker arguments",
		"docker_env": "Docker environment arguments", "metadata": "IDE metadata", "host_alias": "host alias",
		"runtime_assets": "bundled runtime guidance", "managed_config": "managed configuration", "before_open": "before-open.sh",
		"launch_args": "harness launch arguments", "continue_args": "continuation arguments", "harness_args": "harness arguments",
		"shell": "shell",
	}
	label := labels[r.Field]
	if label == "" {
		label = displayInputValue(r.Field)
	}
	if r.Key != "" && (r.Path == "" || r.Field == "managed_config") {
		label += " " + displayInputValue(r.Key)
	}
	if r.Path != "" {
		label += " (" + displayInputValue(r.Path) + ")"
	}
	switch r.Code {
	case "entry_added", "file_added":
		label += " added"
		if r.After != nil {
			label += ": " + displayInputValue(*r.After)
		}
	case "entry_removed", "file_removed":
		label += " removed"
		if r.Before != nil {
			label += ": " + displayInputValue(*r.Before)
		}
	case "order_changed":
		label += " reordered"
	case "file_kind_changed":
		label += " file/directory kind changed"
	default:
		if r.Code == "file_mode_changed" {
			label += " permissions"
		}
		if r.Before != nil && r.After != nil {
			label += ": " + displayInputValue(*r.Before) + " -> " + displayInputValue(*r.After)
		} else {
			label += " changed"
		}
	}
	return label
}
