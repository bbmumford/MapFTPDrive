package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/security"
)

// Host sends requests to one scope's mount host, starting it if needed.
type Host interface {
	Call(context.Context, control.Request) (control.Response, error)
}

// Scanner reads a server's identity for the trust prompts.
type Scanner interface {
	SFTPHostKey(context.Context, config.Endpoint) (security.HostKey, error)
	TLSCertificate(context.Context, config.Endpoint) (security.Certificate, error)
}

type networkScanner struct{}

func (networkScanner) SFTPHostKey(ctx context.Context, endpoint config.Endpoint) (security.HostKey, error) {
	return security.ScanSFTPHostKeyContext(ctx, endpoint)
}

func (networkScanner) TLSCertificate(ctx context.Context, endpoint config.Endpoint) (security.Certificate, error) {
	return security.ScanTLSCertificate(ctx, endpoint)
}

// NetworkScanner scans real servers.
var NetworkScanner Scanner = networkScanner{}

// Prompter asks the user to trust a server identity. Every method returns
// true only on an explicit yes.
type Prompter interface {
	ConfirmPlainFTP() bool
	ConfirmHostKey(host string, key security.HostKey) bool
	ConfirmHostKeyChange(host string, pinned, presented security.HostKey) bool
	ConfirmCertificate(host string, certificate security.Certificate) bool
	ConfirmCertificateChange(host, pinned string, presented security.Certificate) bool
}

// PinIdentity scans the server and returns the endpoint with the identity
// the user confirmed: a pinned SFTP host key, a pinned FTPS certificate
// digest (only for certificates Windows does not trust), or confirmation
// that plain FTP is acceptable. An unchanged pin is kept without asking; a
// changed one is shown old against new before it is replaced.
func PinIdentity(ctx context.Context, endpoint config.Endpoint, scanner Scanner, prompter Prompter) (config.Endpoint, error) {
	switch endpoint.Protocol {
	case config.FTP:
		if !prompter.ConfirmPlainFTP() {
			return endpoint, ErrCancelled
		}
	case config.SFTP:
		presented, err := scanner.SFTPHostKey(ctx, endpoint)
		if err != nil {
			return endpoint, fmt.Errorf("unable to read the SFTP server's host key: %w", err)
		}
		if endpoint.KnownHost != "" {
			pinned, parseErr := security.ParseKnownHost(endpoint.KnownHost)
			if parseErr == nil && pinned.SameKey(presented) {
				return endpoint, nil
			}
			if parseErr == nil {
				if !prompter.ConfirmHostKeyChange(endpoint.Host, pinned, presented) {
					return endpoint, ErrCancelled
				}
				endpoint.KnownHost = presented.Line
				return endpoint, nil
			}
		}
		if !prompter.ConfirmHostKey(endpoint.Host, presented) {
			return endpoint, ErrCancelled
		}
		endpoint.KnownHost = presented.Line
	case config.FTPS:
		presented, err := scanner.TLSCertificate(ctx, endpoint)
		if err != nil {
			return endpoint, fmt.Errorf("unable to read the FTPS server's certificate: %w", err)
		}
		switch {
		case endpoint.CertificatePin != "" && strings.EqualFold(endpoint.CertificatePin, presented.SHA256):
		case endpoint.CertificatePin != "":
			if !prompter.ConfirmCertificateChange(endpoint.Host, endpoint.CertificatePin, presented) {
				return endpoint, ErrCancelled
			}
			endpoint.CertificatePin = presented.SHA256
			if presented.Trusted {
				endpoint.CertificatePin = ""
			}
		case presented.Trusted:
		default:
			if !prompter.ConfirmCertificate(endpoint.Host, presented) {
				return endpoint, ErrCancelled
			}
			endpoint.CertificatePin = presented.SHA256
		}
	}
	return endpoint, nil
}

// LetterUsage is what the mapper knows about drive letters: the
// GetLogicalDrives mask of this process and every drive MapFTPDrive manages
// in either scope (the user's store and the system drive index).
type LetterUsage struct {
	Mask    uint32
	Managed []config.DriveIndexEntry
}

