package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testEndpoint(id, drive string) Endpoint {
	return Endpoint{
		ID:            id,
		Name:          "Test " + id,
		Protocol:      SFTP,
		Host:          "example.test",
		Port:          22,
		Username:      "tester",
		DriveLetter:   drive,
		Scope:         User,
		CredentialKey: "endpoint/" + id,
	}
}

func TestStoreLifecycleAndRename(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "endpoints.json"))
	first := testEndpoint("one", "x")
	if err := store.Upsert(first); err != nil {
		t.Fatal(err)
	}

	found, err := store.FindByDrive(`X:\`)
	if err != nil {
		t.Fatal(err)
	}
	if found.DriveLetter != "X" {
		t.Fatalf("drive was not normalized: %q", found.DriveLetter)
	}

	renamed, err := store.Rename(first.ID, "Archive")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Archive" {
		t.Fatalf("unexpected name %q", renamed.Name)
	}

	disabled, err := store.SetDisabled(first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !disabled.Disabled {
		t.Fatal("endpoint was not disabled")
	}

	removed, err := store.Remove(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != first.ID {
		t.Fatalf("removed %q, want %q", removed.ID, first.ID)
	}
	if _, err := store.FindByDrive("X:"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestStoreRejectsDuplicateDrive(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "endpoints.json"))
	if err := store.Upsert(testEndpoint("one", "X")); err != nil {
		t.Fatal(err)
	}
	err := store.Upsert(testEndpoint("two", "x:"))
	if err == nil {
		t.Fatal("expected duplicate drive error")
	}
}

func TestEndpointValidation(t *testing.T) {
	endpoint := testEndpoint("one", "X")
	endpoint.CredentialKey = ""
	if err := endpoint.Validate(); err == nil {
		t.Fatal("expected missing credential key error")
	}
}
