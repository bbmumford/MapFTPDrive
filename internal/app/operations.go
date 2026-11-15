package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/mount"
)

// maxKeyFileSize bounds the private key file copied into the scope's
// directory; real OpenSSH and PuTTY keys are a few kilobytes.
const maxKeyFileSize = 64 << 10

// Map creates a drive from a new definition. It verifies the server
// identity, mounts with the supplied secret, and only after the drive is
// live stores the credential and the definition; a failure at any step
// undoes the earlier ones, so a failed first mapping leaves nothing behind
// holding the letter. A disabled definition on the same letter is re-used
// when it points at the same account and replaced otherwise.
func (s *Supervisor) Map(ctx context.Context, endpoint config.Endpoint, secret string) (config.Endpoint, error) {
	s.mapMu.Lock()
	defer s.mapMu.Unlock()
	endpoint = endpoint.Normalized()
	endpoint.Disabled = false
	existing, err := s.store.Load()
	if err != nil {
		var corrupt *config.CorruptError
		if !errors.As(err, &corrupt) {
			return config.Endpoint{}, err
		}
	}
	replacedID := ""
	if endpoint.ID == "" {
		for _, candidate := range existing {
			if candidate.DriveLetter == endpoint.DriveLetter && candidate.Disabled && candidate.SameServer(endpoint) {
				endpoint.ID = candidate.ID
			}
		}
	}
	if endpoint.ID == "" {
		id, err := NewID()
		if err != nil {
			return config.Endpoint{}, err
		}
		endpoint.ID = id
	}
	endpoint.CredentialKey = config.CredentialKeyFor(endpoint.ID)
	for _, candidate := range existing {
		if candidate.ID != endpoint.ID && candidate.DriveLetter == endpoint.DriveLetter && candidate.Disabled {
			replacedID = candidate.ID
		}
	}
	var previous *config.Endpoint
	for index := range existing {
		if existing[index].ID == endpoint.ID {
			previous = &existing[index]
		}
	}
	if previous != nil && !previous.Disabled {
		return config.Endpoint{}, fmt.Errorf("drive %s: is already mapped to %s", previous.DriveLetter, previous.Name)
	}
	if err := s.checkAssignment(endpoint, existing, replacedID, true); err != nil {
		return config.Endpoint{}, err
	}
	return s.commit(ctx, endpoint, &secret, previous, replacedID)
}

// Update replaces an existing definition (Edit). The old mount is stopped,
// the new definition is mounted, and the change is saved only if that
// works; otherwise the old definition is mounted again. A nil secret keeps
// the stored one.
func (s *Supervisor) Update(ctx context.Context, endpoint config.Endpoint, secret *string) (config.Endpoint, error) {
	s.mapMu.Lock()
	defer s.mapMu.Unlock()
	endpoint = endpoint.Normalized()
	previous, err := s.store.Find(endpoint.ID)
	if err != nil {
		return config.Endpoint{}, fmt.Errorf("drive definition %s: %w", endpoint.ID, err)
	}
	endpoint.CredentialKey = previous.CredentialKey
	endpoint.Scope = previous.Scope
	endpoint.Disabled = false
	if secret == nil {
		_, stored, err := s.credentials.Get(previous.CredentialKey)
		if err != nil {
			return config.Endpoint{}, fmt.Errorf("load the saved credential: %w", err)
		}
		secret = &stored
	}
	existing, err := s.store.Load()
	if err != nil {
		return config.Endpoint{}, err
	}
	if err := s.checkAssignment(endpoint, existing, "", endpoint.DriveLetter != previous.DriveLetter); err != nil {
		return config.Endpoint{}, err
	}
	return s.commit(ctx, endpoint, secret, &previous, "")
}

