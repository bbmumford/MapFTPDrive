package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"mapftpdrive/internal/buildinfo"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/logging"
	"mapftpdrive/internal/ui"
)

func main() {
	args := os.Args[1:]
	logger := commandLogger(args)
	if err := run(args); err != nil {
		logger.Error("command failed", "command", commandName(args), "error", err)
		// The executable is a GUI-subsystem program with no console, so
		// interactive commands also report failures in a message box unless
		// the command already showed this error itself.
		if interactive(args) && !ui.WasShown(err) {
			ui.ShowError("MapFTPDrive", err)
		}
		fmt.Fprintln(os.Stderr, "MapFTPDrive:", err)
		os.Exit(1)
	}
}

func commandName(args []string) string {
	if len(args) == 0 {
		return "manage"
	}
	return args[0]
}

// interactive reports commands started by a person (Start menu, Explorer
// verbs) as opposed to installers, the service manager or the sign-in Run
// entry, which must never block on a message box.
func interactive(args []string) bool {
	switch commandName(args) {
	case "install", "uninstall", "install-dependencies", "service", "daemon", "version":
		return false
	}
	return true
}

// commandLogger opens the user's log for every command except the hosts,
// which open their own scope's log in host.Run.
func commandLogger(args []string) *logging.Logger {
	switch commandName(args) {
	case "service", "daemon":
		return logging.Discard()
	}
	path, err := config.DefaultPath(config.User)
	if err != nil {
		return logging.Discard()
	}
	logger, _ := logging.Open(logging.Options{Dir: config.LayoutFor(path).LogDir()})
	slog.SetDefault(logger.Logger)
	return logger
}

func parseScope(value string) (config.Scope, error) {
	scope := config.Scope(value)
	if scope != config.User && scope != config.System {
		return "", fmt.Errorf("--scope must be %q or %q", config.User, config.System)
	}
	return scope, nil
}

func run(args []string) error {
	if len(args) == 0 {
		return runManager()
	}
	switch args[0] {
	case "map":
		flags := flag.NewFlagSet("map", flag.ContinueOnError)
		scope := flags.String("scope", string(config.User), "user or system")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		parsed, err := parseScope(*scope)
		if err != nil {
			return err
		}
		return runMapper(parsed)
	case "manage":
		return runManager()
	case "install":
		return installExplorerIntegration()
	case "install-dependencies":
		flags := flag.NewFlagSet("install-dependencies", flag.ContinueOnError)
		source := flags.String("winfsp", "", "path to the bundled WinFsp MSI")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *source == "" {
			return fmt.Errorf("--winfsp is required")
		}
		return installDependencies(*source)
	case "uninstall":
		return uninstallExplorerIntegration()
	case "service":
		return runService()
	case "daemon":
		flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
		scope := flags.String("scope", string(config.User), "user or system")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return runDaemon(config.Scope(*scope))
	case "mount", "unmount", "remove", "rename":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		drive := flags.String("drive", "", "managed drive letter")
		name := flags.String("name", "", "new drive name")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *drive == "" {
			return fmt.Errorf("--drive is required")
		}
		return runDriveAction(args[0], *drive, *name)
	case "version":
		fmt.Printf("MapFTPDrive %s (%s, %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
