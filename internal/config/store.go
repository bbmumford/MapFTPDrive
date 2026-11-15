package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// StoreVersion is the envelope version this build reads and writes. A file
// with a higher version was written by a newer MapFTPDrive and is refused
// rather than rewritten, so a downgrade cannot silently drop new fields.
const StoreVersion = 1

// ErrNewerStore reports a store written by a newer release.
var ErrNewerStore = errors.New("endpoint store was written by a newer MapFTPDrive version")

type storeFile struct {
	Version   int               `json:"version"`
	Endpoints []json.RawMessage `json:"endpoints"`
}

// Problem describes one stored entry that was skipped on load. The raw entry
// is kept in the file so that a fix or a newer release can still recover it.
type Problem struct {
	Index int    `json:"index"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error"`
}

// Snapshot is a full load result: the usable endpoints plus everything the
// management window should tell the user about the file itself.
type Snapshot struct {
	Endpoints []Endpoint
	Problems  []Problem
	// Migrated is set when the file was the unversioned bare array; the next
	// save rewrites it as a versioned envelope.
	Migrated bool
	// Restored is set when the main file was unreadable and the backup was
	// used instead.
	Restored bool
}

type Store struct {
	path string
	mu   sync.Mutex
	// onSave runs after every successful write with the saved endpoints; the
	// system host uses it to refresh the user-readable drive index.
	onSave func([]Endpoint)
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

// BackupPath is the single previous version kept beside the store.
func (s *Store) BackupPath() string {
	return s.path + ".bak"
}

// SetSaveHook installs a callback run after each successful save while the
// store lock is held, so hook writes are ordered like the saves themselves.
func (s *Store) SetSaveHook(hook func([]Endpoint)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onSave = hook
}

// loaded is the in-memory form of the file used by every mutation: the valid
// endpoints plus the raw entries that failed validation and must be written
// back unchanged.
type loaded struct {
	Snapshot
	invalid []json.RawMessage
	// mainCorrupt is set when the main file exists but cannot be decoded; the
	// next save then preserves it instead of rotating it over the backup.
	mainCorrupt bool
}

func (s *Store) Load() ([]Endpoint, error) {
	snapshot, err := s.Snapshot()
	return snapshot.Endpoints, err
}

func (s *Store) Snapshot() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.load()
	return state.Snapshot, err
}

func (s *Store) load() (loaded, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return loaded{}, nil
	}
	if err != nil {
		return loaded{}, err
	}
	state, decodeErr := decodeStore(data)
	if decodeErr == nil {
		return state, nil
	}
	if errors.Is(decodeErr, ErrNewerStore) {
		return loaded{}, decodeErr
	}
	backup, backupErr := os.ReadFile(s.BackupPath())
	if backupErr != nil {
		return loaded{mainCorrupt: true}, &CorruptError{Err: decodeErr}
	}
	state, backupDecodeErr := decodeStore(backup)
	if backupDecodeErr != nil {
		return loaded{mainCorrupt: true}, &CorruptError{Err: fmt.Errorf("%w (backup: %v)", decodeErr, backupDecodeErr)}
	}
	state.Restored = true
	state.mainCorrupt = true
	state.Problems = append([]Problem{{Index: -1, Error: "endpoint store was unreadable and was restored from its backup: " + decodeErr.Error()}}, state.Problems...)
	return state, nil
}

