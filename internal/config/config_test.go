package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func testEndpoint(n int, drive string) Endpoint {
	id := testID(n)
	return Endpoint{
		ID:            id,
		Name:          fmt.Sprintf("Test %d", n),
		Protocol:      SFTP,
		Host:          "example.test",
		Port:          22,
		Username:      "tester",
		DriveLetter:   drive,
		Scope:         User,
		CredentialKey: CredentialKeyFor(id),
		KnownHost:     "example.test ssh-ed25519 AAAA",
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "endpoints.json"))
}

func TestStoreLifecycleAndRename(t *testing.T) {
	store := newTestStore(t)
	first := testEndpoint(1, "x")
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
	if byID, err := store.Find(first.ID); err != nil || byID.Name != first.Name {
		t.Fatalf("Find = %+v, %v", byID, err)
	}

	renamed, err := store.Rename(first.ID, "Archive")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Archive" {
		t.Fatalf("unexpected name %q", renamed.Name)
	}
	if _, err := store.Rename(first.ID, "bad,name"); err == nil {
		t.Fatal("rename accepted a comma")
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
	if _, err := store.Remove(first.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second remove = %v", err)
	}
	if _, err := store.Find(first.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("find removed = %v", err)
	}
}

func TestStoreRejectsDuplicateDriveAndName(t *testing.T) {
	store := newTestStore(t)
	if err := store.Upsert(testEndpoint(1, "X")); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(testEndpoint(2, "x:")); err == nil {
		t.Fatal("expected duplicate drive error")
	}
	sameName := testEndpoint(3, "Y")
	sameName.Name = "TEST 1"
	if err := store.Upsert(sameName); err == nil || !strings.Contains(err.Error(), "already named") {
		t.Fatalf("expected duplicate name error, got %v", err)
	}
}

func TestStoreReplaceTakesOverLetter(t *testing.T) {
	store := newTestStore(t)
	old := testEndpoint(1, "X")
	old.Disabled = true
	if err := store.Upsert(old); err != nil {
		t.Fatal(err)
	}
	replacement := testEndpoint(2, "X")
	if err := store.Replace(replacement, old.ID); err != nil {
		t.Fatal(err)
	}
	endpoints, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].ID != replacement.ID {
		t.Fatalf("after replace: %+v", endpoints)
	}
}

