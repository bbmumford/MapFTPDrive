//go:build windows

package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
)

// Event IDs are fixed per level; the message text carries the detail.
const (
	eventInfo    = 1
	eventWarning = 2
	eventError   = 3
)

// InstallEventSource registers EventSource with EventCreate.exe as its
// message file so the service's messages render in Event Viewer. It needs
// administrative rights; an already registered source is not an error.
func InstallEventSource() error {
	err := eventlog.InstallAsEventCreate(EventSource, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

// RemoveEventSource unregisters EventSource; uninstall calls it.
func RemoveEventSource() error {
	return eventlog.Remove(EventSource)
}

// openEventLog registers the source when missing (the service runs as
// LocalSystem and may) and opens it for warnings and errors.
func openEventLog(source string) (slog.Handler, io.Closer, error) {
	_ = InstallEventSource()
	log, err := eventlog.Open(source)
	if err != nil {
		return nil, nil, err
	}
	return &eventHandler{log: log}, log, nil
}

type eventHandler struct {
	log   *eventlog.Log
	attrs []slog.Attr
	group string
}

func (h *eventHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h *eventHandler) Handle(_ context.Context, record slog.Record) error {
	var text strings.Builder
	text.WriteString(record.Message)
	write := func(attr slog.Attr) bool {
		text.WriteString(" ")
		if h.group != "" {
			text.WriteString(h.group + ".")
		}
		text.WriteString(attr.Key + "=" + attr.Value.String())
		return true
	}
	for _, attr := range h.attrs {
		write(attr)
	}
	record.Attrs(write)
	switch {
	case record.Level >= slog.LevelError:
		return h.log.Error(eventError, text.String())
	case record.Level >= slog.LevelWarn:
		return h.log.Warning(eventWarning, text.String())
	default:
		return h.log.Info(eventInfo, text.String())
	}
}

func (h *eventHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &eventHandler{log: h.log, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...), group: h.group}
}

func (h *eventHandler) WithGroup(name string) slog.Handler {
	group := name
	if h.group != "" {
		group = h.group + "." + name
	}
	return &eventHandler{log: h.log, attrs: h.attrs, group: group}
}