// CheckLetter refuses a letter Windows shows in use or another managed
// drive holds. editingID is the drive being edited, whose own letter is
// fine. A disabled drive of the same scope may be taken over: Map reuses or
// replaces it.
func CheckLetter(endpoint config.Endpoint, editingID string, usage LetterUsage) error {
	letter := config.NormalizeDrive(endpoint.DriveLetter)
	if len(letter) != 1 || letter[0] < 'A' || letter[0] > 'Z' {
		return errors.New("choose a drive letter from A to Z")
	}
	for _, managed := range usage.Managed {
		if managed.Drive != letter || managed.ID == editingID {
			continue
		}
		if managed.Scope == endpoint.Scope && managed.Disabled && editingID == "" {
			continue
		}
		scope := "your"
		if managed.Scope == config.System {
			scope = "the system-wide"
		}
		return fmt.Errorf("drive %s: is already used by %s drive %q; choose another letter", letter, scope, managed.Name)
	}
	editingSameLetter := false
	for _, managed := range usage.Managed {
		if managed.ID == editingID && editingID != "" && managed.Drive == letter {
			editingSameLetter = true
		}
	}
	if !editingSameLetter && usage.Mask&(1<<(letter[0]-'A')) != 0 {
		return fmt.Errorf("drive %s: is already in use on this computer; choose another letter", letter)
	}
	return nil
}

// AvailableLetters lists D: to Z: minus letters in use, keeping current
// (the letter of the drive being edited) and same-scope disabled drives,
// which Map can take over.
func AvailableLetters(scope config.Scope, current string, usage LetterUsage) []string {
	current = config.NormalizeDrive(current)
	taken := make(map[string]bool)
	for _, managed := range usage.Managed {
		if !(managed.Scope == scope && managed.Disabled) {
			taken[managed.Drive] = true
		}
	}
	var letters []string
	for letter := 'D'; letter <= 'Z'; letter++ {
		name := string(letter)
		inUse := usage.Mask&(1<<(letter-'A')) != 0
		if name == current || (!inUse && !taken[name]) {
			letters = append(letters, name+":")
		}
	}
	return letters
}

// DescribeState renders a status for the management window.
func DescribeState(status control.EndpointStatus, now time.Time) string {
	switch status.State {
	case control.StateMounted:
		return "Mounted"
	case control.StateDisabled:
		return "Unmounted"
	case control.StateConnecting:
		if status.LastError != "" {
			return "Connecting (" + status.LastError + ")"
		}
		return "Connecting…"
	case control.StateRetrying:
		wait := status.RetryAt.Sub(now).Round(time.Second)
		if wait < time.Second {
			wait = 0
		}
		return fmt.Sprintf("Retrying in %s: %s", wait, status.LastError)
	case control.StateAttention:
		return "Needs attention: " + status.LastError
	}
	return string(status.State)
}

// DescribeProtocol names a definition's protocol for display.
func DescribeProtocol(endpoint config.Endpoint) string {
	switch {
	case endpoint.Protocol == config.SFTP:
		return "SFTP"
	case endpoint.Protocol == config.FTPS && endpoint.TLSMode == config.TLSImplicit:
		return "FTPS (implicit)"
	case endpoint.Protocol == config.FTPS:
		return "FTPS (explicit)"
	default:
		return "FTP (unencrypted)"
	}
}

// Row is one line of the management window.
type Row struct {
	Status  control.EndpointStatus
	Scope   config.Scope
	Drive   string
	Name    string
	Server  string
	Kind    string
	State   string
	Problem *control.TrustProblem
}

