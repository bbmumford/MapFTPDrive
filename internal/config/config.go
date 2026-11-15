package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Protocol string

const (
	FTP  Protocol = "ftp"
	FTPS Protocol = "ftps"
	SFTP Protocol = "sftp"
)

type Scope string

const (
	User   Scope = "user"
	System Scope = "system"
)

type TLSMode string

const (
	TLSExplicit TLSMode = "explicit"
	TLSImplicit TLSMode = "implicit"
)

type Endpoint struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Protocol       Protocol `json:"protocol"`
	TLSMode        TLSMode  `json:"tlsMode,omitempty"`
	Host           string   `json:"host"`
	Port           uint16   `json:"port"`
	Username       string   `json:"username"`
	DriveLetter    string   `json:"driveLetter"`
	RemotePath     string   `json:"remotePath,omitempty"`
	Scope          Scope    `json:"scope"`
	CredentialKey  string   `json:"credentialKey"`
	Disabled       bool     `json:"disabled,omitempty"`
	KnownHost      string   `json:"knownHost,omitempty"`
	KnownHostsFile string   `json:"-"`
}

func (e Endpoint) Validate() error {
	e = e.Normalized()
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Name) == "" {
		return errors.New("id and name are required")
	}
	switch e.Protocol {
	case FTP, FTPS, SFTP:
	default:
		return fmt.Errorf("unsupported protocol %q", e.Protocol)
	}
	if e.Protocol == FTPS && e.TLSMode != "" && e.TLSMode != TLSExplicit && e.TLSMode != TLSImplicit {
		return fmt.Errorf("unsupported FTPS mode %q", e.TLSMode)
	}
	if strings.TrimSpace(e.Host) == "" {
		return errors.New("host is required")
	}
	if e.Port == 0 {
		return errors.New("port is required")
	}
	if len(e.DriveLetter) != 1 || e.DriveLetter[0] < 'A' || e.DriveLetter[0] > 'Z' {
		return errors.New("drive letter must be A through Z")
	}
	if e.Scope != User && e.Scope != System {
		return fmt.Errorf("unsupported scope %q", e.Scope)
	}
	if strings.TrimSpace(e.CredentialKey) == "" {
		return errors.New("credential key is required")
	}
	return nil
}

func (e Endpoint) Normalized() Endpoint {
	e.ID = strings.TrimSpace(e.ID)
	e.Name = strings.TrimSpace(e.Name)
	e.Host = strings.TrimSpace(e.Host)
	e.Username = strings.TrimSpace(e.Username)
	e.DriveLetter = strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(e.DriveLetter, ":")))
	e.RemotePath = strings.TrimSpace(strings.ReplaceAll(e.RemotePath, `\`, "/"))
	e.CredentialKey = strings.TrimSpace(e.CredentialKey)
	return e
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load() ([]Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() ([]Endpoint, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var endpoints []Endpoint
	if err := json.Unmarshal(data, &endpoints); err != nil {
		return nil, fmt.Errorf("decode endpoint store: %w", err)
	}
	for index := range endpoints {
		endpoints[index] = endpoints[index].Normalized()
		if err := endpoints[index].Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", endpoints[index].ID, err)
		}
	}
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].DriveLetter < endpoints[j].DriveLetter
	})
	return endpoints, nil
}

func (s *Store) Save(endpoints []Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(endpoints)
}

func (s *Store) save(endpoints []Endpoint) error {
	ids := make(map[string]struct{}, len(endpoints))
	letters := make(map[string]struct{}, len(endpoints))
	for index := range endpoints {
		endpoints[index] = endpoints[index].Normalized()
		endpoint := endpoints[index]
		if _, exists := ids[endpoint.ID]; exists {
			return fmt.Errorf("duplicate endpoint id %q", endpoint.ID)
		}
		if _, exists := letters[endpoint.DriveLetter]; exists {
			return fmt.Errorf("drive %s is assigned more than once", endpoint.DriveLetter)
		}
		ids[endpoint.ID] = struct{}{}
		letters[endpoint.DriveLetter] = struct{}{}
	}
	for _, endpoint := range endpoints {
		if err := endpoint.Validate(); err != nil {
			return fmt.Errorf("%s: %w", endpoint.ID, err)
		}
	}
	data, err := json.MarshalIndent(endpoints, "", "  ")
	if err != nil {
		return err
	}
	if err := ensureStoreDirectory(filepath.Dir(s.path)); err != nil {
		return err
	}
	temp := s.path + ".new"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, s.path); err != nil {
		return err
	}
	s.updateDriveIndex(endpoints)
	return nil
}

func (s *Store) Upsert(endpoint Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoint = endpoint.Normalized()
	if err := endpoint.Validate(); err != nil {
		return err
	}
	endpoints, err := s.load()
	if err != nil {
		return err
	}
	replaced := false
	for index, existing := range endpoints {
		if existing.DriveLetter == endpoint.DriveLetter && existing.ID != endpoint.ID {
			return fmt.Errorf("drive %s is already assigned to %s", endpoint.DriveLetter, existing.Name)
		}
		if existing.ID == endpoint.ID {
			endpoints[index] = endpoint
			replaced = true
		}
	}
	if !replaced {
		endpoints = append(endpoints, endpoint)
	}
	return s.save(endpoints)
}

func (s *Store) FindByDrive(drive string) (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	drive = strings.ToUpper(strings.TrimSpace(strings.TrimRight(drive, `:\`)))
	endpoints, err := s.load()
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
	endpoints, err := s.load()
	if err != nil {
		return Endpoint{}, err
	}
	for index, endpoint := range endpoints {
		if endpoint.ID == id {
			endpoints = append(endpoints[:index], endpoints[index+1:]...)
			return endpoint, s.save(endpoints)
		}
	}
	return Endpoint{}, os.ErrNotExist
}

func (s *Store) Rename(id, name string) (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Endpoint{}, errors.New("name is required")
	}
	endpoints, err := s.load()
	if err != nil {
		return Endpoint{}, err
	}
	for index := range endpoints {
		if endpoints[index].ID == id {
			endpoints[index].Name = name
			return endpoints[index], s.save(endpoints)
		}
	}
	return Endpoint{}, os.ErrNotExist
}

func (s *Store) SetDisabled(id string, disabled bool) (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoints, err := s.load()
	if err != nil {
		return Endpoint{}, err
	}
	for index := range endpoints {
		if endpoints[index].ID == id {
			endpoints[index].Disabled = disabled
			return endpoints[index], s.save(endpoints)
		}
	}
	return Endpoint{}, os.ErrNotExist
}
