package store

import "fmt"

func (s *Store) PendingID(id string) (*Reservation, error) {
	journals, err := s.Transfers()
	if err != nil {
		return nil, err
	}
	var found *Reservation
	for _, j := range journals {
		if j.SourceID != id && j.DestinationID != id {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("conflicting transfer journals")
		}
		found = &Reservation{ID: j.ID, Source: j.Source.Name, Destination: j.Destination.Name, Mode: j.Mode, Phase: j.Phase, retry: j.RetryStep()}
	}
	return found, nil
}