// checkAssignment refuses a drive letter or name another definition holds,
// a letter Windows already shows in use, and a letter the other scope
// manages. replacedID is a disabled definition the caller will remove.
func (s *Supervisor) checkAssignment(endpoint config.Endpoint, existing []config.Endpoint, replacedID string, checkLetterInUse bool) error {
	if err := endpoint.Validate(); err != nil {
		return err
	}
	for _, candidate := range existing {
		if candidate.ID == endpoint.ID || candidate.ID == replacedID {
			continue
		}
		if candidate.DriveLetter == endpoint.DriveLetter {
			return fmt.Errorf("drive %s: is already mapped to %s", endpoint.DriveLetter, candidate.Name)
		}
		if strings.EqualFold(candidate.Name, endpoint.Name) {
			return fmt.Errorf("another drive is already named %q", endpoint.Name)
		}
	}
	if checkLetterInUse && s.settings().UsedLetters != nil {
		mask, err := s.settings().UsedLetters()
		if err == nil && mask&(1<<(endpoint.DriveLetter[0]-'A')) != 0 {
			return fmt.Errorf("drive %s: is already in use by another drive or device; choose a free letter", endpoint.DriveLetter)
		}
	}
	if s.settings().OtherScopeDrives != nil {
		others, err := s.settings().OtherScopeDrives()
		if err == nil {
			if other, found := config.FindDriveIndexEntry(others, endpoint.DriveLetter); found && other.ID != endpoint.ID {
				return fmt.Errorf("drive %s: is assigned to the %s drive %q", endpoint.DriveLetter, other.Scope, other.Name)
			}
		}
	}
	return nil
}

// commit performs the shared test-mount-save sequence of Map and Update.
func (s *Supervisor) commit(ctx context.Context, endpoint config.Endpoint, secret *string, previous *config.Endpoint, replacedID string) (config.Endpoint, error) {
	var entryOld *entry
	if previous != nil {
		entryOld = s.lookup(previous.ID)
	}
	if entryOld != nil {
		entryOld.op.Lock()
		defer entryOld.op.Unlock()
		s.stopLoop(entryOld)
	}
	restoreOld := func() {
		if previous != nil && entryOld != nil && !previous.Disabled {
			s.setEndpoint(entryOld, *previous)
			s.startLoop(entryOld, nil)
		}
	}

	keyCopied := false
	if endpoint.AuthMethodOrDefault() == config.AuthKey && endpoint.KeyFile != s.layout.KeyFile(endpoint.ID) {
		staged, err := s.stageKeyFile(endpoint)
		if err != nil {
			restoreOld()
			return config.Endpoint{}, err
		}
		endpoint.KeyFile = staged
		keyCopied = true
	}
	discardStaged := func() {
		if keyCopied {
			_ = discardPath(s.layout.KeyFile(endpoint.ID) + ".new")
		}
	}

	attemptCtx, cancel := context.WithTimeout(ctx, s.settings().AttemptTimeout)
	defer cancel()
	spec, err := s.prepare(attemptCtx, endpoint, secret)
	if err == nil {
		handle, mountErr := s.mountWithin(attemptCtx, spec)
		if mountErr == nil {
			return s.save(endpoint, *secret, previous, replacedID, handle, keyCopied, entryOld, restoreOld)
		}
		err = mountErr
	}
	discardStaged()
	restoreOld()
	if previous == nil {
		_ = discardPath(s.layout.KnownHostsFile(endpoint.ID))
	}
	s.log.Warn("mapping failed", "drive", endpoint.DriveLetter, "name", endpoint.Name, "error", err)
	return config.Endpoint{}, err
}

// save stores the credential and definition for a mounted endpoint and
// adopts the mount, unwinding everything if either write fails.
func (s *Supervisor) save(endpoint config.Endpoint, secret string, previous *config.Endpoint, replacedID string, handle mount.Handle, keyCopied bool, locked *entry, restoreOld func()) (config.Endpoint, error) {
	staged := s.layout.KeyFile(endpoint.ID) + ".new"
	if keyCopied {
		// The mount used the staged copy; the final copy takes its place
		// before the definition that names it is saved.
		if err := os.Rename(staged, s.layout.KeyFile(endpoint.ID)); err != nil {
			_ = handle.Unmount()
			_ = discardPath(staged)
			restoreOld()
			return config.Endpoint{}, err
		}
		endpoint.KeyFile = s.layout.KeyFile(endpoint.ID)
	}
	previousUser, previousSecret, previousErr := s.credentials.Get(endpoint.CredentialKey)
	undo := func(cause error) (config.Endpoint, error) {
		_ = handle.Unmount()
		if previousErr == nil {
			_ = s.credentials.Put(endpoint.CredentialKey, previousUser, previousSecret)
		} else {
			_ = s.credentials.Delete(endpoint.CredentialKey)
		}
		restoreOld()
		s.log.Warn("mapping rolled back", "drive", endpoint.DriveLetter, "error", cause)
		return config.Endpoint{}, cause
	}
	if err := s.credentials.Put(endpoint.CredentialKey, endpoint.Username, secret); err != nil {
		return undo(fmt.Errorf("store credential: %w", err))
	}
	if err := s.store.Replace(endpoint, replacedID); err != nil {
		return undo(err)
	}
	if replacedID != "" {
		if err := s.cleanupRemoved(replacedID, config.CredentialKeyFor(replacedID)); err != nil {
			s.log.Warn("clean up replaced drive", "id", replacedID, "error", err)
		}
	}
	if previous != nil && previous.AuthMethodOrDefault() == config.AuthKey && endpoint.AuthMethodOrDefault() != config.AuthKey {
		_ = discardPath(s.layout.KeyFile(endpoint.ID))
	}
	e := s.ensureEntry(endpoint)
	if e != locked {
		e.op.Lock()
		defer e.op.Unlock()
	}
	s.startLoop(e, handle)
	s.log.Info("mapped", "drive", endpoint.DriveLetter, "name", endpoint.Name, "protocol", endpoint.Protocol)
	return endpoint, nil
}

