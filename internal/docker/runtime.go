package docker

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

const Namespace = "devbox-rewrite"
const HostAlias = "host.docker.internal"

type Owner struct {
	Installation string
	Session      string
	Workspace    string
	Slot         string
}

func (o Owner) Labels() map[string]string {
	return map[string]string{Namespace + ".managed": "true", Namespace + ".ownership": "1", Namespace + ".installation": o.Installation, Namespace + ".session": o.Session, Namespace + ".workspace": o.Workspace, Namespace + ".slot": o.Slot}
}
func ImageLabels(installation string) map[string]string {
	return map[string]string{Namespace + ".managed": "true", Namespace + ".ownership": "1", Namespace + ".installation": installation}
}

type Container struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Image string `json:"Image"`
	State struct {
		Running  bool   `json:"Running"`
		Status   string `json:"Status"`
		ExitCode int    `json:"ExitCode"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]Endpoint `json:"Networks"`
	} `json:"NetworkSettings"`
}

type Endpoint struct {
	IPAddress string `json:"IPAddress"`
	Gateway   string `json:"Gateway"`
	NetworkID string `json:"NetworkID"`
}

func (c Container) Verify(o Owner) error {
	if c.ID == "" || o.Installation == "" || o.Session == "" || o.Workspace == "" || o.Slot == "" {
		return fmt.Errorf("incomplete container ownership proof")
	}
	for key, value := range o.Labels() {
		if c.Config.Labels[key] != value {
			return fmt.Errorf("container ownership mismatch (%s)", key)
		}
	}
	return nil
}

type Image struct {
	ID     string `json:"Id"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func (i Image) Verify(installation string) error {
	if i.ID == "" || installation == "" {
		return fmt.Errorf("incomplete image ownership proof")
	}
	for k, v := range ImageLabels(installation) {
		if i.Config.Labels[k] != v {
			return fmt.Errorf("image ownership mismatch (%s)", k)
		}
	}
	return nil
}

type Mount struct {
	Kind     string   `json:"kind,omitempty"`
	Options  []string `json:"options,omitempty"`
	File     bool     `json:"file,omitempty"`
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	ReadOnly bool     `json:"read_only"`
}
type CreatePlan struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Network  string   `json:"network"`
	Mounts   []Mount  `json:"mounts"`
	Env      []string `json:"-"`
	Ports    []string `json:"ports,omitempty"`
	RawArgs  []string `json:"docker_args,omitempty"`
	Metadata string   `json:"devcontainer_metadata,omitempty"`
}
type BuildPlan struct {
	Directory    string
	Dockerfile   string
	Tag          string
	NoCache      bool
	Installation string
	Arguments    map[string]string
}
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	TTY bool
}
type Runtime struct{ Runner Runner }

func (r Runtime) capture(ctx context.Context, args ...string) ([]byte, error) {
	var out bytes.Buffer
	err := r.Runner.Run(ctx, Command{Args: args, Stdout: &out})
	return out.Bytes(), err
}
func (r Runtime) Inspect(ctx context.Context, name string) (Container, bool, error) {
	var c Container
	// An empty successful inventory proves absence. A failing inspect alone cannot
	// distinguish a missing container from an unavailable daemon or denied access.
	b, err := r.capture(ctx, "container", "ls", "--all", "--filter", "name=^/"+regexp.QuoteMeta(name)+"$", "--format", "{{.ID}}")
	if err != nil {
		return c, false, err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return c, false, nil
	}
	if strings.ContainsAny(id, "\r\n ") {
		return c, false, fmt.Errorf("ambiguous container inventory")
	}
	b, err = r.capture(ctx, "container", "inspect", id)
	if err != nil {
		return c, false, err
	}
	var list []Container
	if err = json.Unmarshal(b, &list); err != nil {
		return c, false, fmt.Errorf("invalid container inspection")
	}
	if len(list) != 1 || strings.TrimPrefix(list[0].Name, "/") != name || list[0].ID == "" {
		return c, false, fmt.Errorf("container identity changed during inspection")
	}
	return list[0], true, nil
}
func (r Runtime) ImageAvailable(ctx context.Context, id string) (bool, error) {
	b, err := r.capture(ctx, "image", "ls", "--all", "--no-trunc", "--quiet")
	if err != nil {
		return false, err
	}
	for _, present := range strings.Fields(string(b)) {
		if present == id {
			return true, nil
		}
	}
	return false, nil
}
func (r Runtime) InspectImage(ctx context.Context, ref string) (Image, error) {
	var image Image
	b, err := r.capture(ctx, "image", "inspect", ref)
	if err != nil {
		return image, err
	}
	var list []Image
	if err = json.Unmarshal(b, &list); err != nil || len(list) != 1 {
		return image, fmt.Errorf("invalid image inspection")
	}
	return list[0], nil
}
func (r Runtime) Build(ctx context.Context, p BuildPlan, out io.Writer) (Image, error) {
	args := []string{"build", "--file", p.Dockerfile, "--tag", p.Tag}
	for _, key := range sortedKeys(p.Arguments) {
		args = append(args, "--build-arg", key+"="+p.Arguments[key])
	}
	if p.NoCache {
		args = append(args, "--no-cache")
	}
	labels := ImageLabels(p.Installation)
	for _, k := range sortedKeys(labels) {
		args = append(args, "--label", k+"="+labels[k])
	}
	args = append(args, p.Directory)
	if err := r.Runner.Run(ctx, Command{Args: args, Stdout: out, Stderr: out}); err != nil {
		return Image{}, err
	}
	image, err := r.InspectImage(ctx, p.Tag)
	if err != nil {
		return image, err
	}
	return image, image.Verify(p.Installation)
}
func (r Runtime) Untag(ctx context.Context, tag, expectedID, installation string) error {
	image, err := r.InspectImage(ctx, tag)
	if err != nil {
		return err
	}
	if err = image.Verify(installation); err != nil {
		return err
	}
	if image.ID != expectedID {
		return fmt.Errorf("image tag no longer points to its recorded image")
	}
	_, err = r.capture(ctx, "image", "rm", tag)
	return err
}