func decodeStore(data []byte) (loaded, error) {
	var state loaded
	var raw []json.RawMessage
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) == 0:
		return state, errors.New("file is empty")
	case trimmed[0] == '[':
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return state, err
		}
		state.Migrated = true
	default:
		var file storeFile
		if err := json.Unmarshal(trimmed, &file); err != nil {
			return state, err
		}
		if file.Version > StoreVersion {
			return state, fmt.Errorf("%w (version %d)", ErrNewerStore, file.Version)
		}
		if file.Version < 1 {
			return state, fmt.Errorf("unsupported endpoint store version %d", file.Version)
		}
		raw = file.Endpoints
	}
	ids := make(map[string]struct{}, len(raw))
	letters := make(map[string]struct{}, len(raw))
	names := make(map[string]struct{}, len(raw))
	for index, entry := range raw {
		var endpoint Endpoint
		problem := func(message string) {
			state.Problems = append(state.Problems, Problem{Index: index, ID: endpoint.ID, Error: message})
			state.invalid = append(state.invalid, entry)
		}
		if err := json.Unmarshal(entry, &endpoint); err != nil {
			problem(err.Error())
			continue
		}
		endpoint = endpoint.Normalized()
		if err := endpoint.Validate(); err != nil {
			problem(err.Error())
			continue
		}
		if _, exists := ids[endpoint.ID]; exists {
			problem("duplicate endpoint id")
			continue
		}
		if _, exists := letters[endpoint.DriveLetter]; exists {
			problem(fmt.Sprintf("drive %s: is assigned more than once", endpoint.DriveLetter))
			continue
		}
		if _, exists := names[strings.ToLower(endpoint.Name)]; exists {
			problem(fmt.Sprintf("drive name %q is used more than once", endpoint.Name))
			continue
		}
		ids[endpoint.ID] = struct{}{}
		letters[endpoint.DriveLetter] = struct{}{}
		names[strings.ToLower(endpoint.Name)] = struct{}{}
		state.Endpoints = append(state.Endpoints, endpoint)
	}
	sortEndpoints(state.Endpoints)
	return state, nil
}

func sortEndpoints(endpoints []Endpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].DriveLetter < endpoints[j].DriveLetter
	})
}

// checkSet enforces the per-scope uniqueness rules on a complete endpoint set.
func checkSet(endpoints []Endpoint) error {
	ids := make(map[string]struct{}, len(endpoints))
	letters := make(map[string]string, len(endpoints))
	names := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if err := endpoint.Validate(); err != nil {
			return fmt.Errorf("%s: %w", endpoint.Name, err)
		}
		if _, exists := ids[endpoint.ID]; exists {
			return fmt.Errorf("duplicate endpoint id %q", endpoint.ID)
		}
		if other, exists := letters[endpoint.DriveLetter]; exists {
			return fmt.Errorf("drive %s: is already assigned to %s", endpoint.DriveLetter, other)
		}
		if _, exists := names[strings.ToLower(endpoint.Name)]; exists {
			return fmt.Errorf("another drive is already named %q", endpoint.Name)
		}
		ids[endpoint.ID] = struct{}{}
		letters[endpoint.DriveLetter] = endpoint.Name
		names[strings.ToLower(endpoint.Name)] = struct{}{}
	}
	return nil
}

// CorruptError reports a store file (and backup) that cannot be decoded.
// Writes still proceed on top of it: the unreadable file is preserved as
// ".corrupt" and the new definitions are saved, so one damaged file does not
// block mapping new drives.
type CorruptError struct {
	Err error
}

func (e *CorruptError) Error() string { return "decode endpoint store: " + e.Err.Error() }
func (e *CorruptError) Unwrap() error { return e.Err }

// loadForWrite is load for mutations: a corrupt file yields an empty set that
// will be written over it after the damaged bytes are set aside.
func (s *Store) loadForWrite() (loaded, error) {
	state, err := s.load()
	var corrupt *CorruptError
	if errors.As(err, &corrupt) {
		return loaded{mainCorrupt: true}, nil
	}
	return state, err
}

// Save replaces the whole valid set. Entries that failed validation on load
// are carried over unchanged.
func (s *Store) Save(endpoints []Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadForWrite()
	if err != nil {
		return err
	}
	return s.save(state, endpoints)
}

func (s *Store) save(previous loaded, endpoints []Endpoint) error {
	normalized := make([]Endpoint, len(endpoints))
	for index := range endpoints {
		normalized[index] = endpoints[index].Normalized()
	}
	if err := checkSet(normalized); err != nil {
		return err
	}
	sortEndpoints(normalized)
	file := storeFile{Version: StoreVersion, Endpoints: make([]json.RawMessage, 0, len(normalized)+len(previous.invalid))}
	for _, endpoint := range normalized {
		entry, err := json.Marshal(endpoint)
		if err != nil {
			return err
		}
		file.Endpoints = append(file.Endpoints, entry)
	}
	file.Endpoints = append(file.Endpoints, previous.invalid...)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := s.rotateBackup(previous.mainCorrupt); err != nil {
		return err
	}
	if err := writeFileDurable(s.path, append(data, '\n'), nil); err != nil {
		return err
	}
	if s.onSave != nil {
		s.onSave(append([]Endpoint(nil), normalized...))
	}
	return nil
}

