package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/mount"
	"mapftpdrive/internal/security"
)

// unmountWait bounds how long stopping a loop waits for WinFsp to release
// the drive before giving up on the handle.
const unmountWait = 45 * time.Second

// startLoop begins supervising e. handle, when not nil, is a mount the
// caller already made (Map and Update test by mounting) and is adopted
// instead of mounting again. Callers hold e.op.
func (s *Supervisor) startLoop(e *entry, handle mount.Handle) {
	s.mu.Lock()
	if e.cancel != nil {
		s.mu.Unlock()
		if handle != nil {
			_ = handle.Unmount()
		}
		return
	}
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	e.cancel, e.done = cancel, done
	s.mu.Unlock()
	go func() {
		defer close(done)
		s.run(ctx, e, handle)
	}()
}

// stopLoop cancels e's loop and waits for it to unmount and exit. Callers
// hold e.op (or are shutting down).
func (s *Supervisor) stopLoop(e *entry) {
	s.mu.Lock()
	cancel, done := e.cancel, e.done
	e.cancel, e.done = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(unmountWait + 5*time.Second):
		s.log.Error("supervision loop did not stop", "drive", s.currentEndpoint(e).DriveLetter)
	}
}

func (s *Supervisor) loopRunning(e *entry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return e.cancel != nil
}

// run keeps one endpoint mounted until ctx ends: it mounts, waits for the
// mount to end, and remounts after a drop; failures back off, and a network
// change or explicit wake retries early. Identity and credential problems
// park the endpoint in Attention until a wake (re-pin, edit or mount).
func (s *Supervisor) run(ctx context.Context, e *entry, handle mount.Handle) {
	failures := 0
	for {
		if handle == nil {
			s.setStatus(e, func(status *Status) {
				status.State = Connecting
				status.RetryAt = time.Time{}
			})
			mounted, err := s.attempt(ctx, s.currentEndpoint(e))
			if ctx.Err() != nil {
				if mounted != nil {
					_ = mounted.Unmount()
				}
				s.setStatus(e, func(status *Status) { *status = Status{State: Disabled} })
				return
			}
			if err != nil {
				failures++
				endpoint := s.currentEndpoint(e)
				if needsAttention(err) {
					s.log.Warn("mount needs attention", "drive", endpoint.DriveLetter, "name", endpoint.Name, "error", err)
					s.setStatus(e, func(status *Status) {
						status.State = Attention
						status.LastError = err.Error()
						status.Attempts = failures
						status.Trust = trustProblem(err)
					})
					select {
					case <-ctx.Done():
						s.setStatus(e, func(status *Status) { *status = Status{State: Disabled} })
						return
					case <-e.wake:
					}
					continue
				}
				delay := s.settings().Backoff.Delay(failures)
				s.log.Warn("mount failed; retrying", "drive", endpoint.DriveLetter, "name", endpoint.Name, "in", delay.Round(time.Second), "error", err)
				s.setStatus(e, func(status *Status) {
					status.State = Retrying
					status.LastError = err.Error()
					status.RetryAt = s.options.Now().Add(delay)
					status.Attempts = failures
					status.Trust = nil
				})
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					s.setStatus(e, func(status *Status) { *status = Status{State: Disabled} })
					return
				case <-e.wake:
					timer.Stop()
				case <-timer.C:
				}
				continue
			}
			handle = mounted
		}
		failures = 0
		endpoint := s.currentEndpoint(e)
		s.log.Info("mounted", "drive", endpoint.DriveLetter, "name", endpoint.Name)
		s.setStatus(e, func(status *Status) {
			*status = Status{State: Mounted}
		})
		ended := make(chan error, 1)
		go func(current mount.Handle) { ended <- current.Wait() }(handle)
		select {
		case <-ctx.Done():
			if err := handle.Unmount(); err != nil {
				s.log.Error("unmount", "drive", endpoint.DriveLetter, "error", err)
			}
			select {
			case waitErr := <-ended:
				if waitErr != nil {
					s.log.Warn("mount ended with an error after unmount", "drive", endpoint.DriveLetter, "error", waitErr)
				}
			case <-time.After(unmountWait):
				s.log.Error("mount did not end after unmount", "drive", endpoint.DriveLetter)
			}
			s.setStatus(e, func(status *Status) { *status = Status{State: Disabled} })
			s.log.Info("unmounted", "drive", endpoint.DriveLetter, "name", endpoint.Name)
			return
		case err := <-ended:
			handle = nil
			message := "the mount ended unexpectedly"
			if err != nil {
				message += ": " + err.Error()
			}
			failures = 1
			delay := s.settings().Backoff.Delay(failures)
			s.log.Warn("mount dropped; remounting", "drive", endpoint.DriveLetter, "name", endpoint.Name, "in", delay.Round(time.Second), "error", message)
			s.setStatus(e, func(status *Status) {
				status.State = Retrying
				status.LastError = message
				status.RetryAt = s.options.Now().Add(delay)
				status.Attempts = failures
			})
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				s.setStatus(e, func(status *Status) { *status = Status{State: Disabled} })
				return
			case <-e.wake:
				timer.Stop()
			case <-timer.C:
			}
		}
	}
}