func (r Runtime) Network(ctx context.Context, name string) error {
	if name == "default" || name == "host" {
		return nil
	}
	_, err := r.capture(ctx, "network", "inspect", name)
	return err
}
func (r Runtime) Volume(ctx context.Context, name string) error {
	_, err := r.capture(ctx, "volume", "inspect", name)
	return err
}

func (r Runtime) Create(ctx context.Context, p CreatePlan, o Owner) (string, error) {
	if err := ValidateEnv(p.Env); err != nil {
		return "", err
	}
	args := []string{"create", "--name", p.Name, "--init", "--user", "devuser", "--workdir", "/workspace"}
	labels := o.Labels()
	for _, k := range sortedKeys(labels) {
		args = append(args, "--label", k+"="+labels[k])
	}
	if p.Network != "default" {
		args = append(args, "--network", p.Network)
	}
	if p.Network != "host" {
		args = append(args, "--add-host", HostAlias+":host-gateway")
	}
	for _, m := range p.Mounts {
		if m.Kind == "volume" || len(m.Options) > 0 {
			value := m.Source + ":" + m.Target
			options := append([]string(nil), m.Options...)
			if m.ReadOnly && !strings.Contains(","+strings.Join(options, ",")+",", ",ro,") {
				options = append(options, "ro")
			}
			if len(options) > 0 {
				value += ":" + strings.Join(options, ",")
			}
			args = append(args, "--volume", value)
			continue
		}
		if strings.ContainsRune(m.Source+m.Target, '\x00') {
			return "", fmt.Errorf("mount path contains NUL")
		}
		fields := []string{"type=bind", "src=" + m.Source, "dst=" + m.Target}
		if m.ReadOnly {
			fields = append(fields, "readonly")
		}
		var encoded strings.Builder
		writer := csv.NewWriter(&encoded)
		if err := writer.Write(fields); err != nil {
			return "", err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return "", err
		}
		args = append(args, "--mount", strings.TrimSuffix(encoded.String(), "\n"))
	}
	if len(p.Env) > 0 {
		// A private file avoids both argv disclosure and contaminating the host
		// Docker client's environment with container keys such as DOCKER_HOST.
		file, err := os.CreateTemp("", Namespace+"-env-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.WriteString(strings.Join(p.Env, "\n") + "\n")
		closeErr := file.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		args = append(args, "--env-file", file.Name())
	}
	for _, port := range p.Ports {
		args = append(args, "--publish", port)
	}
	if p.Metadata != "" {
		args = append(args, "--label", "devcontainer.metadata="+p.Metadata)
	}
	args = append(args, p.RawArgs...)
	args = append(args, "--entrypoint", "/bin/sleep", p.Image, "infinity")
	b, err := r.capture(ctx, args...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", fmt.Errorf("Docker create returned no container ID")
	}
	return id, nil
}
func (r Runtime) Start(ctx context.Context, c Container, o Owner) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	_, err := r.capture(ctx, "start", c.ID)
	return err
}
func (r Runtime) Stop(ctx context.Context, c Container, o Owner) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	_, err := r.capture(ctx, "stop", "--time", "10", c.ID)
	return err
}
func (r Runtime) Remove(ctx context.Context, c Container, o Owner) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	_, err := r.capture(ctx, "rm", c.ID)
	return err
}
func (r Runtime) Exec(ctx context.Context, c Container, o Owner, argv []string, s Streams) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	if len(argv) == 0 {
		return fmt.Errorf("empty exec argv")
	}
	args := []string{"exec", "--user", "devuser", "--workdir", "/workspace"}
	if s.In != nil {
		args = append(args, "--interactive")
	}
	if s.TTY {
		args = append(args, "--tty")
	}
	args = append(args, c.ID)
	args = append(args, argv...)
	return r.Runner.Run(ctx, Command{Args: args, Stdin: s.In, Stdout: s.Out, Stderr: s.Err})
}
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
