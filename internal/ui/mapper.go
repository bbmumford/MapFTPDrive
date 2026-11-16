// Package ui is MapFTPDrive's native interface: the map/edit dialog, the
// management window and the confirmation prompts. Form logic, letter checks
// and workflow decisions live in portable files so they are tested on any
// OS; the lxn/walk windows are Windows-only.
package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"mapftpdrive/internal/config"
)

var ErrCancelled = errors.New("mapping cancelled")

type MapperResult struct {
	Endpoint config.Endpoint
	// Password is the account password, or the private-key passphrase for
	// key authentication; empty when editing keeps the stored secret.
	Password string
}

// MapperOptions configure the map/edit dialog.
type MapperOptions struct {
	Scope config.Scope
	// Initial pre-fills the dialog for editing a stored drive.
	Initial *config.Endpoint
	Usage   LetterUsage
	// Submit runs off the UI thread once the form is valid; prompts it
	// raises are shown over the dialog. An error is shown and the dialog
	// stays open with the user's input; nil closes it.
	Submit func(MapperResult, Prompter) error
}

func ShowMapper(scope config.Scope) (MapperResult, error) {
	return showMapper(MapperOptions{Scope: scope})
}

// ShowMapperWith shows the dialog with options and returns the accepted
// result.
func ShowMapperWith(options MapperOptions) (MapperResult, error) {
	return showMapper(options)
}

func ConfirmRemove(name, drive string) bool {
	return confirmRemove(name, drive)
}

func PromptRename(current string) (string, error) {
	return promptRename(current)
}

func ConfirmHostKey(host, algorithm, fingerprint string) bool {
	return confirmHostKey(host, algorithm, fingerprint)
}

var (
	shownMu   sync.Mutex
	lastShown string
	// showError puts an error in front of the user; tests replace it, since
	// a message box waits for a click no test runner gives.
	showError = showErrorBox
)

// ShowError shows err in a message box and remembers it, so the command
// dispatcher does not show the same error a second time.
func ShowError(title string, err error) {
	shownMu.Lock()
	lastShown = err.Error()
	shownMu.Unlock()
	showError(title, err)
}

// WasShown reports whether err (or an error with the same text) was the
// last one shown by ShowError.
func WasShown(err error) bool {
	shownMu.Lock()
	defer shownMu.Unlock()
	return err != nil && lastShown != "" && strings.HasSuffix(err.Error(), lastShown)
}

// ProtocolChoices are the dialog's protocol options, in index order.
var ProtocolChoices = []string{"SFTP", "FTPS (explicit TLS)", "FTPS (implicit TLS)", "FTP (unencrypted)"}

// ProtocolAt maps a ProtocolChoices index to a protocol and FTPS mode.
func ProtocolAt(index int) (config.Protocol, config.TLSMode) {
	switch index {
	case 1:
		return config.FTPS, config.TLSExplicit
	case 2:
		return config.FTPS, config.TLSImplicit
	case 3:
		return config.FTP, ""
	default:
		return config.SFTP, ""
	}
}

// ProtocolIndex is the ProtocolChoices index of a stored definition.
func ProtocolIndex(endpoint config.Endpoint) int {
	switch {
	case endpoint.Protocol == config.FTPS && endpoint.TLSMode == config.TLSImplicit:
		return 2
	case endpoint.Protocol == config.FTPS:
		return 1
	case endpoint.Protocol == config.FTP:
		return 3
	default:
		return 0
	}
}

// DefaultPortText is the port shown for a protocol choice.
func DefaultPortText(index int) string {
	protocol, mode := ProtocolAt(index)
	return strconv.Itoa(int(config.DefaultPort(protocol, mode)))
}

// NextPortText keeps a port the user typed and otherwise follows the
// protocol: an empty field or the previous protocol's default becomes the
// new protocol's default.
func NextPortText(current string, previousIndex, nextIndex int) string {
	current = strings.TrimSpace(current)
	if current == "" || current == DefaultPortText(previousIndex) {
		return DefaultPortText(nextIndex)
	}
	return current
}

// FormValues are the raw dialog fields.
type FormValues struct {
	Scope         config.Scope
	Name          string
	ProtocolIndex int
	Host          string
	Port          string
	Username      string
	UseKey        bool
	KeyFile       string
	Password      string
	RemotePath    string
	Drive         string
}

// BuildResult validates the form and returns the definition it describes.
// The ID is left empty; the host assigns it (or keeps the edited one).
func BuildResult(values FormValues) (MapperResult, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(values.Port), 10, 16)
	if err != nil || port == 0 {
		return MapperResult{}, errors.New("enter a port from 1 through 65535")
	}
	if err := config.ValidateName(values.Name); err != nil {
		return MapperResult{}, err
	}
	protocol, mode := ProtocolAt(values.ProtocolIndex)
	endpoint := config.Endpoint{
		Name:        values.Name,
		Protocol:    protocol,
		TLSMode:     mode,
		Host:        values.Host,
		Port:        uint16(port),
		Username:    values.Username,
		DriveLetter: values.Drive,
		RemotePath:  values.RemotePath,
		Scope:       values.Scope,
	}
	if values.UseKey {
		if protocol != config.SFTP {
			return MapperResult{}, errors.New("private-key login is available only for SFTP")
		}
		endpoint.Auth = config.AuthKey
		endpoint.KeyFile = strings.TrimSpace(values.KeyFile)
	}
	probe := endpoint.Normalized()
	probe.ID = "00000000-0000-4000-8000-000000000000"
	probe.CredentialKey = config.CredentialKeyFor(probe.ID)
	if err := probe.Validate(); err != nil {
		return MapperResult{}, err
	}
	return MapperResult{Endpoint: endpoint.Normalized(), Password: values.Password}, nil
}

// FormFor pre-fills the form from a stored definition.
func FormFor(endpoint config.Endpoint) FormValues {
	return FormValues{
		Scope:         endpoint.Scope,
		Name:          endpoint.Name,
		ProtocolIndex: ProtocolIndex(endpoint),
		Host:          endpoint.Host,
		Port:          strconv.Itoa(int(endpoint.Port)),
		Username:      endpoint.Username,
		UseKey:        endpoint.AuthMethodOrDefault() == config.AuthKey,
		KeyFile:       endpoint.KeyFile,
		RemotePath:    endpoint.RemotePath,
		Drive:         endpoint.DriveLetter + ":",
	}
}

// RemovalMessage is the confirmation text for Remove.
func RemovalMessage(name, drive string) string {
	return fmt.Sprintf("Remove %s (%s:)?\n\nThis unmounts the drive and deletes its definition, saved password or passphrase, pinned server identity and local cache. Changes still waiting in the cache to be uploaded are lost.",
		name, config.NormalizeDrive(drive))
}
