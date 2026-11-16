//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/sys/windows"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
)

// actionTimeout bounds one request from the window. It is the host's own
// control.RequestTimeout, within which the host answers (reporting progress
// meanwhile), so the host's answer, not a client timeout, is what the user
// sees.
const actionTimeout = control.RequestTimeout

type rowModel struct {
	walk.TableModelBase
	rows []Row
}

func (m *rowModel) RowCount() int { return len(m.rows) }

func (m *rowModel) Value(row, column int) interface{} {
	entry := m.rows[row]
	switch column {
	case 0:
		return entry.Drive
	case 1:
		return entry.Name
	case 2:
		if entry.Scope == config.System {
			return "System-wide"
		}
		return "Only me"
	case 3:
		return entry.Server
	case 4:
		return entry.Kind
	default:
		return entry.State
	}
}

type manager struct {
	options  ManagerOptions
	window   *walk.MainWindow
	table    *walk.TableView
	model    *rowModel
	status   *walk.Label
	buttons  map[string]*walk.PushButton
	reports  []*control.StatusReport
	busy     bool
	stop     chan struct{}
	selected string
}

func showManager(options ManagerOptions) error {
	if options.Scanner == nil {
		options.Scanner = NetworkScanner
	}
	m := &manager{options: options, model: &rowModel{}, stop: make(chan struct{})}
	title := "Manage FTP Drives"
	if _, system := options.Hosts[config.System]; system {
		title += " (administrator)"
	}
	var mount, unmount, edit, rename, remove, test, repin *walk.PushButton
	actions := []Widget{
		PushButton{Text: "Map new drive…", OnClicked: func() { m.mapNew(config.User) }},
	}
	if _, ok := options.Hosts[config.System]; ok {
		actions = append(actions, PushButton{Text: "Map system-wide drive…", OnClicked: func() { m.mapNew(config.System) }})
	}
	actions = append(actions,
		PushButton{AssignTo: &mount, Text: "Mount", OnClicked: func() { m.simple(control.Mount, "Mounted") }},
		PushButton{AssignTo: &unmount, Text: "Unmount", OnClicked: func() { m.simple(control.Unmount, "Unmounted") }},
		PushButton{AssignTo: &edit, Text: "Edit…", OnClicked: m.edit},
		PushButton{AssignTo: &rename, Text: "Rename…", OnClicked: m.rename},
		PushButton{AssignTo: &remove, Text: "Remove…", OnClicked: m.remove},
		HSpacer{},
	)
	secondRow := []Widget{
		PushButton{AssignTo: &test, Text: "Test connection", OnClicked: m.test},
		PushButton{AssignTo: &repin, Text: "Re-pin server identity…", OnClicked: m.repin},
		PushButton{Text: "Refresh", OnClicked: func() { go m.reload() }},
	}
	if options.OpenSystem != nil {
		secondRow = append(secondRow, PushButton{Text: "System-wide drives…", OnClicked: func() {
			if err := options.OpenSystem(); err != nil {
				walk.MsgBox(m.window, title, err.Error(), walk.MsgBoxIconError)
			}
		}})
	}
	secondRow = append(secondRow, HSpacer{})

	err := (MainWindow{
		AssignTo: &m.window,
		Title:    title,
		MinSize:  Size{Width: 820, Height: 420},
		Size:     Size{Width: 900, Height: 480},
		Layout:   VBox{},
		Children: []Widget{
			TableView{
				AssignTo:            &m.table,
				AlternatingRowBG:    true,
				LastColumnStretched: true,
				Columns: []TableViewColumn{
					{Title: "Drive", Width: 50},
					{Title: "Name", Width: 150},
					{Title: "Available to", Width: 90},
					{Title: "Server", Width: 200},
					{Title: "Protocol", Width: 110},
					{Title: "Status"},
				},
				Model:                 m.model,
				OnCurrentIndexChanged: m.selectionChanged,
				OnItemActivated:       m.edit,
			},
			Composite{Layout: HBox{MarginsZero: true}, Children: actions},
			Composite{Layout: HBox{MarginsZero: true}, Children: secondRow},
			Label{AssignTo: &m.status},
		},
	}).Create()
	if err != nil {
		return err
	}
	m.buttons = map[string]*walk.PushButton{
		"mount": mount, "unmount": unmount, "edit": edit, "rename": rename,
		"remove": remove, "test": test, "repin": repin,
	}
	m.window.Closing().Attach(func(*bool, walk.CloseReason) {
		select {
		case <-m.stop:
		default:
			close(m.stop)
		}
	})
	m.selectionChanged()
	go m.poll()
	m.window.Run()
	return nil
}

