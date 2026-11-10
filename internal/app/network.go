package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"devbox/internal/commanderror"
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
	primary := r.Applied.Creation.Network
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
	if r.Applied.Creation.Network == "host" {
		host = "127.0.0.1"
	}
	return NetworkFacts{Name: r.ID, Mode: r.Applied.Creation.Network, Primary: primary, Host: host, Gateway: c.NetworkSettings.Networks[primary].Gateway, Networks: c.NetworkSettings.Networks}
}
func (f NetworkFacts) Env() map[string]string {
	return map[string]string{"DEVBOX_HOST": f.Host, "DEVBOX_NETWORK": f.Mode, "DEVBOX_PRIMARY_NETWORK": f.Primary, "DEVBOX_DEFAULT_GATEWAY_IP": f.Gateway}
}
func (e *Engine) NetworkFacts(ctx context.Context, target, localName string) (NetworkFacts, error) {
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return NetworkFacts{}, err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return NetworkFacts{}, err
	}
	if !exists {
		return NetworkFacts{}, commanderror.New("container_missing", "Container not found; network details are unavailable.", r.PublicName(), nil,
			commanderror.Next("Recreate from current configuration", "recreate", r.PublicName()))
	}
	return networkFacts(r, c), nil
}
func validateSecondaryNetwork(name string) error {
	if !config.NetworkName.MatchString(name) || name == "default" || name == "host" {
		return fmt.Errorf("select an existing secondary Docker network by name")
	}
	return nil
}

func (e *Engine) ChangeNetwork(ctx context.Context, target, localName, name string, connect bool) error {
	if err := validateSecondaryNetwork(name); err != nil {
		return err
	}
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return err
	}
	lock, err := e.Store.Lock(ctx, r.ID)
	if err != nil {
		return err
	}
	defer lock.Close()
	r, err = loadSelected(lock, r)
	if err != nil {
		return err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	_, err = e.changeNetwork(ctx, lock, r, c, exists, name, connect)
	return err
}

func (e *Engine) changeNetwork(ctx context.Context, lock *store.Locked, r store.Record, c docker.Container, exists bool, name string, connect bool) (BatchStatus, error) {
	if !exists && !connect && slices.Contains(r.Settings.SecondaryNetworks, name) {
		return BatchSucceeded, saveSecondaryNetwork(lock, r, name, false)
	}
	if !exists {
		return "", commanderror.New("container_missing", "Container not found.", r.PublicName(), nil,
			commanderror.Next("Recreate from current configuration", "recreate", r.PublicName()))
	}
	if r.Applied.Creation.Network == "host" || c.HostConfig.NetworkMode == "host" {
		return "", commanderror.New("network_host_mode", "Host-network containers cannot attach secondary networks.", r.PublicName(), nil)
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
		return "", commanderror.New("primary_network_protected", "Cannot disconnect the primary network. Change its configuration first.", r.PublicName(), nil,
			commanderror.Next("Then recreate to apply the network change", "recreate", r.PublicName()))
	}
	if connect && actual == facts.Primary {
		return BatchUnchanged, nil
	}
	_, attached := facts.Networks[actual]
	changed := attached != connect || slices.Contains(r.Settings.SecondaryNetworks, actual) != connect
	if attached != connect {
		if connect {
			if err := e.Docker.Network(ctx, name); err != nil {
				return "", err
			}
		}
		if err := e.Docker.AttachNetwork(ctx, c, e.owner(r), name, connect); err != nil {
			return "", err
		}
		if connect {
			// Docker accepts network IDs too. Save the actual name so a later
			// replacement uses the same naming contract as config networks.
			live, present, inspectErr := e.inspect(ctx, r)
			if inspectErr != nil {
				return "", inspectErr
			}
			if !present {
				return "", fmt.Errorf("container disappeared after attaching network")
			}
			for candidate, endpoint := range live.NetworkSettings.Networks {
				if endpoint.NetworkID == name {
					actual = candidate
					break
				}
			}
		}
	}
	if err := saveSecondaryNetwork(lock, r, actual, connect); err != nil {
		verb := "disconnect"
		if connect {
			verb = "connect"
		}
		return "", commanderror.New("network_save_failed", "The Docker attachment is updated, but saving the network choice failed. Retry the command.", r.PublicName(), err,
			commanderror.Next("Save network choice", "network", verb, actual, r.PublicName()))
	}
	if c.State.Running {
		if err := e.refreshNetwork(ctx, r); err != nil {
			return "", commanderror.New("network_refresh_failed", "The network attachment change is applied and saved, but refreshing network information inside the container failed. Accessing the session will retry the refresh.", r.PublicName(), err,
				commanderror.Next("Inspect current network attachments", "network", "inspect", r.PublicName()))
		}
	}
	if changed {
		return BatchSucceeded, nil
	}
	return BatchUnchanged, nil
}

