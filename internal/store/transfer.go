package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
)

// Transfer is the single authority for an unfinished operation and reserves
// both endpoints. External placement lets source deletion finish without
// unlinking recovery. Publication/removal reserves/releases both names at once.
type Transfer struct {
	Version       int                      `json:"version"`
	ID            string                   `json:"id"`
	Mode          string                   `json:"mode"`
	Phase         string                   `json:"phase"`
	Source        environment.Identity     `json:"source"`
	Destination   environment.Identity     `json:"destination"`
	RequestedTo   string                   `json:"requested_to,omitempty"`
	SourceID      string                   `json:"source_id"`
	DestinationID string                   `json:"destination_id"`
	Running       bool                     `json:"restore_running"`
	ManualStart   bool                     `json:"manual_start"`
	Started       time.Time                `json:"started_at"`
	Desired       environment.Fingerprints `json:"desired"`
}

// Reservation is a read-only view derived from the journal, not another file.
type Reservation struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	Phase       string `json:"phase"`
	retry       commanderror.Step
}

func (p Reservation) RetryStep() commanderror.Step { return p.retry }

// TransferCommand renders the CLI operation for a journal mode. The durable
// modes also select harness capabilities; they are not CLI command names.
func TransferCommand(mode string) string {
	if mode == "relocate" {
		return "copy --move"
	}
	return "copy"
}

// RetryStep uses the journal's exact endpoints, including same-folder slots.
// It does not rediscover defaults or depend on the source record still existing.
func (j Transfer) RetryStep() commanderror.Step {
	args := []string{"copy"}
	if j.Mode == "relocate" {
		args = append(args, "--move")
	}
	if j.Source.Workspace == j.Destination.Workspace {
		args = append(args, j.Source.Workspace, "--from", j.Source.Selector(), "--to", "."+environment.Slot(j.Destination.Profile, j.Destination.Project))
	} else {
		args = append(args, j.Source.Name, j.Destination.Workspace)
		if j.Destination.Profile != j.Source.Profile || j.Destination.Project != j.Source.Project {
			args = append(args, "--to", "."+environment.Slot(j.Destination.Profile, j.Destination.Project))
		}
	}
	return commanderror.Next("Resume transfer", args...)
}

