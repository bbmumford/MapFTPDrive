//go:build windows

package main

import (
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestElevatedCommandLineRoundTrips(t *testing.T) {
	cases := [][]string{
		{"rename", "--drive", "X", "--name", `Work "Shared"`},
		{"rename", "--drive", "X", "--name", `ends with backslash\`},
		{"rename", "--drive", "X", "--name", `two\\ "quoted\" parts`},
		{"map", "--scope", "system"},
		{"rename", "--drive", "X", "--name", ""},
	}
	for _, arguments := range cases {
		// DecomposeCommandLine uses the shell's parser, which treats the
		// first token as the program name, so a placeholder precedes the
		// arguments exactly as the elevated process sees them.
		parsed, err := windows.DecomposeCommandLine(`MapFTPDrive.exe ` + elevatedCommandLine(arguments))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(parsed[1:], arguments) {
			t.Errorf("round trip of %q gave %q", arguments, parsed[1:])
		}
	}
}
