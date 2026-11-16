//go:build !windows

package main

import (
	"strings"
	"testing"
)

// On Windows these commands open the map dialog or the management window,
// so they are only exercised where they report the platform instead.
func TestWindowCommandsOffWindows(t *testing.T) {
	for _, args := range [][]string{{"map"}, {"manage"}, nil} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "Windows") {
			t.Fatalf("%v off Windows = %v", args, err)
		}
	}
}
