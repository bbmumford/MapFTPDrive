package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDriveIndexCarriesNoConnectionDetails(t *testing.T) {
	endpoints := []Endpoint{{ //nolint:gosec // G101: fixture values the index must not contain, not real credentials.
		ID:            "6f1c1c3e-8f5c-4c9a-9a51-0d4c1c3e8f5c",
		Name:          "Shared",
		Protocol:      SFTP,
		Host:          "files.example.test",
		Port:          22,
		Username:      "operator",
		DriveLetter:   "s:",
		RemotePath:    "/srv/private",
		Scope:         System,
		CredentialKey: "endpoint/6f1c1c3e-8f5c-4c9a-9a51-0d4c1c3e8f5c",
		Disabled:      true,
		KnownHost:     "files.example.test ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA",
	}}
	data, err := encodeDriveIndex(System, endpoints)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"files.example.test", "operator", "/srv/private", "endpoint/", "ssh-ed25519", "sftp"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("drive index exposes %q:\n%s", secret, data)
		}
	}
	drives, err := decodeDriveIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	want := DriveIndexEntry{Drive: "S", Scope: System, ID: endpoints[0].ID, Name: "Shared", Disabled: true}
	if len(drives) != 1 || drives[0] != want {
		t.Fatalf("decoded %+v, want %+v", drives, want)
	}
	endpoint := drives[0].Endpoint()
	if endpoint.DriveLetter != "S" || endpoint.Scope != System || !endpoint.Disabled || endpoint.Name != "Shared" {
		t.Fatalf("endpoint %+v", endpoint)
	}
}

func TestDriveIndexRejectsMalformedEntries(t *testing.T) {
	data := []byte(`{"version":1,"drives":[
		{"drive":"S","scope":"system","id":"` + testID(1) + `","name":"ok"},
		{"drive":"ST","scope":"system","id":"` + testID(2) + `","name":"two letters"},
		{"drive":"T","scope":"other","id":"` + testID(3) + `","name":"bad scope"},
		{"drive":"U","scope":"system","id":"","name":"no id"},
		{"drive":"V","scope":"system","id":"a","name":"not a UUID"}
	]}`)
	drives, err := decodeDriveIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(drives) != 1 || drives[0].ID != testID(1) {
		t.Fatalf("decoded %+v", drives)
	}
	if _, err := decodeDriveIndex([]byte(`{"version":2,"drives":[]}`)); err == nil {
		t.Fatal("accepted an unknown index version")
	}
	if _, err := decodeDriveIndex([]byte(`{"drives":[]}`)); err == nil {
		t.Fatal("accepted an index without a version")
	}
	if _, err := decodeDriveIndex([]byte(`[`)); err == nil {
		t.Fatal("accepted malformed JSON")
	}
}

func TestUserStoreDoesNotWriteDriveIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoints.json")
	store := NewStore(path)
	if err := store.Save([]Endpoint{testEndpoint(1, "Q")}); err != nil {
		t.Fatal(err)
	}
	if err := store.RefreshDriveIndex(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DriveIndexPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a user store wrote a drive index: %v", err)
	}
}

func TestMaintainedIndexFollowsEverySave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoints.json")
	store := NewStore(path)
	store.MaintainDriveIndex()
	index := DriveIndexPath(path)
	if err := store.RefreshDriveIndex(); err != nil {
		t.Fatal(err)
	}
	if entries, err := ReadDriveIndex(index); err != nil || len(entries) != 0 {
		t.Fatalf("index of an empty store = %+v, %v", entries, err)
	}
	if err := store.Upsert(testEndpoint(1, "Q")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rename(testID(1), "Renamed"); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadDriveIndex(index)
	if err != nil || len(entries) != 1 || entries[0].Name != "Renamed" || entries[0].Scope != System {
		t.Fatalf("index after rename = %+v, %v", entries, err)
	}
	if _, err := store.Remove(testID(1)); err != nil {
		t.Fatal(err)
	}
	if entries, err := ReadDriveIndex(index); err != nil || len(entries) != 0 {
		t.Fatalf("index after remove = %+v, %v", entries, err)
	}
}

func TestDriveIndexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := DriveIndexPath(filepath.Join(dir, "endpoints.json"))
	if entries, err := ReadDriveIndex(path); err != nil || entries != nil {
		t.Fatalf("missing index = %v, %v", entries, err)
	}
	disabled := testEndpoint(2, "B")
	disabled.Disabled = true
	if err := WriteDriveIndex(path, System, []Endpoint{testEndpoint(1, "Q"), disabled}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own temporary file.
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"credentialKey", "knownHost", "host"} {
		if strings.Contains(string(data), `"`+secret+`"`) {
			t.Fatalf("index leaks %s: %s", secret, data)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("index mode = %v, %v", info.Mode(), err)
		}
	}
	entries, err := ReadDriveIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Drive != "B" || !entries[0].Disabled || entries[1].Scope != System {
		t.Fatalf("entries = %+v", entries)
	}
	if entry, ok := FindDriveIndexEntry(entries, `q:\`); !ok || entry.ID != testID(1) {
		t.Fatalf("lookup = %+v, %v", entry, ok)
	}
	if _, ok := FindDriveIndexEntry(entries, "Z"); ok {
		t.Fatal("unexpected match")
	}

	if err := WriteDriveIndex(path, System, []Endpoint{testEndpoint(3, "Q")}); err != nil {
		t.Fatalf("an existing index must be replaceable: %v", err)
	}

	// The written index is read-only for Users on Windows, so hand-made
	// variants go to separate files.
	write := func(name, content string) string {
		t.Helper()
		variant := filepath.Join(dir, name)
		if err := os.WriteFile(variant, []byte(content), 0o644); err != nil { //nolint:gosec // G306: mirrors the index's readable mode.
			t.Fatal(err)
		}
		return variant
	}
	tampered := `{"version":1,"drives":[{"drive":"C","scope":"system","id":"../x","name":"x"},{"drive":"1","scope":"system","id":"` + testID(1) + `"},{"drive":"D","scope":"root","id":"` + testID(1) + `"},{"drive":"E:","scope":"user","id":"` + testID(3) + `","name":"ok"}]}`
	entries, err = ReadDriveIndex(write("tampered.json", tampered))
	if err != nil || len(entries) != 1 || entries[0].Drive != "E" {
		t.Fatalf("tampered index = %+v, %v", entries, err)
	}
	if _, err := ReadDriveIndex(write("newer.json", `{"version":7}`)); err == nil {
		t.Fatal("newer index accepted")
	}
	if _, err := ReadDriveIndex(write("corrupt.json", `[`)); err == nil {
		t.Fatal("corrupt index accepted")
	}
}
