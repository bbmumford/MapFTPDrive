//go:build windows

package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/sys/windows"
	"mapftpdrive/internal/config"
)

// showMapper runs the map/edit dialog. With options.Submit set, the dialog
// stays open while the submission (identity prompts, test, mount, save)
// runs in the background and closes only when it succeeds, so a failed
// attempt never loses what the user typed.
func showMapper(options MapperOptions) (MapperResult, error) {
	return runMapperDialog(nil, options)
}

func runMapperDialog(owner walk.Form, options MapperOptions) (MapperResult, error) {
	var dialog *walk.Dialog
	var nameEdit, hostEdit, portEdit, userEdit, passwordEdit, keyEdit, pathEdit *walk.LineEdit
	var protocolBox, driveBox *walk.ComboBox
	var keyCheck *walk.CheckBox
	var keyButton, cancelButton, okButton *walk.PushButton
	var passwordLabel, warningLabel, statusLabel *walk.Label
	var result MapperResult
	accepted := false
	busy := false

	values := FormValues{Scope: options.Scope, Port: DefaultPortText(0)}
	editing := options.Initial != nil
	if editing {
		values = FormFor(*options.Initial)
	}
	if options.Usage.Mask == 0 {
		if mask, err := windows.GetLogicalDrives(); err == nil {
			options.Usage.Mask = mask
		}
	}
	drives := AvailableLetters(options.Scope, values.Drive, options.Usage)
	if len(drives) == 0 {
		drives = []string{"Z:"}
	}
	driveIndex := 0
	for index, drive := range drives {
		if strings.EqualFold(drive, values.Drive) {
			driveIndex = index
		}
	}
	previousProtocol := values.ProtocolIndex

	title := "Map FTP Drive"
	okText := "Map Drive"
	passwordText := "Password:"
	if editing {
		title = "Edit FTP Drive"
		okText = "Save"
	}
	if options.Scope == config.System {
		title += " (system-wide)"
	}

	refresh := func() {
		protocol, _ := ProtocolAt(protocolBox.CurrentIndex())
		sftp := protocol == config.SFTP
		if !sftp && keyCheck.Checked() {
			keyCheck.SetChecked(false)
		}
		keyCheck.SetEnabled(sftp && !busy)
		useKey := keyCheck.Checked()
		keyEdit.SetEnabled(useKey && !busy)
		keyButton.SetEnabled(useKey && !busy)
		label := "Password:"
		if useKey {
			label = "Key passphrase:"
		}
		if editing {
			label = strings.TrimSuffix(label, ":") + " (blank keeps it):"
		}
		_ = passwordLabel.SetText(label)
		switch {
		case protocol == config.FTP:
			_ = warningLabel.SetText("FTP sends credentials and data without encryption. Prefer SFTP or FTPS.")
		case options.Scope == config.System:
			_ = warningLabel.SetText("System-wide drives are available to every user of this computer with these credentials.")
		default:
			_ = warningLabel.SetText("")
		}
	}
	setBusy := func(state bool, message string) {
		busy = state
		for _, widget := range []walk.Widget{nameEdit, hostEdit, portEdit, userEdit, passwordEdit, pathEdit, protocolBox, driveBox, okButton} {
			widget.SetEnabled(!state)
		}
		_ = statusLabel.SetText(message)
		refresh()
	}
	submit := func() {
		if busy {
			return
		}
		form := FormValues{
			Scope:         options.Scope,
			Name:          nameEdit.Text(),
			ProtocolIndex: protocolBox.CurrentIndex(),
			Host:          hostEdit.Text(),
			Port:          portEdit.Text(),
			Username:      userEdit.Text(),
			UseKey:        keyCheck.Checked(),
			KeyFile:       keyEdit.Text(),
			Password:      passwordEdit.Text(),
			RemotePath:    pathEdit.Text(),
			Drive:         driveBox.Text(),
		}
		built, err := BuildResult(form)
		if err == nil {
			editingID := ""
			if editing {
				editingID = options.Initial.ID
			}
			err = CheckLetter(built.Endpoint, editingID, options.Usage)
		}
		if err != nil {
			walk.MsgBox(dialog, "Check the drive details", err.Error(), walk.MsgBoxIconWarning)
			return
		}
		if options.Submit == nil {
			result, accepted = built, true
			dialog.Accept()
			return
		}
		setBusy(true, "Connecting to "+built.Endpoint.Host+"…")
		go func() {
			submitErr := options.Submit(built, walkPrompter{owner: dialog})
			dialog.Synchronize(func() {
				setBusy(false, "")
				switch {
				case submitErr == nil:
					result, accepted = built, true
					dialog.Accept()
				case errors.Is(submitErr, ErrCancelled):
				default:
					walk.MsgBox(dialog, "Unable to "+strings.ToLower(okText)+" the drive", submitErr.Error(), walk.MsgBoxIconError)
				}
			})
		}()
	}

	err := (Dialog{
		AssignTo:      &dialog,
		Title:         title,
		MinSize:       Size{Width: 560, Height: 470},
		Layout:        VBox{},
		DefaultButton: &okButton,
		CancelButton:  &cancelButton,
		Children: []Widget{
			Composite{
				Layout: Grid{Columns: 3, Spacing: 8},
				Children: []Widget{
					Label{Text: "Drive name:"},
					LineEdit{AssignTo: &nameEdit, Text: values.Name, MaxLength: config.MaxNameLength, ColumnSpan: 2},
					Label{Text: "Protocol:"},
					ComboBox{
						AssignTo:     &protocolBox,
						Model:        ProtocolChoices,
						CurrentIndex: values.ProtocolIndex,
						ColumnSpan:   2,
						OnCurrentIndexChanged: func() {
							next := protocolBox.CurrentIndex()
							_ = portEdit.SetText(NextPortText(portEdit.Text(), previousProtocol, next))
							previousProtocol = next
							refresh()
						},
					},
					Label{Text: "Server:"},
					LineEdit{AssignTo: &hostEdit, Text: values.Host, ColumnSpan: 2},
					Label{Text: "Port:"},
					LineEdit{AssignTo: &portEdit, Text: values.Port, MaxLength: 5, ColumnSpan: 2},
					Label{Text: "Username:"},
					LineEdit{AssignTo: &userEdit, Text: values.Username, ColumnSpan: 2},
					HSpacer{},
					CheckBox{
						AssignTo:         &keyCheck,
						Text:             "Log in with a private key (SFTP)",
						Checked:          values.UseKey,
						ColumnSpan:       2,
						OnCheckedChanged: func() { refresh() },
					},
					Label{Text: "Key file:"},
					LineEdit{AssignTo: &keyEdit, Text: values.KeyFile},
					PushButton{
						AssignTo: &keyButton,
						Text:     "Browse…",
						OnClicked: func() {
							picker := walk.FileDialog{Title: "Choose the private key file", Filter: "All files (*.*)|*.*"}
							if ok, _ := picker.ShowOpen(dialog); ok {
								_ = keyEdit.SetText(picker.FilePath)
							}
						},
					},
					Label{AssignTo: &passwordLabel, Text: passwordText},
					LineEdit{AssignTo: &passwordEdit, PasswordMode: true, ColumnSpan: 2},
					Label{Text: "Remote path:"},
					LineEdit{AssignTo: &pathEdit, Text: values.RemotePath, ColumnSpan: 2},
					Label{Text: "Drive letter:"},
					ComboBox{AssignTo: &driveBox, Model: drives, CurrentIndex: driveIndex, ColumnSpan: 2},
				},
			},
			Label{AssignTo: &warningLabel},
			Label{AssignTo: &statusLabel},
			VSpacer{},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &cancelButton,
						Text:     "Cancel",
						OnClicked: func() {
							if !busy {
								dialog.Cancel()
							}
						},
					},
					PushButton{AssignTo: &okButton, Text: okText, OnClicked: submit},
				},
			},
		},
	}).Create(owner)
	if err != nil {
		return MapperResult{}, err
	}
	// A submission in flight reports back through dialog.Synchronize, so
	// the dialog must outlive it.
	dialog.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if busy {
			*canceled = true
		}
	})
	refresh()
	dialog.Run()
	if !accepted {
		return MapperResult{}, ErrCancelled
	}
	return result, nil
}

