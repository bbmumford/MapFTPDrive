package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DriveIndexName is the file beside the system endpoint store that lists
// system drives for processes that may not read the store: the system
// directory admits only SYSTEM and Administrators, but Explorer verbs and
// the mapper run unelevated and must still recognise system drives.
const DriveIndexName = "drives.json"

const driveIndexVersion = 1

// IndexedDrive is one entry of the drive index. It deliberately omits the
// server, user name, remote path, host key and credential reference,
// because every local user can read the index.
type IndexedDrive struct {
	Drive    string `json:"drive"`
	Scope    Scope  `json:"scope"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
}

type driveIndex struct {
	Version int            `json:"version"`
	Drives  []IndexedDrive `json:"drives"`
}

// Endpoint returns the subset of the endpoint the index records, enough to
// confirm, label and address a drive action.
func (d IndexedDrive) Endpoint() Endpoint {
	return Endpoint{ID: d.ID, Name: d.Name, DriveLetter: d.Drive, Scope: d.Scope, Disabled: d.Disabled}
}

func encodeDriveIndex(endpoints []Endpoint) ([]byte, error) {
	index := driveIndex{Version: driveIndexVersion, Drives: make([]IndexedDrive, 0, len(endpoints))}
	for _, endpoint := range endpoints {
		endpoint = endpoint.Normalized()
		index.Drives = append(index.Drives, IndexedDrive{
			Drive:    endpoint.DriveLetter,
			Scope:    endpoint.Scope,
			ID:       endpoint.ID,
			Name:     endpoint.Name,
			Disabled: endpoint.Disabled,
		})
	}
	return json.MarshalIndent(index, "", "  ")
}

func decodeDriveIndex(data []byte) ([]IndexedDrive, error) {
	var index driveIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decode drive index: %w", err)
	}
	if index.Version != driveIndexVersion {
		return nil, fmt.Errorf("unsupported drive index version %d", index.Version)
	}
	drives := make([]IndexedDrive, 0, len(index.Drives))
	for _, drive := range index.Drives {
		if len(drive.Drive) != 1 || drive.Drive[0] < 'A' || drive.Drive[0] > 'Z' || drive.ID == "" {
			continue
		}
		if drive.Scope != User && drive.Scope != System {
			continue
		}
		drives = append(drives, drive)
	}
	return drives, nil
}

// DriveIndexPath returns the location of the system drive index.
func DriveIndexPath() (string, error) {
	path, err := DefaultPath(System)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), DriveIndexName), nil
}

// ReadDriveIndex returns the system drives recorded by the service. A
// missing index means no system drive has been saved yet.
func ReadDriveIndex() ([]IndexedDrive, error) {
	path, err := DriveIndexPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the fixed index path from DriveIndexPath.
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeDriveIndex(data)
}

// updateDriveIndex rewrites the drive index after the system store saved
// endpoints. The store is already committed, so a failure is left for the
// next save or RefreshDriveIndex rather than reported as a failed save.
func (s *Store) updateDriveIndex(endpoints []Endpoint) {
	if !isSystemStore(s.path) {
		return
	}
	_ = s.writeDriveIndex(endpoints)
}

func (s *Store) writeDriveIndex(endpoints []Endpoint) error {
	data, err := encodeDriveIndex(endpoints)
	if err != nil {
		return err
	}
	return writeDriveIndexFile(filepath.Join(filepath.Dir(s.path), DriveIndexName), data)
}

// RefreshDriveIndex rewrites the drive index from the store's current
// contents. The service calls it at start so an index that is missing or
// stale, for example after an upgrade, matches the store again.
func (s *Store) RefreshDriveIndex() error {
	if !isSystemStore(s.path) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoints, err := s.load()
	if err != nil {
		return err
	}
	return s.writeDriveIndex(endpoints)
}
