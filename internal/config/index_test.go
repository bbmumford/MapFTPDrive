package config

import (
	"strings"
	"testing"
)

func TestDriveIndexCarriesNoConnectionDetails(t *testing.T) {
	endpoints := []Endpoint{{
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
	data, err := encodeDriveIndex(endpoints)
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
	want := IndexedDrive{Drive: "S", Scope: System, ID: endpoints[0].ID, Name: "Shared", Disabled: true}
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
		{"drive":"S","scope":"system","id":"a","name":"ok"},
		{"drive":"s","scope":"system","id":"b","name":"lower"},
		{"drive":"ST","scope":"system","id":"c","name":"two letters"},
		{"drive":"T","scope":"other","id":"d","name":"bad scope"},
		{"drive":"U","scope":"system","id":"","name":"no id"}
	]}`)
	drives, err := decodeDriveIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(drives) != 1 || drives[0].ID != "a" {
		t.Fatalf("decoded %+v", drives)
	}
	if _, err := decodeDriveIndex([]byte(`{"version":2,"drives":[]}`)); err == nil {
		t.Fatal("accepted an unknown index version")
	}
	if _, err := decodeDriveIndex([]byte(`[`)); err == nil {
		t.Fatal("accepted malformed JSON")
	}
}

func TestUserStoreDoesNotWriteDriveIndex(t *testing.T) {
	store := NewStore(t.TempDir() + "/endpoints.json")
	if err := store.RefreshDriveIndex(); err != nil {
		t.Fatal(err)
	}
}
