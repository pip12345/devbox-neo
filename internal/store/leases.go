package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
)

type Process struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	Boot  string `json:"boot"`
}
type Lease struct {
	Version int       `json:"version"`
	ID      string    `json:"id"`
	Process Process   `json:"process"`
	Action  string    `json:"action"`
	Created time.Time `json:"created_at"`
}

// ProcessIdentity uses kernel start ticks and boot identity, not kill(pid, 0):
// a reused PID or a machine reboot must not keep an abandoned lease alive.
func ProcessIdentity(pid int) (Process, error) {
	var p Process
	p.PID = pid
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return p, err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return p, fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 20 {
		return p, fmt.Errorf("incomplete process identity")
	}
	if fields[0] == "Z" {
		return p, os.ErrNotExist
	}
	if _, err = strconv.ParseUint(fields[19], 10, 64); err != nil {
		return p, fmt.Errorf("invalid process start ticks")
	}
	p.Start = fields[19]
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return p, err
	}
	p.Boot = strings.TrimSpace(string(boot))
	if p.Boot == "" {
		return p, fmt.Errorf("missing boot identity")
	}
	return p, nil
}
func (l *Locked) Lease(action string) (Lease, error) {
	var lease Lease
	p, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return lease, err
	}
	id, err := fsutil.ID()
	if err != nil {
		return lease, err
	}
	dir, err := l.leaseDirectory(true)
	if err != nil {
		return lease, err
	}
	lease = Lease{Version: 2, ID: id, Process: p, Action: action, Created: time.Now().UTC()}
	return lease, fsutil.JSON(filepath.Join(dir, id+".json"), lease)
}

// Release reports whether this attachment still owned its lease. Forced
// replacement removes old leases, so their later cleanup must not touch the
// replacement's activity or automatic lifetime.
func (l *Locked) Release(id string) (bool, error) {
	if !idPattern.MatchString(id) {
		return false, fmt.Errorf("invalid lease ID")
	}
	p, err := l.leasePath(id + ".json")
	if err != nil {
		return false, err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
func (l *Locked) Active() ([]Lease, error)     { return l.active(true) }
func (l *Locked) LiveLeases() ([]Lease, error) { return l.active(false) }

func (l *Locked) active(reap bool) ([]Lease, error) {
	dir, err := l.leaseDirectory(false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var active []Lease
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".write-") {
			continue
		}
		p, err := l.leasePath(entry.Name())
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var lease Lease
		if err = config.Decode(b, &lease); err != nil {
			return nil, fmt.Errorf("corrupt lease: %w", err)
		}
		if lease.Version != 2 || !idPattern.MatchString(lease.ID) || entry.Name() != lease.ID+".json" || lease.Process.PID < 1 || lease.Process.Start == "" || lease.Process.Boot == "" {
			return nil, fmt.Errorf("invalid lease identity")
		}
		current, err := ProcessIdentity(lease.Process.PID)
		if os.IsNotExist(err) || (err == nil && current != lease.Process) {
			if reap {
				if _, err = l.Release(lease.ID); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot verify lease process: %w", err)
		}
		active = append(active, lease)
	}
	return active, nil
}
func (l *Locked) RequireIdle() error {
	active, err := l.LiveLeases()
	if err != nil {
		return err
	}
	if len(active) > 0 {
		noun := "commands"
		if len(active) == 1 {
			noun = "command"
		}
		commands := make([]string, len(active))
		for i, lease := range active {
			commands[i] = fmt.Sprintf("%q: host PID %d, since %s", lease.Action, lease.Process.PID, lease.Created.Format(time.RFC3339))
		}
		return commanderror.New("session_busy", fmt.Sprintf("Environment is in use by %d %s (%s).", len(active), noun, strings.Join(commands, "; ")), l.Name, nil,
			commanderror.Next("Inspect active commands", "status", l.Name))
	}
	return nil
}

func (l *Locked) leaseDirectory(create bool) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	rel := filepath.Join("state/leases", l.ID)
	if create {
		return fsutil.Dir(l.store.Home, rel, 0700)
	}
	return fsutil.Path(l.store.Home, rel)
}
func (l *Locked) leasePath(name string) (string, error) {
	root, err := l.leaseDirectory(false)
	if err != nil {
		return "", err
	}
	return fsutil.Path(root, name)
}
