package main

import (
	"flag"
	"fmt"
	"os"

	"mapftpdrive/internal/buildinfo"
	"mapftpdrive/internal/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "MapFTPDrive:", err)
		os.Exit(1)
	}
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
