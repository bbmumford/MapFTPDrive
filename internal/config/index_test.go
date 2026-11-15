package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	data, err := os.ReadFile(path)
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
		if err := os.WriteFile(variant, []byte(content), 0o644); err != nil {
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