func TestEndpointValidation(t *testing.T) {
	cases := map[string]func(*Endpoint){
		"credential key":        func(e *Endpoint) { e.CredentialKey = "" },
		"foreign credential":    func(e *Endpoint) { e.CredentialKey = "endpoint/other" },
		"path traversal id":     func(e *Endpoint) { e.ID = `..\..\Windows\System32\x`; e.CredentialKey = CredentialKeyFor(e.ID) },
		"short id":              func(e *Endpoint) { e.ID = "one"; e.CredentialKey = CredentialKeyFor(e.ID) },
		"bad dash":              func(e *Endpoint) { e.ID = "00000000x0000-4000-8000-000000000001" },
		"protocol":              func(e *Endpoint) { e.Protocol = "webdav" },
		"tls on sftp":           func(e *Endpoint) { e.TLSMode = TLSExplicit },
		"tls mode":              func(e *Endpoint) { e.Protocol = FTPS; e.TLSMode = "opportunistic"; e.KnownHost = "" },
		"host":                  func(e *Endpoint) { e.Host = " " },
		"host with space":       func(e *Endpoint) { e.Host = "a b" },
		"port":                  func(e *Endpoint) { e.Port = 0 },
		"drive":                 func(e *Endpoint) { e.DriveLetter = "1" },
		"scope":                 func(e *Endpoint) { e.Scope = "global" },
		"username control":      func(e *Endpoint) { e.Username = "a\nb" },
		"key on ftp":            func(e *Endpoint) { e.Protocol = FTP; e.Auth = AuthKey; e.KeyFile = `C:\k`; e.KnownHost = "" },
		"key without file":      func(e *Endpoint) { e.Auth = AuthKey },
		"relative key file":     func(e *Endpoint) { e.Auth = AuthKey; e.KeyFile = `keys\id` },
		"key file on password":  func(e *Endpoint) { e.KeyFile = `C:\k` },
		"auth":                  func(e *Endpoint) { e.Auth = "kerberos" },
		"known host on ftp":     func(e *Endpoint) { e.Protocol = FTP },
		"pin on sftp":           func(e *Endpoint) { e.CertificatePin = strings.Repeat("a", 64) },
		"short pin":             func(e *Endpoint) { e.Protocol = FTPS; e.KnownHost = ""; e.CertificatePin = "abc" },
		"multi-line known host": func(e *Endpoint) { e.KnownHost = "a\nb" },
		"name comma":            func(e *Endpoint) { e.Name = "a,b" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			endpoint := testEndpoint(1, "X")
			mutate(&endpoint)
			if err := endpoint.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	valid := testEndpoint(1, "X")
	valid.ID = strings.ToUpper(valid.ID)
	if err := valid.Validate(); err != nil {
		t.Fatalf("uppercase UUID should normalize: %v", err)
	}
	keyed := testEndpoint(2, "Y")
	keyed.Auth = AuthKey
	keyed.KeyFile = `C:\Users\me\.ssh\id_ed25519`
	if err := keyed.Validate(); err != nil {
		t.Fatalf("key auth: %v", err)
	}
	keyed.KeyFile = `\\server\share\id`
	if err := keyed.Validate(); err != nil {
		t.Fatalf("UNC key path: %v", err)
	}
	pinned := testEndpoint(3, "Z")
	pinned.Protocol = FTPS
	pinned.TLSMode = TLSImplicit
	pinned.KnownHost = ""
	pinned.CertificatePin = strings.Repeat("AB", 32)
	if err := pinned.Validate(); err != nil {
		t.Fatalf("certificate pin: %v", err)
	}
	explicitPassword := testEndpoint(4, "W")
	explicitPassword.Auth = AuthPassword
	if normalized := explicitPassword.Normalized(); normalized.Auth != "" || normalized.AuthMethodOrDefault() != AuthPassword {
		t.Fatalf("password auth did not normalize: %+v", normalized)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"Archive", "Büro-Laufwerk", strings.Repeat("x", MaxNameLength), "a=b; c"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	invalid := []string{"", "  ", strings.Repeat("x", MaxNameLength+1), `a\b`, "a/b", "a,b", "tab\there", "nul\x00", strings.Repeat("😀", 17)}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
	if err := ValidateName(strings.Repeat("😀", 16)); err != nil {
		t.Errorf("16 surrogate pairs fit in 32 units: %v", err)
	}
}

func TestDefaultPort(t *testing.T) {
	cases := []struct {
		protocol Protocol
		mode     TLSMode
		want     uint16
	}{
		{FTP, "", 21},
		{FTPS, TLSExplicit, 21},
		{FTPS, TLSImplicit, 990},
		{SFTP, "", 22},
	}
	for _, c := range cases {
		if got := DefaultPort(c.protocol, c.mode); got != c.want {
			t.Errorf("DefaultPort(%s, %s) = %d, want %d", c.protocol, c.mode, got, c.want)
		}
	}
}

func TestNormalizeDrive(t *testing.T) {
	for input, want := range map[string]string{"x": "X", "x:": "X", `X:\`: "X", `X:"`: "X", `X:\.`: "X", ` y: `: "Y"} {
		if got := NormalizeDrive(input); got != want {
			t.Errorf("NormalizeDrive(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSameServer(t *testing.T) {
	a := testEndpoint(1, "X")
	b := testEndpoint(2, "Y")
	b.Host = "EXAMPLE.test"
	if !a.SameServer(b) {
		t.Fatal("same server not detected")
	}
	b.Username = "other"
	if a.SameServer(b) {
		t.Fatal("different account treated as same server")
	}
}

func TestStoreMigratesBareArray(t *testing.T) {
	store := newTestStore(t)
	legacy, err := json.Marshal([]Endpoint{testEndpoint(1, "X")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Migrated || len(snapshot.Endpoints) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, err := store.SetDisabled(testID(1), true); err != nil {
		t.Fatal(err)
	}
	var file storeFile
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("store was not rewritten as an envelope: %v", err)
	}
	if file.Version != StoreVersion || len(file.Endpoints) != 1 {
		t.Fatalf("envelope = %+v", file)
	}
	backup, err := os.ReadFile(store.BackupPath())
	if err != nil || string(backup) != string(legacy) {
		t.Fatalf("backup should hold the pre-migration file: %q, %v", backup, err)
	}
}

func TestStoreSkipsAndPreservesInvalidEntries(t *testing.T) {
	store := newTestStore(t)
	good := testEndpoint(1, "X")
	goodRaw, _ := json.Marshal(good)
	traversal := testEndpoint(2, "Y")
	traversal.ID = `..\..\evil`
	traversalRaw, _ := json.Marshal(traversal)
	duplicate := testEndpoint(3, "X")
	duplicateRaw, _ := json.Marshal(duplicate)
	content := fmt.Sprintf(`{"version":1,"endpoints":[%s,%s,%s,"not an object"]}`, goodRaw, traversalRaw, duplicateRaw)
	if err := os.WriteFile(store.Path(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Endpoints) != 1 || snapshot.Endpoints[0].ID != good.ID {
		t.Fatalf("endpoints = %+v", snapshot.Endpoints)
	}
	if len(snapshot.Problems) != 3 {
		t.Fatalf("problems = %+v", snapshot.Problems)
	}
	if err := store.Upsert(testEndpoint(4, "Z")); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Endpoints) != 2 || len(snapshot.Problems) != 3 {
		t.Fatalf("invalid entries were not preserved across a save: %+v", snapshot)
	}
}

func TestStoreRestoresFromBackup(t *testing.T) {
	store := newTestStore(t)
	if err := store.Upsert(testEndpoint(1, "X")); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(testEndpoint(2, "Y")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("{truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Restored || len(snapshot.Endpoints) != 1 || len(snapshot.Problems) != 1 {
		t.Fatalf("backup restore = %+v", snapshot)
	}
	goodBackup, err := os.ReadFile(store.BackupPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(testEndpoint(3, "Z")); err != nil {
		t.Fatal(err)
	}
	corrupt, err := os.ReadFile(store.Path() + ".corrupt")
	if err != nil || string(corrupt) != "{truncated" {
		t.Fatalf("corrupt file not preserved: %q, %v", corrupt, err)
	}
	backup, err := os.ReadFile(store.BackupPath())
	if err != nil || string(backup) != string(goodBackup) {
		t.Fatal("a corrupt main file must not replace the last good backup")
	}
	endpoints, err := store.Load()
	if err != nil || len(endpoints) != 2 {
		t.Fatalf("after repair: %+v, %v", endpoints, err)
	}
}

func TestStoreCorruptWithoutBackup(t *testing.T) {
	store := newTestStore(t)
	if err := os.WriteFile(store.Path(), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load()
	var corrupt *CorruptError
	if !errors.As(err, &corrupt) {
		t.Fatalf("expected CorruptError, got %v", err)
	}
	if err := store.Upsert(testEndpoint(1, "X")); err != nil {
		t.Fatalf("a corrupt store must not block new definitions: %v", err)
	}
	if data, err := os.ReadFile(store.Path() + ".corrupt"); err != nil || string(data) != "garbage" {
		t.Fatalf("corrupt bytes not kept: %q, %v", data, err)
	}
	if _, err := os.Stat(store.BackupPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt bytes must not become the backup: %v", err)
	}
	if err := store.Upsert(testEndpoint(2, "Y")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := store.Snapshot(); err != nil || len(snapshot.Endpoints) != 1 {
		t.Fatalf("empty main file should fall back to the backup: %+v, %v", snapshot, err)
	}
}

func TestStoreRefusesNewerVersion(t *testing.T) {
	store := newTestStore(t)
	if err := os.WriteFile(store.Path(), []byte(`{"version":99,"endpoints":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrNewerStore) {
		t.Fatalf("expected ErrNewerStore, got %v", err)
	}
	if err := store.Upsert(testEndpoint(1, "X")); !errors.Is(err, ErrNewerStore) {
		t.Fatalf("a newer store must not be overwritten: %v", err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"version":0,"endpoints":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("version 0 envelope accepted")
	}
}

func TestStoreSaveHookAndBackupRotation(t *testing.T) {
	store := newTestStore(t)
	var saved [][]Endpoint
	store.SetSaveHook(func(endpoints []Endpoint) { saved = append(saved, endpoints) })
	if err := store.Save([]Endpoint{testEndpoint(1, "X")}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save([]Endpoint{testEndpoint(1, "X"), testEndpoint(2, "Y")}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(store.BackupPath())
	if err != nil || string(backup) != string(first) {
		t.Fatal("backup is not the previous version")
	}
	if len(saved) != 2 || len(saved[1]) != 2 {
		t.Fatalf("hook calls = %+v", saved)
	}
	if err := store.Save([]Endpoint{testEndpoint(1, "X"), testEndpoint(2, "X")}); err == nil {
		t.Fatal("conflicting set saved")
	}
	if len(saved) != 2 {
		t.Fatal("hook ran for a failed save")
	}
	entries, err := os.ReadDir(filepath.Dir(store.Path()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestStoreUpdate(t *testing.T) {
	store := newTestStore(t)
	if err := store.Upsert(testEndpoint(1, "X")); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(testID(1), func(endpoint *Endpoint) error {
		endpoint.ID = testID(9)
		endpoint.Port = 2222
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != testID(1) || updated.Port != 2222 {
		t.Fatalf("update changed the id or lost the change: %+v", updated)
	}
	failure := errors.New("refused")
	if _, err := store.Update(testID(1), func(*Endpoint) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("update error = %v", err)
	}
	if _, err := store.Update(testID(1), func(endpoint *Endpoint) error { endpoint.Name = ""; return nil }); err == nil {
		t.Fatal("update saved an invalid endpoint")
	}
	if _, err := store.Update(testID(7), func(*Endpoint) error { return nil }); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing id = %v", err)
	}
}

func TestLayoutNamesNeverUseRawID(t *testing.T) {
	layout := LayoutFor(filepath.Join("base", "endpoints.json"))
	id := testID(1)
	for _, path := range append([]string{layout.KnownHostsFile(id), layout.KeyFile(id)}, layout.CacheDirs(id)...) {
		if strings.Contains(path, id) {
			t.Fatalf("%s contains the raw id", path)
		}
		if !strings.HasPrefix(path, "base") {
			t.Fatalf("%s escapes the scope directory", path)
		}
	}
	if EndpointKey(id) == EndpointKey(testID(2)) || len(EndpointKey(id)) != 16 {
		t.Fatal("endpoint keys are not distinct 16-character names")
	}
	if layout.LogDir() != filepath.Join("base", "logs") || layout.CacheRoot() != filepath.Join("base", "cache") {
		t.Fatal("unexpected layout")
	}
}

func TestIsAbsoluteWindowsPath(t *testing.T) {
	for path, want := range map[string]bool{`C:\k`: true, `c:/k`: true, `\\srv\share`: true, `\\`: false, `k`: false, `C:k`: false, `/k`: false} {
		if got := IsAbsoluteWindowsPath(path); got != want {
			t.Errorf("IsAbsoluteWindowsPath(%q) = %v", path, got)
		}
	}
}
