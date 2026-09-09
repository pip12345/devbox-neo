package environment

import (
	"encoding/json"
	"fmt"
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

func (r Report) PendingCreationChanges() []InputChange {
	result := []InputChange{}
	for _, inputChange := range r.PendingInputChanges {
		if inputChange.Scope != RuntimeScope {
			result = append(result, inputChange)
		}
	}
	return result
}

// CompareInputs explains leaf changes, while Compare retains the existing
// image > container > runtime action priority. In particular, the image hash
// feeding the container fingerprint is not another independent change reason.
func CompareInputs(before, after Inputs) Report {
	d := differ{inputChanges: []InputChange{}}
	d.scope = ImageScope
	a, b := before.Image, after.Image
	d.scalar("image_mode", a.Mode, b.Mode)
	d.scalar("harness", a.Harness, b.Harness)
	d.file("harness_definition", "", a.Definition, b.Definition)
	d.file("dockerfile", "", a.Dockerfile, b.Dockerfile)
	d.file("ignore_rules", "", a.Ignore, b.Ignore)
	d.files("build_context", a.Context, b.Context)
	d.opaque("generated_layer", a.Layer, b.Layer)
	for _, key := range inputKeys(a.Arguments, b.Arguments) {
		old, had := a.Arguments[key]
		next, has := b.Arguments[key]
		d.entry("build_argument", key, old, next, had, has, false)
	}

	d.scope = ContainerScope
	c, n := before.Container, after.Container
	d.scalar("workspace", c.Identity.Workspace, n.Identity.Workspace)
	d.scalar("slot", c.Identity.Slot, n.Identity.Slot)
	d.scalar("network", c.Network, n.Network)
	d.scalar("read_only", strconv.FormatBool(c.ReadOnly), strconv.FormatBool(n.ReadOnly))
	d.list("harness_stores", publicEntries(c.Stores), publicEntries(n.Stores))
	d.list("auth_mounts", publicEntries(c.Auth), publicEntries(n.Auth))
	envStart := len(d.inputChanges)
	for _, key := range inputKeys(c.Env, n.Env) {
		old, had := c.Env[key]
		next, has := n.Env[key]
		d.entry("env", key, old, next, had, has, true)
	}
	envChanged := len(d.inputChanges) != envStart
	d.file("setup", "", c.Setup, n.Setup)
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
	d.files("managed_config", r.Files, s.Files)
	d.file("entrypoint", "", r.Entrypoint, s.Entrypoint)
	d.list("launch_args", r.Launch.Args, s.Launch.Args)
	d.list("continue_args", r.Launch.Continue, s.Launch.Continue)
	d.list("harness_args", r.Args, s.Args)
	d.scalar("on_exit", r.OnExit, s.OnExit)
	d.list("shell", r.Shell, s.Shell)
	return Report{Compare(before.Fingerprints(), after.Fingerprints()), d.inputChanges}
}

type differ struct {
	scope        Scope
	inputChanges []InputChange
	imageFiles   map[string]bool
}

func (d *differ) add(r InputChange) {
	r.Scope = d.scope
	// The Dockerfile/ignore file may also be present in the captured build
	// context. Report each physical file change once, retaining the specific label.
	if r.Scope == ImageScope && r.Path != "" && (r.Field == "dockerfile" || r.Field == "ignore_rules" || r.Field == "build_context") {
		key := r
		key.Field, key.Key = "", ""
		hash := Digest(key)
		if d.imageFiles[hash] {
			return
		}
		if d.imageFiles == nil {
			d.imageFiles = map[string]bool{}
		}
		d.imageFiles[hash] = true
	}
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

func (d *differ) files(field string, before, after map[string]FileInput) {
	for _, name := range inputKeys(before, after) {
		d.file(field, name, before[name], after[name])
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
		"dockerfile": "Dockerfile", "ignore_rules": "Docker ignore rules", "build_context": "build context file",
		"generated_layer": "generated Devbox image layer", "build_argument": "build argument",
		"workspace": "workspace", "slot": "slot", "network": "network", "read_only": "read-only",
		"harness_stores": "harness stores", "auth_mounts": "auth mounts", "env": "environment variable",
		"setup": "setup.sh", "mounts": "mounts", "ports": "ports", "docker_args": "Docker arguments",
		"docker_env": "Docker environment arguments", "metadata": "IDE metadata", "host_alias": "host alias",
		"runtime_assets": "bundled runtime guidance", "managed_config": "managed config file", "entrypoint": "entrypoint.sh",
		"launch_args": "harness launch arguments", "continue_args": "continuation arguments", "harness_args": "harness arguments",
		"on_exit": "on-exit", "shell": "shell",
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
