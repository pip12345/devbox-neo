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

// The journal reserves both bindings and pins the two backing trees. The
// destination record stays private until commitment; Move never swaps roots or
// redirects the source's backing files during preparation.
type Transfer struct {
	Version                int                 `json:"version"`
	ID                     string              `json:"id"`
	Mode                   string              `json:"mode"`
	Phase                  string              `json:"phase"`
	Source                 environment.Binding `json:"source"`
	Destination            environment.Binding `json:"destination"`
	SourceID               string              `json:"source_id"`
	DestinationID          string              `json:"destination_id"`
	SourceTree             string              `json:"source_tree"`
	DestinationTree        string              `json:"destination_tree"`
	SourceContainerID      string              `json:"source_container_id"`
	SourceImageID          string              `json:"source_image_id"`
	SourceImageTag         string              `json:"source_image_tag"`
	ContainerName          string              `json:"container_name"`
	DestinationContainerID string              `json:"destination_container_id,omitempty"`
	Running                bool                `json:"restore_running"`
	ManualStart            bool                `json:"manual_start"`
	Started                time.Time           `json:"started_at"`
}

func (j Transfer) SourceName() string { return environment.PublicName(j.Source, j.SourceID) }
func (j Transfer) DestinationName() string {
	return environment.PublicName(j.Destination, j.DestinationID)
}

type Reservation struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	Phase       string `json:"phase"`
	retry       commanderror.Step
}

func (p Reservation) RetryStep() commanderror.Step { return p.retry }
func (j Transfer) Reservation() *Reservation {
	return &Reservation{ID: j.ID, Source: j.SourceName(), Destination: j.DestinationName(), Mode: j.Mode, Phase: j.Phase, retry: j.RetryStep()}
}

func TransferCommand(mode string) string {
	if mode == "relocate" {
		return "copy --move"
	}
	return "copy"
}

// Retry uses captured bindings and identity, not a newly selected default or a
// filesystem lookup of a workspace which may no longer exist.
func (j Transfer) RetryStep() commanderror.Step {
	args := []string{"copy"}
	if j.Mode == "relocate" {
		args = append(args, "--move")
	}
	args = append(args, j.SourceName(), j.Destination.Workspace, "--as", j.Destination.LocalName)
	return commanderror.Next("Resume transfer", args...)
}

func (j Transfer) Validate() error {
	if j.Version != 5 || !tokenPattern.MatchString(j.ID) || !idPattern.MatchString(j.SourceID) || !idPattern.MatchString(j.DestinationID) || j.Started.IsZero() {
		return fmt.Errorf("invalid transfer identity")
	}
	if j.Mode != "clone" && j.Mode != "relocate" || j.Phase != "prepare" && j.Phase != "committed" {
		return fmt.Errorf("invalid transfer policy or phase")
	}
	if (j.Mode == "relocate") != (j.SourceID == j.DestinationID) || (j.Mode == "clone" && (j.Running || j.ManualStart)) {
		return fmt.Errorf("invalid transfer identity policy")
	}
	if j.Source == j.Destination || j.Source.Validate() != nil || j.Destination.Validate() != nil {
		return fmt.Errorf("invalid transfer bindings")
	}
	expectedTree := InitialStateTree
	if j.Mode == "relocate" {
		expectedTree = "data/" + j.ID
	}
	if !ValidStateTree(j.SourceTree) || j.DestinationTree != expectedTree || (j.Mode == "relocate" && j.SourceTree == j.DestinationTree) {
		return fmt.Errorf("invalid transfer trees")
	}
	if j.ContainerName != environment.ResourceName(j.Destination, j.DestinationID, j.ID) || !hashPattern.MatchString(j.SourceContainerID) ||
		!strings.HasPrefix(j.SourceImageID, "sha256:") || !hashPattern.MatchString(strings.TrimPrefix(j.SourceImageID, "sha256:")) ||
		!strings.HasPrefix(j.SourceImageTag, "dbx/session:"+j.SourceID+"-") || !config.ImageReference.MatchString(j.SourceImageTag) {
		return fmt.Errorf("invalid transfer runtime references")
	}
	if j.Phase == "committed" && (!hashPattern.MatchString(j.DestinationContainerID) || j.DestinationContainerID == j.SourceContainerID) || j.Phase == "prepare" && j.DestinationContainerID != "" {
		return fmt.Errorf("invalid transfer destination commit")
	}
	return nil
}

func (s *Store) transferDirectory(sourceID string) (string, error) {
	if !idPattern.MatchString(sourceID) {
		return "", fmt.Errorf("invalid transfer source ID")
	}
	return fsutil.Path(s.Home, filepath.Join("state/transfers", sourceID))
}
func (s *Store) transferPath(sourceID, name string) (string, error) {
	root, err := s.transferDirectory(sourceID)
	if err != nil {
		return "", err
	}
	return fsutil.Path(root, name)
}

func (s *Store) ReadTransfer(sourceID string) (*Transfer, error) {
	p, err := s.transferPath(sourceID, "journal.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j Transfer
	if err = config.Decode(data, &j); err == nil {
		err = j.Validate()
	}
	if err == nil && j.SourceID != sourceID {
		err = fmt.Errorf("transfer source differs from journal path")
	}
	if err != nil {
		return nil, commanderror.New("invalid_transfer_journal", "Cannot read the transfer journal; reserved identities and trees cannot be trusted. Repair it or restore a verified backup; do not simply delete it.", p, err)
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
	var result []Transfer
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unsupported transfer entry %s; finish pending transfers with the previous build before converting", entry.Name())
		}
		j, err := s.ReadTransfer(entry.Name())
		if err != nil {
			return nil, err
		}
		if j != nil {
			result = append(result, *j)
		}
	}
	return result, nil
}

