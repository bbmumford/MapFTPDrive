//go:build windows

package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/sys/windows"
	"mapftpdrive/internal/config"
)

func showMapper(scope config.Scope) (MapperResult, error) {
	var window *walk.MainWindow
	var nameEdit, hostEdit, portEdit, userEdit, passwordEdit, pathEdit *walk.LineEdit
	var protocolBox, driveBox *walk.ComboBox
	var result MapperResult
	cancelled := true
	protocols := []string{"SFTP", "FTPS (explicit TLS)", "FTPS (implicit TLS)", "FTP (unencrypted)"}
	drives := availableDriveLetters()

	title := "Map FTP Drive"
	if scope == config.System {
		title += " — system-wide"
	}
	_, err := (MainWindow{
		AssignTo: &window,
		Title:    title,
		MinSize:  Size{Width: 520, Height: 430},
		Layout:   VBox{MarginsZero: false},
		Children: []Widget{
			Label{
				Text: "Create a persistent Windows drive backed by an FTP-family endpoint.",
			},
			Composite{
				Layout: Grid{Columns: 2, Spacing: 8},
				Children: []Widget{
					Label{Text: "Drive name:"},
					LineEdit{AssignTo: &nameEdit},
					Label{Text: "Protocol:"},
					ComboBox{AssignTo: &protocolBox, Model: protocols, CurrentIndex: 0},
					Label{Text: "Server:"},
					LineEdit{AssignTo: &hostEdit},
					Label{Text: "Port:"},
					LineEdit{AssignTo: &portEdit, Text: "22"},
					Label{Text: "Username:"},
					LineEdit{AssignTo: &userEdit},
					Label{Text: "Password:"},
					LineEdit{AssignTo: &passwordEdit, PasswordMode: true},
					Label{Text: "Remote path:"},
					LineEdit{AssignTo: &pathEdit},
					Label{Text: "Drive letter:"},
					ComboBox{AssignTo: &driveBox, Model: drives, CurrentIndex: 0},
				},
			},
			Label{
				Text: "FTP sends credentials and data without encryption. Prefer SFTP or FTPS.",
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "Cancel",
						OnClicked: func() {
							window.Close()
						},
					},
					PushButton{
						Text: "Map Drive",
						OnClicked: func() {
							port, parseErr := strconv.ParseUint(strings.TrimSpace(portEdit.Text()), 10, 16)
							if parseErr != nil || port == 0 {
								walk.MsgBox(window, "Invalid port", "Enter a port from 1 through 65535.", walk.MsgBoxIconWarning)
								return
							}
							protocol := config.SFTP
							var tlsMode config.TLSMode
							switch protocolBox.CurrentIndex() {
							case 1:
								protocol = config.FTPS
								tlsMode = config.TLSExplicit
							case 2:
								protocol = config.FTPS
								tlsMode = config.TLSImplicit
							case 3:
								protocol = config.FTP
								answer := walk.MsgBox(window, "Unencrypted connection", "FTP is unencrypted. Continue anyway?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning)
								if answer != walk.DlgCmdYes {
									return
								}
							}
							result = MapperResult{
								Endpoint: config.Endpoint{
									Name:        nameEdit.Text(),
									Protocol:    protocol,
									TLSMode:     tlsMode,
									Host:        hostEdit.Text(),
									Port:        uint16(port),
									Username:    userEdit.Text(),
									DriveLetter: strings.TrimSuffix(driveBox.Text(), ":"),
									RemotePath:  pathEdit.Text(),
									Scope:       scope,
								},
								Password: passwordEdit.Text(),
							}
							probe := result.Endpoint
							probe.ID = "pending"
							probe.CredentialKey = "pending"
							if validationErr := probe.Validate(); validationErr != nil {
								walk.MsgBox(window, "Invalid endpoint", validationErr.Error(), walk.MsgBoxIconWarning)
								return
							}
							cancelled = false
							window.Close()
						},
					},
				},
			},
		},
	}).Run()
	if err != nil {
		return MapperResult{}, err
	}
	if cancelled {
		return MapperResult{}, ErrCancelled
	}
	return result, nil
}

func availableDriveLetters() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		mask = 0
	}
	var result []string
	for letter := 'D'; letter <= 'Z'; letter++ {
		bit := uint32(1) << (letter - 'A')
		if mask&bit == 0 {
			result = append(result, fmt.Sprintf("%c:", letter))
		}
	}
	if len(result) == 0 {
		result = append(result, "Z:")
	}
	return result
}

func confirmRemove(name, drive string) bool {
	message := fmt.Sprintf("Remove %s (%s:) and delete its saved credential?", name, strings.TrimRight(drive, `:\`))
	return walk.MsgBox(nil, "Remove FTP Drive", message, walk.MsgBoxYesNo|walk.MsgBoxIconWarning) == walk.DlgCmdYes
}

func confirmHostKey(host, algorithm, fingerprint string) bool {
	message := fmt.Sprintf(
		"First connection to %s.\n\nHost key type: %s\nFingerprint: %s\n\nTrust and save this host key?",
		host,
		algorithm,
		fingerprint,
	)
	return walk.MsgBox(nil, "Verify SFTP host key", message, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
}

func promptRename(current string) (string, error) {
	var dialog *walk.Dialog
	var edit *walk.LineEdit
	var accepted bool
	_, err := (Dialog{
		AssignTo: &dialog,
		Title:    "Rename FTP Drive",
		MinSize:  Size{Width: 380, Height: 140},
		Layout:   VBox{},
		Children: []Widget{
			Label{Text: "New drive name:"},
			LineEdit{AssignTo: &edit, Text: current},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{Text: "Cancel", OnClicked: func() { dialog.Cancel() }},
					PushButton{Text: "Rename", OnClicked: func() {
						if strings.TrimSpace(edit.Text()) == "" {
							return
						}
						accepted = true
						dialog.Accept()
					}},
				},
			},
		},
	}).Run(nil)
	if err != nil {
		return "", err
	}
	if !accepted {
		return "", ErrCancelled
	}
	return strings.TrimSpace(edit.Text()), nil
}

func showError(title string, err error) {
	walk.MsgBox(nil, title, err.Error(), walk.MsgBoxIconError)
}
