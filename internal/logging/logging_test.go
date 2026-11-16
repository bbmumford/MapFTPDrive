package logging

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRotatingKeepsBoundedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	rotating, err := OpenRotating(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 20; index++ {
		if _, err := fmt.Fprintf(rotating, "line %02d %s\n", index, strings.Repeat("x", 30)); err != nil {
			t.Fatal(err)
		}
	}
	if err := rotating.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rotating.Close(); err != nil {
		t.Fatal("second close failed")
	}
	if _, err := rotating.Write([]byte("late")); err == nil {
		t.Fatal("write after close succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("files = %v", entries)
	}
	current, _ := os.ReadFile(path)
	newest, _ := os.ReadFile(path + ".1")
	if !strings.Contains(string(current), "line 19") || len(current) > 100 || len(newest) > 100 {
		t.Fatalf("current %q newest %q", current, newest)
	}

	reopened, err := OpenRotating(path, 1000, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.size != int64(len(current)) {
		t.Fatal("reopened file does not append")
	}
}

func TestOpenWritesFileAndReportsMissingEventLog(t *testing.T) {
	dir := t.TempDir()
	logger, err := Open(Options{Dir: dir, EventLog: runtime.GOOS != "windows"})
	if runtime.GOOS != "windows" && err == nil {
		t.Fatal("event log reported available off Windows")
	}
	logger.Info("mounted", "drive", "R")
	logger.Debug("hidden")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mounted drive=R") || strings.Contains(string(data), "hidden") {
		t.Fatalf("log = %q", data)
	}
}

func TestOpenSurvivesUnwritableDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logger, err := Open(Options{Dir: filepath.Join(blocker, "logs")})
	if err == nil {
		t.Fatal("expected an error for an impossible directory")
	}
	logger.Error("still usable")
	Discard().Warn("ignored")
}

func TestFanoutRespectsLevels(t *testing.T) {
	var debug, warn bytes.Buffer
	handler := Fanout(
		slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}),
		slog.NewTextHandler(&warn, &slog.HandlerOptions{Level: slog.LevelWarn}),
	)
	logger := slog.New(handler).With("scope", "user").WithGroup("mount")
	logger.Debug("detail", "id", 1)
	logger.Warn("problem", "id", 2)
	if !strings.Contains(debug.String(), "detail") || !strings.Contains(debug.String(), "problem") {
		t.Fatalf("debug sink = %q", debug.String())
	}
	if strings.Contains(warn.String(), "detail") || !strings.Contains(warn.String(), "scope=user mount.id=2") {
		t.Fatalf("warn sink = %q", warn.String())
	}
	if Fanout().Enabled(context.Background(), slog.LevelError) {
		t.Fatal("empty fanout enabled")
	}
}