// stageKeyFile copies the user's private key next to the store as
// keys\<key>.new; the copy replaces keys\<key> once the definition is
// saved, so the drive keeps working if the original file moves.
func (s *Supervisor) stageKeyFile(endpoint config.Endpoint) (string, error) {
	source, err := os.Open(endpoint.KeyFile)
	if err != nil {
		return "", fmt.Errorf("private key file: %w", err)
	}
	defer source.Close()
	data, err := io.ReadAll(io.LimitReader(source, maxKeyFileSize+1))
	if err != nil {
		return "", fmt.Errorf("private key file: %w", err)
	}
	if len(data) > maxKeyFileSize {
		return "", errors.New("private key file is too large to be a key")
	}
	if err := os.MkdirAll(s.layout.KeysDir(), 0o700); err != nil {
		return "", err
	}
	staged := s.layout.KeyFile(endpoint.ID) + ".new"
	if err := os.WriteFile(staged, data, 0o600); err != nil {
		return "", err
	}
	return staged, nil
}

// Test checks a definition without mounting or saving: server identity,
// login and a listing of the remote root. A nil secret uses the stored one.
func (s *Supervisor) Test(ctx context.Context, endpoint config.Endpoint, secret *string) error {
	endpoint = endpoint.Normalized()
	if endpoint.ID == "" {
		id, err := NewID()
		if err != nil {
			return err
		}
		endpoint.ID = id
	}
	endpoint.CredentialKey = config.CredentialKeyFor(endpoint.ID)
	if err := endpoint.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.settings().AttemptTimeout)
	defer cancel()
	spec, err := s.prepare(ctx, endpoint, secret)
	if err != nil {
		return err
	}
	return s.engine.Probe(ctx, spec)
}

// TestStored tests the saved definition with its saved secret.
func (s *Supervisor) TestStored(ctx context.Context, id string) error {
	endpoint, err := s.store.Find(id)
	if err != nil {
		return err
	}
	return s.Test(ctx, endpoint, nil)
}

// Enable marks a definition enabled and mounts it, waiting up to
// SettleTimeout for the first attempt. It returns a message when the mount
// is still connecting, and the attempt's error when it failed (the drive
// keeps retrying in the background unless it needs attention).
func (s *Supervisor) Enable(ctx context.Context, id string) (string, error) {
	endpoint, err := s.store.SetDisabled(id, false)
	if err != nil {
		return "", err
	}
	e := s.ensureEntry(endpoint)
	e.op.Lock()
	version := s.version(e)
	if s.loopRunning(e) {
		wake(e.wake)
	} else {
		s.startLoop(e, nil)
	}
	e.op.Unlock()
	return s.settle(ctx, e, version)
}

func (s *Supervisor) settle(ctx context.Context, e *entry, version uint64) (string, error) {
	s.mu.Lock()
	alreadyMounted := e.status.State == Mounted && e.version == version
	s.mu.Unlock()
	if alreadyMounted {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.settings().SettleTimeout)
	defer cancel()
	status, settled := s.waitSettled(ctx, e, version)
	switch {
	case !settled:
		return "still connecting; the drive appears when the server answers", nil
	case status.State == Mounted:
		return "", nil
	case status.State == Retrying:
		return "", fmt.Errorf("%s (MapFTPDrive keeps retrying)", status.LastError)
	default:
		return "", errors.New(status.LastError)
	}
}

