package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"mapftpdrive/internal/buildinfo"
	"mapftpdrive/internal/cleanup"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/explorer"
	"mapftpdrive/internal/logging"
	"mapftpdrive/internal/service"
)

func main() {
	args := os.Args[1:]
	logger := commandLogger(args)
	if err := run(args); err != nil {
		logger.Error("command failed", "command", commandName(args), "error", err)
		reportError(err, interactive(args))
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
// entry, which must never block on a message box. The executable uses the
// GUI subsystem, so an interactive command has no console and reportError
// also shows its failure in a message box; the others fail with an exit
// code only.
func interactive(args []string) bool {
	switch commandName(args) {
	case "install", "prepare-system-data", "configure-service", "uninstall", "uninstall-cleanup",
		"purge-user", "service", "daemon", "version":
		return false
	}
	return true
}

// commandLogger opens the user's log for every command except the hosts,
// which open their own scope's log in host.Run, and the uninstall commands,
// which delete the user's data directory that holds the log and cannot
// while this process keeps a log file in it open.
func commandLogger(args []string) *logging.Logger {
	switch commandName(args) {
	case "service", "daemon", "uninstall", "uninstall-cleanup", "purge-user":
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
		return installMachine()
	case "prepare-system-data":
		return prepareSystemData()
	case "configure-service":
		return service.Configure()
	case "uninstall", "uninstall-cleanup":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		keepData := flags.Bool("keep-data", false, "keep drive definitions, passwords and logs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if args[0] == "uninstall" {
			return uninstallMachine(*keepData)
		}
		return uninstallCleanup(*keepData)
	case "purge-user":
		return cleanup.PurgeCurrentUser()
	case "service":
		flags := flag.NewFlagSet("service", flag.ContinueOnError)
		runCleanup := flags.Bool("cleanup", false, "run the uninstall cleanup and stop")
		keepData := flags.Bool("keep-data", false, "with --cleanup, only stop the user hosts")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *runCleanup {
			return service.RunTask(func() error { return runUninstallCleanup(*keepData) })
		}
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
			if args[0] == "mount" {
				return mountUnmountedDrive()
			}
			return fmt.Errorf("--drive is required")
		}
		letter, err := explorer.NormalizeDrive(*drive)
		if err != nil {
			return err
		}
		return runDriveAction(args[0], letter, *name)
	case "version":
		fmt.Printf("MapFTPDrive %s (%s, %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
