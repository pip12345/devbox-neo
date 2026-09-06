package app

import (
	"context"
	"fmt"
	"sort"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/store"
)

type NetworkFacts struct {
	Name     string                     `json:"name"`
	Mode     string                     `json:"mode"`
	Primary  string                     `json:"primary"`
	Host     string                     `json:"host"`
	Gateway  string                     `json:"gateway"`
	Networks map[string]docker.Endpoint `json:"networks"`
}

func networkFacts(r store.Record, c docker.Container) NetworkFacts {
	primary := r.Creation.Network
	if primary == "default" {
		primary = "bridge"
	}
	for name, endpoint := range c.NetworkSettings.Networks {
		if endpoint.NetworkID == primary {
			primary = name
			break
		}
	}
	host := "host.docker.internal"
	if r.Creation.Network == "host" {
		host = "127.0.0.1"
	}
	return NetworkFacts{Name: r.Identity.Name, Mode: r.Creation.Network, Primary: primary, Host: host, Gateway: c.NetworkSettings.Networks[primary].Gateway, Networks: c.NetworkSettings.Networks}
}
func (f NetworkFacts) Env() map[string]string {
	return map[string]string{"DEVBOX_HOST": f.Host, "DEVBOX_NETWORK": f.Mode, "DEVBOX_PRIMARY_NETWORK": f.Primary, "DEVBOX_DEFAULT_GATEWAY_IP": f.Gateway}
}
func (e *Engine) NetworkFacts(ctx context.Context, target, profile string) (NetworkFacts, error) {
	r, err := e.Locate(ctx, target, profile)
	if err != nil {
		return NetworkFacts{}, err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return NetworkFacts{}, err
	}
	if !exists {
		return NetworkFacts{}, fmt.Errorf("container is missing")
	}
	return networkFacts(r, c), nil
}
func (e *Engine) ChangeNetwork(ctx context.Context, target, profile, name string, connect bool) error {
	if !config.NetworkName.MatchString(name) || name == "default" || name == "host" {
		return fmt.Errorf("select an existing secondary Docker network by name")
	}
	r, err := e.Locate(ctx, target, profile)
	if err != nil {
		return err
	}
	lock, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return err
	}
	defer lock.Close()
	r, err = lock.Load()
	if err != nil {
		return err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("container is missing")
	}
	if r.Creation.Network == "host" || c.HostConfig.NetworkMode == "host" {
		return fmt.Errorf("host-network containers cannot attach secondary networks")
	}
	facts := networkFacts(r, c)
	actual := name
	for candidate, endpoint := range facts.Networks {
		if endpoint.NetworkID == name {
			actual = candidate
			break
		}
	}
	if !connect && actual == facts.Primary {
		return fmt.Errorf("cannot disconnect the primary network; change configuration and recreate")
	}
	_, attached := facts.Networks[actual]
	if connect && attached {
		return nil
	}
	if !connect && !attached {
		return fmt.Errorf("network is not attached")
	}
	if err = e.Docker.Network(ctx, name); err != nil {
		return err
	}
	if err = e.Docker.AttachNetwork(ctx, c, e.owner(r), name, connect); err != nil {
		return err
	}
	if c.State.Running {
		return e.installRuntime(ctx, r)
	}
	return nil
}
func sortedEnv(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
