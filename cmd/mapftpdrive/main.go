package main

import (
	"flag"
	"fmt"
	"os"

	"mapftpdrive/internal/buildinfo"
	"mapftpdrive/internal/cleanup"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/explorer"
	"mapftpdrive/internal/service"
)

func main() {
	args := os.Args[1:]
	if err := run(args); err != nil {
		reportError(err, interactiveCommand(args))
		os.Exit(1)
	}
}

// interactiveCommand reports whether args name a command that Explorer or
// the Start menu starts. The executable uses the GUI subsystem, so those
// invocations have no console and reportError must show a message box;
// installer and service commands keep failing silently with an exit code.
func interactiveCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "map", "mount", "unmount", "remove", "rename":
		return true
	}
	return false
}

func run(args []string) error {
	if len(args) == 0 {
		return launchUI(config.User)
	}
	switch args[0] {
	case "map":
		flags := flag.NewFlagSet("map", flag.ContinueOnError)
		scope := flags.String("scope", string(config.User), "user or system")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return launchUI(config.Scope(*scope))
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
