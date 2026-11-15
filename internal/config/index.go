package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DriveIndexName is the index file kept beside a scope's endpoint store.
const DriveIndexName = "drives.json"

const driveIndexVersion = 1

// DriveIndexEntry is the public, secret-free view of one managed drive.
// Unelevated processes read the system scope's index to recognise system
// drives (Explorer verbs) and to refuse drive-letter clashes (the mapper),
// without being able to read the protected endpoint store itself.
type DriveIndexEntry struct {
	Drive    string `json:"drive"`
	Scope    Scope  `json:"scope"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
}

type driveIndexFile struct {
	Version int               `json:"version"`
	Drives  []DriveIndexEntry `json:"drives"`
}

// DriveIndex is the user-readable drive letter index written by the system
// host; see WriteDriveIndex.
func (l Layout) DriveIndex() string { return filepath.Join(l.Dir, DriveIndexName) }

// DriveIndexPath returns the index location for the store at storePath.
func DriveIndexPath(storePath string) string {
	return LayoutFor(storePath).DriveIndex()
}

// DriveIndex builds the index entries for a scope's endpoints.
func DriveIndex(scope Scope, endpoints []Endpoint) []DriveIndexEntry {
	entries := make([]DriveIndexEntry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		entries = append(entries, DriveIndexEntry{
			Drive:    endpoint.DriveLetter,
			Scope:    scope,
			ID:       endpoint.ID,
			Name:     endpoint.Name,
			Disabled: endpoint.Disabled,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Drive < entries[j].Drive })
	return entries
}

// WriteDriveIndex atomically replaces the index at path. On Windows the file
// gets a protected DACL granting SYSTEM and Administrators full control and
// Users read access, applied before the file becomes visible, because the
// directory it lives in is readable by administrators only. The system host
// calls it after every save of the system store and once at start-up.
func WriteDriveIndex(path string, scope Scope, endpoints []Endpoint) error {
	data, err := json.MarshalIndent(driveIndexFile{Version: driveIndexVersion, Drives: DriveIndex(scope, endpoints)}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileDurable(path, append(data, '\n'), applyDriveIndexSecurity)
}

// ReadDriveIndex returns the valid entries of the index at path. A missing
// index is an empty list. Entries are re-validated because readers pass the
// letter and ID on to privileged commands.
func ReadDriveIndex(path string) ([]DriveIndexEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file driveIndexFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode drive index: %w", err)
	}
	if file.Version > driveIndexVersion {
		return nil, fmt.Errorf("drive index version %d is newer than this MapFTPDrive", file.Version)
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