func (e *Engine) NetworkFactsAll(ctx context.Context) (BatchReport[NetworkFacts], error) {
	targets, err := e.selectSavedSessions(ctx)
	if err != nil {
		return BatchReport[NetworkFacts]{}, err
	}
	b := newBatch[NetworkFacts](targets)
	b.run(ctx, func(ctx context.Context, target SelectedTarget, item *BatchResult[NetworkFacts]) error {
		lock, r, err := e.lockSelectedSession(ctx, target)
		if err != nil {
			return err
		}
		defer lock.Close()
		item.Session = r.PublicName()
		c, exists, err := e.inspect(ctx, r)
		if err != nil {
			return err
		}
		if !exists {
			item.Status, item.Reason = BatchSkipped, "Container not found."
			return nil
		}
		facts := networkFacts(r, c)
		item.Result = &facts
		return nil
	})
	return b.report, b.err(ctx)
}

func (e *Engine) ChangeNetworkAll(ctx context.Context, name string, connect bool) (BatchReport[NetworkFacts], error) {
	if err := validateSecondaryNetwork(name); err != nil {
		return BatchReport[NetworkFacts]{}, err
	}
	targets, err := e.selectSavedSessions(ctx)
	if err != nil {
		return BatchReport[NetworkFacts]{}, err
	}
	b := newBatch[NetworkFacts](targets)
	b.run(ctx, func(ctx context.Context, target SelectedTarget, item *BatchResult[NetworkFacts]) error {
		lock, r, err := e.lockSelectedSession(ctx, target)
		if err != nil {
			return err
		}
		defer lock.Close()
		item.Session = r.PublicName()
		c, exists, err := e.inspect(ctx, r)
		if err != nil {
			return err
		}
		if !exists {
			item.Status, item.Reason = BatchSkipped, "Container not found."
			return nil
		}
		item.Status, err = e.changeNetwork(ctx, lock, r, c, true, name, connect)
		var restriction *commanderror.Error
		if errors.As(err, &restriction) && (restriction.Code == "network_host_mode" || restriction.Code == "primary_network_protected") {
			item.Status, item.Reason = BatchSkipped, restriction.Message
			return nil
		}
		return err
	})
	return b.report, b.err(ctx)
}

// A recreated network can keep its name while Docker's saved endpoint still
// refers to the removed ID. Diagnose actual endpoints, not desired config or
// names alone, and only after startup fails so healthy access stays independent.
func (e *Engine) diagnoseStartNetworks(ctx context.Context, c docker.Container, r store.Record, startErr error) error {
	if ctx.Err() != nil {
		return startErr
	}
	names := make([]string, 0, len(c.NetworkSettings.Networks))
	for name := range c.NetworkSettings.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		id := c.NetworkSettings.Networks[name].NetworkID
		if id == "" {
			id = name
		}
		if err := e.Docker.Network(ctx, id); err != nil {
			if ctx.Err() != nil {
				return startErr
			}
			return commanderror.New("container_network_unavailable", fmt.Sprintf("Container could not start, and its attached Docker network %q cannot be inspected. If the network was removed or replaced, restore it or update network choices, then recreate the container.", name), r.PublicName(), errors.Join(startErr, err),
				commanderror.Step{Command: []string{"docker", "network", "inspect", id}, Reason: "Check the attached Docker network"},
				commanderror.Next("After fixing network choices, recreate the container (container-local files are lost)", "recreate", "--container", r.PublicName()))
		}
	}
	return startErr
}

func saveSecondaryNetwork(lock *store.Locked, r store.Record, name string, connect bool) error {
	if slices.Contains(r.Settings.SecondaryNetworks, name) == connect {
		return nil
	}
	if connect {
		r.Settings.SecondaryNetworks = append(slices.Clone(r.Settings.SecondaryNetworks), name)
	} else {
		r.Settings.SecondaryNetworks = slices.DeleteFunc(slices.Clone(r.Settings.SecondaryNetworks), func(n string) bool { return n == name })
	}
	r.Action, r.Activity = "network", time.Now().UTC()
	return lock.Save(r)
}

// Only explicit dbx choices authorize restoration; live Docker attachments are
// facts, not instructions to make a network part of the saved session.
func (e *Engine) preflightSecondaryNetworks(ctx context.Context, r store.Record, primary string) error {
	for _, name := range r.Settings.SecondaryNetworks {
		if primary == "host" {
			return commanderror.New("secondary_network_conflict", "Disconnect saved secondary networks before applying host networking.", r.PublicName(), nil,
				commanderror.Next("Forget secondary network", "network", "disconnect", name, r.PublicName()))
		}
		if err := e.Docker.Network(ctx, name); err != nil {
			return commanderror.New("secondary_network_missing", "A saved secondary network is unavailable. Restore it or explicitly disconnect it before recreating.", r.PublicName(), err,
				commanderror.Next("Forget secondary network", "network", "disconnect", name, r.PublicName()))
		}
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