// Rows flattens status reports into display rows ordered by scope, then
// drive letter.
func Rows(reports []*control.StatusReport, now time.Time) []Row {
	var rows []Row
	for _, report := range reports {
		if report == nil {
			continue
		}
		for _, status := range report.Endpoints {
			endpoint := status.Endpoint
			server := endpoint.Host
			if endpoint.Port != 0 && endpoint.Port != config.DefaultPort(endpoint.Protocol, endpoint.TLSMode) {
				server = fmt.Sprintf("%s:%d", endpoint.Host, endpoint.Port)
			}
			if endpoint.RemotePath != "" {
				server += "/" + strings.TrimPrefix(endpoint.RemotePath, "/")
			}
			rows = append(rows, Row{
				Status:  status,
				Scope:   report.Scope,
				Drive:   endpoint.DriveLetter + ":",
				Name:    endpoint.Name,
				Server:  server,
				Kind:    DescribeProtocol(endpoint),
				State:   DescribeState(status, now),
				Problem: status.Trust,
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Scope != rows[j].Scope {
			return rows[i].Scope == config.User
		}
		return rows[i].Drive < rows[j].Drive
	})
	return rows
}

// ReportProblems summarises store problems for the window's status line.
func ReportProblems(reports []*control.StatusReport) string {
	var parts []string
	for _, report := range reports {
		if report == nil {
			continue
		}
		if report.StoreError != "" {
			parts = append(parts, fmt.Sprintf("%s drives: %s", report.Scope, report.StoreError))
		}
		if count := len(report.Problems); count > 0 {
			parts = append(parts, fmt.Sprintf("%d invalid %s drive definition(s) skipped", count, report.Scope))
		}
	}
	return strings.Join(parts, "; ")
}

// SubmitMap pins the server identity and asks the host to test, mount and
// save the new drive. editing is the stored definition being edited, or
// nil for a new drive; an empty password keeps the stored secret.
func SubmitMap(ctx context.Context, host Host, scanner Scanner, prompter Prompter, result MapperResult, editing *config.Endpoint) (config.Endpoint, error) {
	endpoint := result.Endpoint.Normalized()
	if editing != nil {
		endpoint.ID = editing.ID
		endpoint.CredentialKey = editing.CredentialKey
		if endpoint.Protocol == editing.Protocol && strings.EqualFold(endpoint.Host, editing.Host) && endpoint.Port == editing.Port {
			endpoint.KnownHost = editing.KnownHost
			endpoint.CertificatePin = editing.CertificatePin
		}
	}
	endpoint, err := PinIdentity(ctx, endpoint, scanner, prompter)
	if err != nil {
		return endpoint, err
	}
	operation := control.Map
	if editing != nil {
		operation = control.Update
	}
	sent := endpoint
	response, err := host.Call(ctx, control.Request{Operation: operation, Endpoint: &sent, Password: result.Password})
	if err != nil {
		return endpoint, err
	}
	if response.Endpoint != nil {
		endpoint = *response.Endpoint
	}
	return endpoint, nil
}

// Repin re-reads the server identity of a stored drive, shows the pinned
// and presented fingerprints, and on confirmation sends the new pin.
func Repin(ctx context.Context, host Host, scanner Scanner, prompter Prompter, endpoint config.Endpoint) (string, error) {
	request := control.Request{Operation: control.Repin, ID: endpoint.ID}
	switch endpoint.Protocol {
	case config.SFTP:
		presented, err := scanner.SFTPHostKey(ctx, endpoint)
		if err != nil {
			return "", fmt.Errorf("unable to read the SFTP server's host key: %w", err)
		}
		pinned, parseErr := security.ParseKnownHost(endpoint.KnownHost)
		var confirmed bool
		if parseErr == nil {
			confirmed = prompter.ConfirmHostKeyChange(endpoint.Host, pinned, presented)
		} else {
			confirmed = prompter.ConfirmHostKey(endpoint.Host, presented)
		}
		if !confirmed {
			return "", ErrCancelled
		}
		request.KnownHost = presented.Line
	case config.FTPS:
		presented, err := scanner.TLSCertificate(ctx, endpoint)
		if err != nil {
			return "", fmt.Errorf("unable to read the FTPS server's certificate: %w", err)
		}
		var confirmed bool
		if endpoint.CertificatePin != "" {
			confirmed = prompter.ConfirmCertificateChange(endpoint.Host, endpoint.CertificatePin, presented)
		} else {
			confirmed = prompter.ConfirmCertificate(endpoint.Host, presented)
		}
		if !confirmed {
			return "", ErrCancelled
		}
		request.CertificatePin = presented.SHA256
	default:
		return "", errors.New("plain FTP has no server identity to pin")
	}
	response, err := host.Call(ctx, request)
	return response.Message, err
}
