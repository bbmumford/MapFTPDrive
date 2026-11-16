package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// DriveIndexName is the file beside the system endpoint store that lists
// system drives for processes that may not read the store: the system
// directory admits only SYSTEM and Administrators, but Explorer verbs, the
// mapper's drive-letter clash check and user hosts run unelevated and must
// still recognise system drives.
const DriveIndexName = "drives.json"

const driveIndexVersion = 1

// DriveIndexEntry is the public, secret-free view of one managed drive. It
// deliberately omits the server, user name, remote path, host key and
// credential reference, because every local user can read the index.
type DriveIndexEntry struct {
	Drive    string `json:"drive"`
	Scope    Scope  `json:"scope"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
}

// Endpoint returns the subset of the endpoint the index records, enough to
// confirm, label and address a drive action.
func (e DriveIndexEntry) Endpoint() Endpoint {
	return Endpoint{ID: e.ID, Name: e.Name, DriveLetter: e.Drive, Scope: e.Scope, Disabled: e.Disabled}
}

type driveIndexFile struct {
	Version int               `json:"version"`
	Drives  []DriveIndexEntry `json:"drives"`
}

// DriveIndex is the location of the drive index beside the store.
func (l Layout) DriveIndex() string { return filepath.Join(l.Dir, DriveIndexName) }

// DriveIndexPath returns the index location for the store at storePath.
func DriveIndexPath(storePath string) string {
	return LayoutFor(storePath).DriveIndex()
}

// DriveIndex builds the index entries for a scope's endpoints, sorted by
// drive letter.
func DriveIndex(scope Scope, endpoints []Endpoint) []DriveIndexEntry {
	entries := make([]DriveIndexEntry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		entries = append(entries, DriveIndexEntry{
			Drive:    NormalizeDrive(endpoint.DriveLetter),
			Scope:    scope,
			ID:       endpoint.ID,
			Name:     endpoint.Name,
			Disabled: endpoint.Disabled,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Drive < entries[j].Drive })
	return entries
}

func encodeDriveIndex(scope Scope, endpoints []Endpoint) ([]byte, error) {
	data, err := json.MarshalIndent(driveIndexFile{Version: driveIndexVersion, Drives: DriveIndex(scope, endpoints)}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// decodeDriveIndex returns the valid entries of an index file. Entries are
// re-validated because readers pass the letter and ID on to privileged
// commands.
func decodeDriveIndex(data []byte) ([]DriveIndexEntry, error) {
	var file driveIndexFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode drive index: %w", err)
	}
	if file.Version > driveIndexVersion {
		return nil, fmt.Errorf("drive index version %d is newer than this MapFTPDrive", file.Version)
	}
	if file.Version < driveIndexVersion {
		return nil, fmt.Errorf("unsupported drive index version %d", file.Version)
	}
	entries := make([]DriveIndexEntry, 0, len(file.Drives))
	for _, entry := range file.Drives {
		entry.Drive = NormalizeDrive(entry.Drive)
		if len(entry.Drive) != 1 || entry.Drive[0] < 'A' || entry.Drive[0] > 'Z' {
			continue
		}
		if ValidateID(entry.ID) != nil || (entry.Scope != User && entry.Scope != System) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// WriteDriveIndex atomically replaces the index at path. On Windows the
// file is created with a protected DACL granting SYSTEM and Administrators
// full control and Users read access before it becomes visible, because the
// directory it lives in admits administrators only (writeDriveIndexFile).
// The system store rewrites it on every save once MaintainDriveIndex is on,
// and the system host refreshes it at start (Store.RefreshDriveIndex).
func WriteDriveIndex(path string, scope Scope, endpoints []Endpoint) error {
	data, err := encodeDriveIndex(scope, endpoints)
	if err != nil {
		return err
	}
	if err := ensureStoreDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	return writeDriveIndexFile(path, data)
}

// ReadDriveIndex returns the valid entries of the index at path. A missing
// index is an empty list: no system drive has been saved yet.
func ReadDriveIndex(path string) ([]DriveIndexEntry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: an index path from DriveIndexPath; entries are re-validated.
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeDriveIndex(data)
}

// ReadSystemDriveIndex reads the system scope's index from its fixed
// location beside the system store. Unelevated processes use it to find
// system drives: findManagedDrive in the command, the mapper's letter
// check and user hosts refusing a letter a system drive holds.
func ReadSystemDriveIndex() ([]DriveIndexEntry, error) {
	path, err := DefaultPath(System)
	if err != nil {
		return nil, err
	}
	return ReadDriveIndex(DriveIndexPath(path))
}

// FindDriveIndexEntry looks up a drive letter in index entries.
func FindDriveIndexEntry(entries []DriveIndexEntry, drive string) (DriveIndexEntry, bool) {
	drive = NormalizeDrive(drive)
	for _, entry := range entries {
		if entry.Drive == drive {
			return entry, true
		}
	}
	return DriveIndexEntry{}, false
}

// MaintainDriveIndex makes every later save rewrite the drive index beside
// the store. NewStore turns it on for the system store; the system host
// turns it on for the store it serves, so a store at another path (tests,
// a relocated data directory) keeps its index too.
func (s *Store) MaintainDriveIndex() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.driveIndex = true
}

// updateDriveIndex rewrites the drive index after save committed
// endpoints. The store is already durable, so a failure is logged and left
// for the next save or RefreshDriveIndex rather than reported as a failed
// save. The caller holds s.mu.
func (s *Store) updateDriveIndex(endpoints []Endpoint) {
	if !s.driveIndex {
		return
	}
	path := DriveIndexPath(s.path)
	if err := WriteDriveIndex(path, System, endpoints); err != nil {
		slog.Error("write drive index", "path", path, "error", err)
	}
}

// RefreshDriveIndex rewrites the drive index from the store's current
// contents when the store maintains one. The system host calls it at start
// so an index that is missing or stale, for example after an upgrade,
// matches the store again.
func (s *Store) RefreshDriveIndex() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.driveIndex {
		return nil
	}
	state, err := s.load()
	if err != nil {
		return err
	}
	return WriteDriveIndex(DriveIndexPath(s.path), System, state.Endpoints)
}