// Disable unmounts and keeps the definition, marked disabled.
func (s *Supervisor) Disable(id string) error {
	if e := s.lookup(id); e != nil {
		e.op.Lock()
		defer e.op.Unlock()
		s.stopLoop(e)
	}
	endpoint, err := s.store.SetDisabled(id, true)
	if err != nil {
		return err
	}
	s.ensureEntry(endpoint)
	return nil
}

// Remove unmounts and deletes the definition, its credential, pinned host
// key file, private key copy and VFS cache.
func (s *Supervisor) Remove(id string) error {
	if e := s.lookup(id); e != nil {
		e.op.Lock()
		defer e.op.Unlock()
		s.stopLoop(e)
	}
	removed, err := s.store.Remove(id)
	if err != nil {
		return err
	}
	return s.cleanupRemoved(removed.ID, removed.CredentialKey)
}

func (s *Supervisor) cleanupRemoved(id, credentialKey string) error {
	if e := s.lookup(id); e != nil {
		s.stopLoop(e)
	}
	s.forget(id)
	var errs []error
	if err := s.credentials.Delete(credentialKey); err != nil {
		errs = append(errs, fmt.Errorf("delete credential: %w", err))
	}
	for _, path := range []string{s.layout.KnownHostsFile(id), s.layout.KeyFile(id), s.layout.KeyFile(id) + ".new"} {
		if err := discardPath(path); err != nil {
			errs = append(errs, err)
		}
	}
	for _, dir := range s.layout.CacheDirs(id) {
		if err := discardTree(dir); err != nil {
			errs = append(errs, err)
		}
	}
	s.log.Info("removed", "id", id)
	return errors.Join(errs...)
}

// Rename changes the drive name. WinFsp FUSE volumes reject label changes
// through Explorer, so a mounted drive is remounted with the new label;
// network-mode volumes keep their \mapftpdrive\<id> prefix, which does not
// depend on the name.
func (s *Supervisor) Rename(ctx context.Context, id, name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := config.ValidateName(name); err != nil {
		return "", err
	}
	updated, err := s.store.Rename(id, name)
	if err != nil {
		return "", err
	}
	e := s.ensureEntry(updated)
	e.op.Lock()
	running := s.loopRunning(e)
	version := s.version(e)
	if running {
		s.stopLoop(e)
		version = s.version(e)
		s.startLoop(e, nil)
	}
	e.op.Unlock()
	s.log.Info("renamed", "drive", updated.DriveLetter, "name", name)
	if !running {
		return "", nil
	}
	message, err := s.settle(ctx, e, version)
	if err != nil {
		return "", fmt.Errorf("the drive was renamed, but remounting it failed: %w", err)
	}
	return message, nil
}

// Repin stores a newly confirmed SFTP host key or FTPS certificate digest
// and mounts the drive with it.
func (s *Supervisor) Repin(ctx context.Context, id, knownHost, certificatePin string) (string, error) {
	if knownHost == "" && certificatePin == "" {
		return "", errors.New("a host key or certificate digest is required")
	}
	updated, err := s.store.Update(id, func(endpoint *config.Endpoint) error {
		switch endpoint.Protocol {
		case config.SFTP:
			if knownHost == "" {
				return errors.New("an SFTP drive is pinned by its host key")
			}
			endpoint.KnownHost = knownHost
		case config.FTPS:
			if certificatePin == "" {
				return errors.New("an FTPS drive is pinned by its certificate digest")
			}
			endpoint.CertificatePin = certificatePin
		default:
			return errors.New("plain FTP has no server identity to pin")
		}
		endpoint.Disabled = false
		return nil
	})
	if err != nil {
		return "", err
	}
	e := s.ensureEntry(updated)
	e.op.Lock()
	version := s.version(e)
	if s.loopRunning(e) {
		s.stopLoop(e)
		version = s.version(e)
	}
	s.startLoop(e, nil)
	e.op.Unlock()
	s.log.Info("re-pinned server identity", "drive", updated.DriveLetter, "name", updated.Name)
	return s.settle(ctx, e, version)
}

// discardPath deletes a file this package created; a missing file is fine.
func discardPath(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// discardTree deletes a cache directory this package's engine created.
func discardTree(path string) error {
	return os.RemoveAll(path)
}
