package main

import (
	"testing"

	"mapftpdrive/internal/config"
)

func TestInteractiveCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"map"}, {"manage"}, {"mount", "--drive", "R"}, {"rename"}, {"bogus"}} {
		if !interactive(args) {
			t.Errorf("%v should report errors in a message box", args)
		}
	}
	for _, command := range []string{"install", "prepare-system-data", "configure-service", "uninstall", "uninstall-cleanup", "purge-user", "service", "daemon", "version"} {
		if interactive([]string{command}) {
			t.Errorf("%s must never block on a message box", command)
		}
	}
	if commandName(nil) != "manage" || commandName([]string{"map"}) != "map" {
		t.Fatal("command names")
	}
}

func TestParseScope(t *testing.T) {
	if scope, err := parseScope("system"); err != nil || scope != config.System {
		t.Fatalf("system = %v, %v", scope, err)
	}
	if _, err := parseScope("global"); err == nil {
		t.Fatal("unknown scope accepted")
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"unknown command": {"format"},
		"missing drive":   {"unmount"},
		"bad scope":       {"map", "--scope", "everyone"},
		"bad flag":        {"map", "--nope"},
		"removed command": {"install-dependencies"},
	}
	for name, args := range cases {
		if err := run(args); err == nil {
			t.Errorf("%s: %v accepted", name, args)
		}
	}
	if err := run([]string{"version"}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"service", "purge-user"} {
		if logger := commandLogger([]string{command}); logger == nil {
			t.Fatalf("no logger for the %s command", command)
		}
	}
}
