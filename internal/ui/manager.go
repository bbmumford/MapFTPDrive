package ui

import (
	"context"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
)

// ManagerOptions configure the management window.
type ManagerOptions struct {
	// Hosts are the scopes this process may manage: the user's, plus the
	// system scope when elevated.
	Hosts map[config.Scope]Host
	// OpenSystem relaunches the window elevated to manage system-wide
	// drives; nil when already elevated.
	OpenSystem func() error
	Scanner    Scanner
	// Usage reports drive letters in use for the map and edit dialogs.
	Usage func() LetterUsage
}

// ShowManager runs the management window until it is closed.
func ShowManager(options ManagerOptions) error {
	return showManager(options)
}

// fetchReports asks every host for status; a host that cannot be reached
// contributes a report carrying the error so the window can say so.
func fetchReports(ctx context.Context, hosts map[config.Scope]Host) []*control.StatusReport {
	var reports []*control.StatusReport
	for _, scope := range []config.Scope{config.User, config.System} {
		host, ok := hosts[scope]
		if !ok {
			continue
		}
		response, err := host.Call(ctx, control.Request{Operation: control.Status})
		switch {
		case err != nil:
			reports = append(reports, &control.StatusReport{Scope: scope, StoreError: "host unavailable: " + err.Error()})
		case response.Report != nil:
			response.Report.Scope = scope
			reports = append(reports, response.Report)
		}
	}
	return reports
}

// UsageFromReports adds the drives listed by status reports to a letter
// mask, for the dialogs opened from the management window.
func UsageFromReports(mask uint32, reports []*control.StatusReport) LetterUsage {
	usage := LetterUsage{Mask: mask}
	for _, report := range reports {
		if report == nil {
			continue
		}
		for _, status := range report.Endpoints {
			usage.Managed = append(usage.Managed, config.DriveIndexEntry{
				Drive:    status.Endpoint.DriveLetter,
				Scope:    report.Scope,
				ID:       status.Endpoint.ID,
				Name:     status.Endpoint.Name,
				Disabled: status.State == control.StateDisabled,
			})
		}
	}
	return usage
}
