// Package logging gives each MapFTPDrive process a persistent log: a
// size-rotated file in the scope's log directory (user processes:
// %LOCALAPPDATA%\MapFTPDrive\logs; the service: the protected
// %ProgramData%\MapFTPDrive\logs) and, for the service, the Windows Event
// Log for warnings and errors. The executable is a GUI-subsystem program
// and the service has no console, so without this every error that happens
// before a window appears would be lost.
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	// FileName is the active log file in the log directory.
	FileName = "mapftpdrive.log"
	// DefaultMaxSize rotates the file at 4 MiB.
	DefaultMaxSize = 4 << 20
	// DefaultKeep is the number of rotated files kept (.1 is the newest).
	DefaultKeep = 3
	// EventSource is the Event Log source the service writes to.
	EventSource = "MapFTPDrive"
)

type Options struct {
	Dir     string
	MaxSize int64
	Keep    int
	// EventLog also sends warnings and errors to the Windows Event Log under
	// EventSource; it is set for the service only.
	EventLog bool
	// Level is the lowest level written to the file.
	Level slog.Level
}

// Logger owns the log sinks of one process.
type Logger struct {
	*slog.Logger
	handler slog.Handler
	closers []io.Closer
}

// Handler is the combined handler, for routing other loggers (rclone) here.
func (l *Logger) Handler() slog.Handler { return l.handler }

func (l *Logger) Close() error {
	var errs []error
	for _, closer := range l.closers {
		errs = append(errs, closer.Close())
	}
	return errors.Join(errs...)
}

// Open creates the log directory and sinks. A sink that cannot be opened is
// skipped and reported in the returned error while the remaining sinks still
// work, so logging problems never stop a host from mounting.
func Open(options Options) (*Logger, error) {
	if options.MaxSize <= 0 {
		options.MaxSize = DefaultMaxSize
	}
	if options.Keep <= 0 {
		options.Keep = DefaultKeep
	}
	logger := &Logger{}
	var handlers []slog.Handler
	var errs []error
	if options.Dir != "" {
		file, err := OpenRotating(filepath.Join(options.Dir, FileName), options.MaxSize, options.Keep)
		if err != nil {
			errs = append(errs, err)
		} else {
			logger.closers = append(logger.closers, file)
			handlers = append(handlers, slog.NewTextHandler(file, &slog.HandlerOptions{Level: options.Level}))
		}
	}
	if options.EventLog {
		handler, closer, err := openEventLog(EventSource)
		if err != nil {
			errs = append(errs, err)
		} else {
			logger.closers = append(logger.closers, closer)
			handlers = append(handlers, handler)
		}
	}
	logger.handler = Fanout(handlers...)
	logger.Logger = slog.New(logger.handler)
	return logger, errors.Join(errs...)
}

// Discard is a logger that writes nowhere, for tests and fallbacks.
func Discard() *Logger {
	handler := Fanout()
	return &Logger{Logger: slog.New(handler), handler: handler}
}

// Fanout sends each record to every handler that accepts its level.
func Fanout(handlers ...slog.Handler) slog.Handler {
	return fanout(handlers)
}

type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range f {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range f {
		if handler.Enabled(ctx, record.Level) {
			errs = append(errs, handler.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(fanout, len(f))
	for index, handler := range f {
		next[index] = handler.WithAttrs(attrs)
	}
	return next
}

func (f fanout) WithGroup(name string) slog.Handler {
	next := make(fanout, len(f))
	for index, handler := range f {
		next[index] = handler.WithGroup(name)
	}
	return next
}

// Rotating is an append-only file that is renamed to .1 (shifting older
// files up to .keep) once it would exceed maxSize.
type Rotating struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	keep    int
	file    *os.File
	size    int64
}

func OpenRotating(path string, maxSize int64, keep int) (*Rotating, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	rotating := &Rotating{path: path, maxSize: maxSize, keep: keep}
	if err := rotating.open(); err != nil {
		return nil, err
	}
	return rotating, nil
}

func (r *Rotating) open() error {
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	r.file, r.size = file, info.Size()
	return nil
}

func (r *Rotating) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(data)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := r.file.Write(data)
	r.size += int64(written)
	return written, err
}

// rotate shifts path.(n-1) to path.n down to path to path.1; the rename
// onto path.keep replaces the oldest file.
func (r *Rotating) rotate() error {
	if err := r.file.Close(); err != nil {
		return err
	}
	r.file = nil
	for index := r.keep - 1; index >= 1; index-- {
		from := fmt.Sprintf("%s.%d", r.path, index)
		if _, err := os.Stat(from); err == nil {
			if err := os.Rename(from, fmt.Sprintf("%s.%d", r.path, index+1)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		return err
	}
	return r.open()
}

func (r *Rotating) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