func (s *Store) Pending(id string) (*Reservation, error) { return s.PendingID(id) }
func (l *Locked) RequireAvailable() error {
	if err := l.check(); err != nil {
		return err
	}
	pending, err := l.store.PendingID(l.ID)
	if err != nil {
		return err
	}
	if pending != nil {
		return commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", pending.Source, nil, pending.retry)
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
	if source.store != destination.store || source.ID != j.SourceID || destination.ID != j.DestinationID {
		return fmt.Errorf("transfer requires both endpoint locks")
	}
	return j.Validate()
}

func (l *Locked) SaveTransfer(destination *Locked, j Transfer) error {
	if err := transferLocks(l, destination, j); err != nil {
		return err
	}
	current, err := l.store.ReadTransfer(j.SourceID)
	if err != nil {
		return err
	}
	if current == nil {
		if j.Phase != "prepare" {
			return fmt.Errorf("transfer must begin in preparation")
		}
		if err := l.RequireAvailable(); err != nil {
			return err
		}
		if destination.ID != l.ID {
			if err := destination.RequireAvailable(); err != nil {
				return err
			}
		}
	} else {
		prior := j
		prior.Phase, prior.DestinationContainerID = current.Phase, current.DestinationContainerID
		if prior != *current || current.Phase == "committed" && j != *current {
			return fmt.Errorf("transfer intent changed")
		}
	}
	root, err := l.store.transferDirectory(j.SourceID)
	if err != nil {
		return err
	}
	if _, err := fsutil.Dir(root, ".", 0700); err != nil {
		return err
	}
	path, err := l.store.transferPath(j.SourceID, "journal.json")
	if err != nil {
		return err
	}
	return fsutil.JSON(path, j)
}

// Prepared records are kept outside canonical session storage. A committed
// journal owns their publication even if the source record has already changed.
func (l *Locked) SaveTransferDestination(j Transfer, r Record) error {
	if err := l.check(); err != nil {
		return err
	}
	if l.ID != j.DestinationID || r.ID != l.ID || r.StateTree != j.DestinationTree || r.Settings.Binding != j.Destination {
		return fmt.Errorf("prepared record differs from transfer destination")
	}
	current, err := l.store.ReadTransfer(j.SourceID)
	if err != nil || current == nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("transfer reservation missing")
	}
	if *current != j || j.Phase != "prepare" {
		return fmt.Errorf("transfer changed before candidate publication")
	}
	if err := r.Validate(r.ID); err != nil {
		return err
	}
	r.Applied.Creation.RawArgs = r.Applied.Inputs.Container.RawArgs
	path, err := l.store.transferPath(j.SourceID, "destination.json")
	if err != nil {
		return err
	}
	return fsutil.JSON(path, r)
}

func (s *Store) ReadTransferDestination(ctx context.Context, j Transfer) (Record, error) {
	var r Record
	if err := ctx.Err(); err != nil {
		return r, err
	}
	path, err := s.transferPath(j.SourceID, "destination.json")
	if err != nil {
		return r, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := config.Decode(data, &r); err != nil {
		return r, err
	}
	r.Directory = r.ID
	if err := r.Validate(r.ID); err != nil {
		return r, err
	}
	if r.ID != j.DestinationID || r.StateTree != j.DestinationTree || r.Settings.Binding != j.Destination || r.Applied.Creation.Name != j.ContainerName ||
		(j.Phase == "committed" && r.Applied.ContainerID != j.DestinationContainerID) {
		return r, fmt.Errorf("prepared record differs from reserved destination")
	}
	return r, nil
}

func (l *Locked) RemoveTransferCandidate(j Transfer) error {
	if err := l.check(); err != nil {
		return err
	}
	if l.ID != j.DestinationID {
		return fmt.Errorf("candidate cleanup differs from lock")
	}
	path, err := l.store.transferPath(j.SourceID, "destination.json")
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (l *Locked) FinishTransfer(destination *Locked, j Transfer) error {
	if err := transferLocks(l, destination, j); err != nil {
		return err
	}
	if j.Phase != "committed" {
		return fmt.Errorf("transfer not committed")
	}
	r, err := destination.ReadRecord(l.ctx)
	if err != nil {
		return err
	}
	if r.ID != j.DestinationID || r.StateTree != j.DestinationTree || r.Applied.ContainerID != j.DestinationContainerID || r.Settings.Binding != j.Destination {
		return fmt.Errorf("destination publication differs from committed transfer")
	}
	return l.removeTransfer(j)
}

func (l *Locked) AbortTransfer(destination *Locked, j Transfer) error {
	if err := transferLocks(l, destination, j); err != nil {
		return err
	}
	if j.Phase != "prepare" {
		return fmt.Errorf("cannot abort committed transfer")
	}
	return l.removeTransfer(j)
}

func (l *Locked) removeTransfer(j Transfer) error {
	current, err := l.store.ReadTransfer(j.SourceID)
	if err != nil {
		return err
	}
	if current == nil || *current != j {
		return fmt.Errorf("transfer changed before release")
	}
	root, err := l.store.transferDirectory(j.SourceID)
	if err != nil {
		return err
	}
	return releaseTransferDirectory(root, os.RemoveAll)
}

func releaseTransferDirectory(root string, removeFiles func(string) error) error {
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	// Completion/abort has settled both endpoints. Persist release before
	// discarding files: a surviving committed journal still needs its
	// prepared record to retry cleanup after interruption.
	if err := os.Remove(filepath.Join(root, "journal.json")); err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		return err
	}
	if err := removeFiles(root); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