// rotateBackup keeps the current file as the single backup before it is
// replaced. A main file that does not decode is set aside as ".corrupt"
// instead, so the last good backup survives the repair.
func (s *Store) rotateBackup(mainCorrupt bool) error {
	current, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if mainCorrupt {
		return writeFileDurable(s.path+".corrupt", current, nil)
	}
	return writeFileDurable(s.BackupPath(), current, nil)
}

// mutate runs change against the current valid set and saves the result.
func (s *Store) mutate(change func([]Endpoint) ([]Endpoint, error)) error {
	state, err := s.loadForWrite()
	if err != nil {
		return err
	}
	next, err := change(append([]Endpoint(nil), state.Endpoints...))
	if err != nil {
		return err
	}
	return s.save(state, next)
}

// Upsert inserts or replaces the definition with the same ID. A different
// definition already holding the letter or the name is an error; callers
// that intend to replace such an entry remove it in the same transaction
// with Replace.
func (s *Store) Upsert(endpoint Endpoint) error {
	return s.Replace(endpoint, "")
}

// Replace upserts endpoint and, when replacedID is not empty, removes that
// definition in the same write. Map uses it to take over the letter of a
// disabled definition atomically.
func (s *Store) Replace(endpoint Endpoint, replacedID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoint = endpoint.Normalized()
	if err := endpoint.Validate(); err != nil {
		return err
	}
	return s.mutate(func(endpoints []Endpoint) ([]Endpoint, error) {
		next := endpoints[:0]
		replaced := false
		for _, existing := range endpoints {
			switch {
			case replacedID != "" && existing.ID == replacedID && existing.ID != endpoint.ID:
				continue
			case existing.ID == endpoint.ID:
				next = append(next, endpoint)
				replaced = true
			default:
				next = append(next, existing)
			}
		}
		if !replaced {
			next = append(next, endpoint)
		}
		return next, nil
	})
}

// Update applies change to the definition with id and saves it.
func (s *Store) Update(id string, change func(*Endpoint) error) (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var updated Endpoint
	err := s.mutate(func(endpoints []Endpoint) ([]Endpoint, error) {
		for index := range endpoints {
			if endpoints[index].ID != id {
				continue
			}
			candidate := endpoints[index]
			if err := change(&candidate); err != nil {
				return nil, err
			}
			candidate.ID = id
			endpoints[index] = candidate.Normalized()
			updated = endpoints[index]
			return endpoints, nil
		}
		return nil, os.ErrNotExist
	})
	return updated, err
}

func (s *Store) Find(id string) (Endpoint, error) {
	endpoints, err := s.Load()
	if err != nil {
		return Endpoint{}, err
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == id {
			return endpoint, nil
		}
	}
	return Endpoint{}, os.ErrNotExist
}

func (s *Store) FindByDrive(drive string) (Endpoint, error) {
	drive = NormalizeDrive(drive)
	endpoints, err := s.Load()
	if err != nil {
		return Endpoint{}, err
	}
	for _, endpoint := range endpoints {
		if endpoint.DriveLetter == drive {
			return endpoint, nil
		}
	}
	return Endpoint{}, os.ErrNotExist
}

func (s *Store) Remove(id string) (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed Endpoint
	err := s.mutate(func(endpoints []Endpoint) ([]Endpoint, error) {
		for index, endpoint := range endpoints {
			if endpoint.ID == id {
				removed = endpoint
				return append(endpoints[:index], endpoints[index+1:]...), nil
			}
		}
		return nil, os.ErrNotExist
	})
	return removed, err
}

func (s *Store) Rename(id, name string) (Endpoint, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return Endpoint{}, err
	}
	return s.Update(id, func(endpoint *Endpoint) error {
		endpoint.Name = name
		return nil
	})
}

func (s *Store) SetDisabled(id string, disabled bool) (Endpoint, error) {
	return s.Update(id, func(endpoint *Endpoint) error {
		endpoint.Disabled = disabled
		return nil
	})
}