// attempt makes one bounded connect-and-mount attempt. A mount that
// completes after the attempt timed out is unmounted, so a slow server
// never leaves an unsupervised drive behind.
func (s *Supervisor) attempt(ctx context.Context, endpoint config.Endpoint) (mount.Handle, error) {
	ctx, cancel := context.WithTimeout(ctx, s.settings().AttemptTimeout)
	defer cancel()
	spec, err := s.prepare(ctx, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return s.mountWithin(ctx, spec)
}

func (s *Supervisor) mountWithin(ctx context.Context, spec mount.Spec) (mount.Handle, error) {
	type result struct {
		handle mount.Handle
		err    error
	}
	results := make(chan result, 1)
	go func() {
		handle, err := s.engine.Mount(ctx, spec)
		results <- result{handle, err}
	}()
	select {
	case outcome := <-results:
		return outcome.handle, outcome.err
	case <-ctx.Done():
		go func() {
			if outcome := <-results; outcome.handle != nil {
				_ = outcome.handle.Unmount()
			}
		}()
		return nil, fmt.Errorf("connect to %s: %w", spec.Endpoint.Host, ctx.Err())
	}
}

// prepare checks the server's identity against the pins and assembles the
// mount specification. secret overrides the stored credential when set.
func (s *Supervisor) prepare(ctx context.Context, endpoint config.Endpoint, secret *string) (mount.Spec, error) {
	spec := mount.Spec{Endpoint: endpoint}
	if secret != nil {
		spec.Secret.Password = *secret
	} else {
		username, password, err := s.credentials.Get(endpoint.CredentialKey)
		if err != nil {
			return spec, &attentionError{fmt.Errorf("load the saved credential: %w", err)}
		}
		if username != "" {
			spec.Endpoint.Username = username
		}
		spec.Secret.Password = password
	}
	if endpoint.AuthMethodOrDefault() == config.AuthKey {
		if _, err := os.Stat(endpoint.KeyFile); err != nil {
			return spec, &attentionError{fmt.Errorf("private key file: %w", err)}
		}
	}
	if err := s.verifyTrust(ctx, endpoint); err != nil {
		return spec, err
	}
	if endpoint.Protocol == config.SFTP {
		path := s.layout.KnownHostsFile(endpoint.ID)
		if err := os.MkdirAll(s.layout.KnownHostsDir(), 0o700); err != nil {
			return spec, err
		}
		if err := os.WriteFile(path, []byte(endpoint.KnownHost+"\n"), 0o600); err != nil {
			return spec, err
		}
		spec.KnownHostsFile = path
	}
	return spec, nil
}

// verifyTrust compares the server's current identity with the pin, so a
// changed host key or certificate is reported with both fingerprints
// instead of as a generic connection failure. rclone enforces the pin again
// on every connection it makes.
func (s *Supervisor) verifyTrust(ctx context.Context, endpoint config.Endpoint) error {
	switch endpoint.Protocol {
	case config.SFTP:
		if endpoint.KnownHost == "" {
			return security.ErrHostKeyNotPinned
		}
		presented, err := s.prober.SFTPHostKey(ctx, endpoint)
		if err != nil {
			return err
		}
		return security.CheckHostKey(endpoint, presented)
	case config.FTPS:
		presented, err := s.prober.TLSCertificate(ctx, endpoint)
		if err != nil {
			return err
		}
		return security.CheckCertificate(endpoint, presented)
	}
	return nil
}

// attentionError marks a failure that retrying cannot fix.
type attentionError struct{ err error }

func (e *attentionError) Error() string { return e.err.Error() }
func (e *attentionError) Unwrap() error { return e.err }

func needsAttention(err error) bool {
	var attention *attentionError
	var hostKey *security.HostKeyChangedError
	var certificate *security.CertificateChangedError
	return errors.As(err, &attention) || errors.As(err, &hostKey) || errors.As(err, &certificate) ||
		errors.Is(err, security.ErrHostKeyNotPinned) || errors.Is(err, security.ErrCertificateUntrusted) ||
		errors.Is(err, credentials.ErrNotFound)
}

func trustProblem(err error) *TrustProblem {
	var hostKey *security.HostKeyChangedError
	var certificate *security.CertificateChangedError
	switch {
	case errors.As(err, &hostKey):
		return &TrustProblem{Kind: TrustHostKeyChanged, Pinned: hostKey.Pinned.Fingerprint, Presented: hostKey.Presented.Fingerprint}
	case errors.As(err, &certificate):
		return &TrustProblem{Kind: TrustCertificateChanged, Pinned: certificate.Pinned, Presented: certificate.Presented.SHA256}
	case errors.Is(err, security.ErrHostKeyNotPinned):
		return &TrustProblem{Kind: TrustHostKeyNotPinned}
	case errors.Is(err, security.ErrCertificateUntrusted):
		return &TrustProblem{Kind: TrustCertificateUntrusted}
	}
	return nil
}