// poll refreshes status until the window closes.
func (m *manager) poll() {
	m.reload()
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.reload()
		}
	}
}

func (m *manager) reload() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	reports := fetchReports(ctx, m.options.Hosts)
	cancel()
	select {
	case <-m.stop:
		return
	default:
	}
	m.window.Synchronize(func() {
		m.reports = reports
		m.model.rows = Rows(reports, time.Now())
		m.model.PublishRowsReset()
		for index, row := range m.model.rows {
			if row.Status.Endpoint.ID == m.selected {
				_ = m.table.SetCurrentIndex(index)
			}
		}
		if !m.busy {
			problems := ReportProblems(reports)
			if problems == "" && len(m.model.rows) == 0 {
				problems = "No drives yet. Choose Map new drive… to add one."
			}
			m.status.SetText(problems)
		}
		m.selectionChanged()
	})
}

func (m *manager) current() (Row, bool) {
	index := m.table.CurrentIndex()
	if index < 0 || index >= len(m.model.rows) {
		return Row{}, false
	}
	return m.model.rows[index], true
}

func (m *manager) selectionChanged() {
	row, ok := m.current()
	if ok {
		m.selected = row.Status.Endpoint.ID
	}
	enable := func(key string, state bool) {
		if button := m.buttons[key]; button != nil {
			button.SetEnabled(state && !m.busy)
		}
	}
	for _, key := range []string{"edit", "rename", "remove", "test"} {
		enable(key, ok)
	}
	enable("mount", ok && row.Status.State != control.StateMounted)
	enable("unmount", ok && row.Status.State != control.StateDisabled)
	enable("repin", ok && row.Status.Endpoint.Protocol != config.FTP)
	if button := m.buttons["repin"]; button != nil {
		text := "Re-pin server identity…"
		switch {
		case ok && row.Status.Endpoint.Protocol == config.SFTP:
			text = "Re-pin host key…"
		case ok && row.Status.Endpoint.Protocol == config.FTPS:
			text = "Re-pin certificate…"
		}
		button.SetText(text)
	}
}

// run performs action off the UI thread with the window's actions
// disabled, then reports the outcome on the UI thread.
func (m *manager) run(progress string, action func(context.Context) (string, error)) {
	if m.busy {
		return
	}
	m.busy = true
	m.status.SetText(progress)
	m.selectionChanged()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		message, err := action(ctx)
		cancel()
		m.window.Synchronize(func() {
			m.busy = false
			m.status.SetText(message)
			m.selectionChanged()
			if err != nil && !errors.Is(err, ErrCancelled) {
				m.status.SetText("")
				walk.MsgBox(m.window, "MapFTPDrive", err.Error(), walk.MsgBoxIconError)
			}
		})
		m.reload()
	}()
}

func (m *manager) host(scope config.Scope) (Host, error) {
	host, ok := m.options.Hosts[scope]
	if !ok {
		return nil, fmt.Errorf("%s drives can only be managed as an administrator", scope)
	}
	return host, nil
}

func (m *manager) usage() LetterUsage {
	mask, _ := windows.GetLogicalDrives()
	usage := UsageFromReports(mask, m.reports)
	if m.options.Usage != nil {
		extra := m.options.Usage()
		usage.Mask |= extra.Mask
		usage.Managed = append(usage.Managed, extra.Managed...)
	}
	return usage
}