func confirmRemove(name, drive string) bool {
	return walk.MsgBox(nil, "Remove FTP Drive", RemovalMessage(name, drive), walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) == walk.DlgCmdYes
}

func confirmHostKey(host, algorithm, fingerprint string) bool {
	message := fmt.Sprintf(
		"First connection to %s.\n\nHost key type: %s\nSHA-256 fingerprint: %s\n\nTrust and pin this host key?",
		host,
		algorithm,
		fingerprint,
	)
	return walk.MsgBox(nil, "Verify SFTP host key", message, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
}

func promptRename(current string) (string, error) {
	return runRenameDialog(nil, current)
}

// runRenameDialog asks for a new drive name, validated as a volume label.
func runRenameDialog(owner walk.Form, current string) (string, error) {
	var dialog *walk.Dialog
	var edit *walk.LineEdit
	var okButton, cancelButton *walk.PushButton
	var name string
	accepted := false
	_, err := (Dialog{
		AssignTo:      &dialog,
		Title:         "Rename FTP Drive",
		MinSize:       Size{Width: 380, Height: 150},
		Layout:        VBox{},
		DefaultButton: &okButton,
		CancelButton:  &cancelButton,
		Children: []Widget{
			Label{Text: fmt.Sprintf("New drive name (up to %d characters, no \\ / or commas):", config.MaxNameLength)},
			LineEdit{AssignTo: &edit, Text: current, MaxLength: config.MaxNameLength},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &cancelButton, Text: "Cancel", OnClicked: func() { dialog.Cancel() }},
					PushButton{AssignTo: &okButton, Text: "Rename", OnClicked: func() {
						candidate := strings.TrimSpace(edit.Text())
						if err := config.ValidateName(candidate); err != nil {
							walk.MsgBox(dialog, "Rename FTP Drive", err.Error(), walk.MsgBoxIconWarning)
							return
						}
						name, accepted = candidate, true
						dialog.Accept()
					}},
				},
			},
		},
	}).Run(owner)
	if err != nil {
		return "", err
	}
	if !accepted {
		return "", ErrCancelled
	}
	return name, nil
}

func showError(title string, err error) {
	walk.MsgBox(nil, title, err.Error(), walk.MsgBoxIconError)
}
