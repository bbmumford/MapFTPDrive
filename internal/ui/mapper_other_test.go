//go:build !windows

package ui

import (
	"testing"

	"mapftpdrive/internal/config"
)

// On Windows these entry points open real windows, so they are only
// exercised where the stubs apply.
func TestNativeInterfaceStubs(t *testing.T) {
	if _, err := ShowMapper(config.User); err == nil {
		t.Fatal("mapper reported success without a window")
	}
	if _, err := ShowMapperWith(MapperOptions{}); err == nil {
		t.Fatal("mapper reported success without a window")
	}
	if ShowManager(ManagerOptions{}) == nil {
		t.Fatal("manager reported success without a window")
	}
	if _, err := PromptRename("x"); err == nil || ConfirmRemove("x", "R") || ConfirmHostKey("h", "a", "f") {
		t.Fatal("prompts answered without a window")
	}
}