func (m *manager) simple(operation control.Operation, done string) {
	row, ok := m.current()
	if !ok {
		return
	}
	m.run(fmt.Sprintf("%s %s…", operation, row.Drive), func(ctx context.Context) (string, error) {
		host, err := m.host(row.Scope)
		if err != nil {
			return "", err
		}
		response, err := host.Call(ctx, control.Request{Operation: operation, ID: row.Status.Endpoint.ID})
		if err != nil {
			return "", err
		}
		if response.Message != "" {
			return fmt.Sprintf("%s: %s", row.Drive, response.Message), nil
		}
		return fmt.Sprintf("%s %s", done, row.Drive), nil
	})
}

func (m *manager) mapNew(scope config.Scope) {
	host, err := m.host(scope)
	if err != nil {
		walk.MsgBox(m.window, "MapFTPDrive", err.Error(), walk.MsgBoxIconError)
		return
	}
	m.openDialog(scope, host, nil)
}

func (m *manager) edit() {
	row, ok := m.current()
	if !ok || m.busy {
		return
	}
	host, err := m.host(row.Scope)
	if err != nil {
		walk.MsgBox(m.window, "MapFTPDrive", err.Error(), walk.MsgBoxIconError)
		return
	}
	initial := row.Status.Endpoint
	m.openDialog(row.Scope, host, &initial)
}

func (m *manager) openDialog(scope config.Scope, host Host, initial *config.Endpoint) {
	_, err := runMapperDialog(m.window, MapperOptions{
		Scope:   scope,
		Initial: initial,
		Usage:   m.usage(),
		Submit: func(result MapperResult, prompter Prompter) error {
			ctx, cancel := context.WithTimeout(context.Background(), SubmitTimeout)
			defer cancel()
			_, err := SubmitMap(ctx, host, m.options.Scanner, prompter, result, initial)
			return err
		},
	})
	if err != nil && !errors.Is(err, ErrCancelled) {
		walk.MsgBox(m.window, "MapFTPDrive", err.Error(), walk.MsgBoxIconError)
	}
	go m.reload()
}

func (m *manager) rename() {
	row, ok := m.current()
	if !ok || m.busy {
		return
	}
	name, err := runRenameDialog(m.window, row.Name)
	if err != nil {
		return
	}
	m.run("Renaming "+row.Drive+"…", func(ctx context.Context) (string, error) {
		host, err := m.host(row.Scope)
		if err != nil {
			return "", err
		}
		response, err := host.Call(ctx, control.Request{Operation: control.Rename, ID: row.Status.Endpoint.ID, Name: name})
		if err != nil {
			return "", err
		}
		return firstNonEmpty(response.Message, "Renamed "+row.Drive+" to "+name), nil
	})
}

func (m *manager) remove() {
	row, ok := m.current()
	if !ok || m.busy {
		return
	}
	if walk.MsgBox(m.window, "Remove FTP Drive", RemovalMessage(row.Name, row.Drive), walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
		return
	}
	m.run("Removing "+row.Drive+"…", func(ctx context.Context) (string, error) {
		host, err := m.host(row.Scope)
		if err != nil {
			return "", err
		}
		_, err = host.Call(ctx, control.Request{Operation: control.Remove, ID: row.Status.Endpoint.ID})
		return "Removed " + row.Drive, err
	})
}

func (m *manager) test() {
	row, ok := m.current()
	if !ok {
		return
	}
	m.run("Testing the connection to "+row.Status.Endpoint.Host+"…", func(ctx context.Context) (string, error) {
		host, err := m.host(row.Scope)
		if err != nil {
			return "", err
		}
		if _, err := host.Call(ctx, control.Request{Operation: control.Test, ID: row.Status.Endpoint.ID}); err != nil {
			return "", err
		}
		return "The connection to " + row.Status.Endpoint.Host + " works.", nil
	})
}

func (m *manager) repin() {
	row, ok := m.current()
	if !ok {
		return
	}
	m.run("Reading the server identity of "+row.Status.Endpoint.Host+"…", func(ctx context.Context) (string, error) {
		host, err := m.host(row.Scope)
		if err != nil {
			return "", err
		}
		message, err := Repin(ctx, host, m.options.Scanner, walkPrompter{owner: m.window}, row.Status.Endpoint)
		if err != nil {
			return "", err
		}
		return firstNonEmpty(message, "Pinned the new server identity of "+row.Drive), nil
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