func (j Transfer) Validate() error {
	if j.Version != 1 || !idPattern.MatchString(j.ID) || !idPattern.MatchString(j.SourceID) || !idPattern.MatchString(j.DestinationID) || j.Started.IsZero() {
		return fmt.Errorf("invalid transfer identity")
	}
	if j.Mode != "clone" && j.Mode != "relocate" {
		return fmt.Errorf("invalid transfer mode")
	}
	if j.Phase != "prepare" && j.Phase != "committed" {
		return fmt.Errorf("invalid transfer phase")
	}
	if (j.Mode == "relocate") != (j.SourceID == j.DestinationID) || (j.Mode == "clone" && (j.Running || j.ManualStart)) {
		return fmt.Errorf("invalid transfer policy")
	}
	for _, id := range []environment.Identity{j.Source, j.Destination} {
		if !validName(id.Name) || !filepath.IsAbs(id.Workspace) || filepath.Clean(id.Workspace) != id.Workspace || id.Name != environment.ContainerName(id.Workspace, id.Slot) {
			return fmt.Errorf("invalid transfer endpoint")
		}
		if err := id.ValidateSlot(); err != nil {
			return err
		}
	}
	if j.RequestedTo != "" {
		slot, prefixed := strings.CutPrefix(j.RequestedTo, ".")
		profile, project, err := environment.ParseSlot(slot)
		// Inheritance can remove a requested profile, but cannot add one or
		// change whether the destination uses project configuration.
		if !prefixed || err != nil || project != j.Destination.Project || (profile != j.Destination.Profile && (!project || j.Destination.Profile != "")) {
			return fmt.Errorf("invalid requested transfer destination")
		}
	}
	if j.Source.Workspace != j.Destination.Workspace && j.Mode == "relocate" && (j.Source.Profile != j.Destination.Profile || j.Source.Project != j.Destination.Project) {
		return fmt.Errorf("cross-folder relocation must retain the source combination")
	}
	if j.Source.Name == j.Destination.Name {
		return fmt.Errorf("transfer endpoints must differ")
	}
	if !hashPattern.MatchString(j.Desired.Image) || !hashPattern.MatchString(j.Desired.Container) || !hashPattern.MatchString(j.Desired.Runtime) {
		return fmt.Errorf("invalid transfer inputs")
	}
	return nil
}
func (s *Store) transferPath(name string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("invalid session name")
	}
	return fsutil.Path(s.Home, filepath.Join("state/transfers", name+".json"))
}
func (s *Store) ReadTransfer(name string) (*Transfer, error) {
	p, err := s.transferPath(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j Transfer
	if err = config.Decode(b, &j); err != nil {
		return nil, fmt.Errorf("corrupt transfer journal: %w", err)
	}
	if err = j.Validate(); err != nil {
		return nil, err
	}
	if j.Source.Name != name {
		return nil, fmt.Errorf("transfer journal source mismatch")
	}
	return &j, nil
}
func (s *Store) Transfers() ([]Transfer, error) {
	root, err := fsutil.Path(s.Home, "state/transfers")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Transfer{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		j, err := s.ReadTransfer(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if j != nil {
			result = append(result, *j)
		}
	}
	return result, nil
}
func (s *Store) Pending(name string) (*Reservation, error) {
	if !validName(name) {
		return nil, fmt.Errorf("invalid session name")
	}
	journals, err := s.Transfers()
	if err != nil {
		return nil, err
	}
	var pending *Reservation
	for _, j := range journals {
		if j.Source.Name == name || j.Destination.Name == name {
			if pending != nil {
				return nil, fmt.Errorf("conflicting transfer journals")
			}
			pending = &Reservation{ID: j.ID, Source: j.Source.Name, Destination: j.Destination.Name, Mode: j.Mode, Phase: j.Phase, retry: j.RetryStep()}
		}
	}
	return pending, nil
}
func (l *Locked) RequireAvailable() error {
	if err := l.check(); err != nil {
		return err
	}
	pending, err := l.store.Pending(l.Name)
	if err != nil {
		return err
	}
	if pending != nil {
		return commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", l.Name, nil, pending.retry)
	}
	return nil
}
func transferLocks(source, destination *Locked, j Transfer) error {
	if err := source.check(); err != nil {
		return err
	}
	if err := destination.check(); err != nil {
		return err
	}
	if source.store != destination.store || source.Name != j.Source.Name || destination.Name != j.Destination.Name {
		return fmt.Errorf("transfer requires both endpoint locks")
	}
	return j.Validate()
}
func (l *Locked) SaveTransfer(destination *Locked, j Transfer) error {
	if err := transferLocks(l, destination, j); err != nil {
		return err
	}
	current, err := l.store.ReadTransfer(l.Name)
	if err != nil {
		return err
	}
	if current == nil {
		if j.Phase != "prepare" {
			return fmt.Errorf("transfer must begin in preparation")
		}
		if err = l.RequireAvailable(); err != nil {
			return err
		}
		if err = destination.RequireAvailable(); err != nil {
			return err
		}
	} else {
		priorPhase := current.Phase
		current.Phase = j.Phase
		if *current != j || (priorPhase == "committed" && j.Phase != "committed") {
			return fmt.Errorf("transfer journal cannot change its contract or move backwards")
		}
	}
	if _, err := fsutil.Dir(l.store.Home, "state/transfers", 0700); err != nil {
		return err
	}
	// Persist a newly created journal directory before any endpoint mutation.
	parent, err := os.Open(filepath.Join(l.store.Home, "state"))
	if err != nil {
		return err
	}
	err = parent.Sync()
	parent.Close()
	if err != nil {
		return err
	}
	p, err := l.store.transferPath(l.Name)
	if err != nil {
		return err
	}
	return fsutil.JSON(p, j)
}
func removeSynced(p string) error {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir, err := os.Open(filepath.Dir(p))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (l *Locked) FinishTransfer(destination *Locked, j Transfer) error {
	if err := transferLocks(l, destination, j); err != nil {
		return err
	}
	if j.Phase != "committed" {
		return fmt.Errorf("cannot finish an uncommitted transfer")
	}
	current, err := l.store.ReadTransfer(l.Name)
	if err != nil {
		return err
	}
	if current == nil || *current != j {
		return fmt.Errorf("transfer journal changed before cleanup")
	}
	p, err := l.store.transferPath(l.Name)
	if err != nil {
		return err
	}
	return removeSynced(p)
}

// ReadRecord is reserved for read-only inspection and the transfer engine under
// endpoint locks. Ordinary lifecycle operations use Load, which rejects pending work.
func (l *Locked) ReadRecord(ctx context.Context) (Record, error) {
	if err := l.check(); err != nil {
		return Record{}, err
	}
	return l.store.Read(ctx, l.Name)
}
