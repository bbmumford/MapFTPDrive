package explorer

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mapftpdrive/internal/mount"
)

// installedExecutable is where both installers place the program; the
// space in it exercises the quoting of argv[0].
const installedExecutable = `C:\Program Files\MapFTPDrive\MapFTPDrive.exe`

// commandLineToArgv splits a command line the way a Go program on Windows
// builds os.Args (os/exec_windows.go). It is reproduced here so the
// registry command strings are checked on every CI host, not only Windows.
func commandLineToArgv(cmd string) []string {
	var args []string
	for len(cmd) > 0 {
		if cmd[0] == ' ' || cmd[0] == '\t' {
			cmd = cmd[1:]
			continue
		}
		var arg []byte
		arg, cmd = readNextArg(cmd)
		args = append(args, string(arg))
	}
	return args
}

func appendBSBytes(b []byte, n int) []byte {
	for ; n > 0; n-- {
		b = append(b, '\\')
	}
	return b
}

func readNextArg(cmd string) (arg []byte, rest string) {
	var b []byte
	var inquote bool
	var nslash int
	for ; len(cmd) > 0; cmd = cmd[1:] {
		c := cmd[0]
		switch c {
		case ' ', '\t':
			if !inquote {
				return appendBSBytes(b, nslash), cmd[1:]
			}
		case '"':
			b = appendBSBytes(b, nslash/2)
			if nslash%2 == 0 {
				if inquote && len(cmd) > 1 && cmd[1] == '"' {
					b = append(b, c)
					cmd = cmd[1:]
				}
				inquote = !inquote
			} else {
				b = append(b, c)
			}
			nslash = 0
			continue
		case '\\':
			nslash++
			continue
		}
		b = appendBSBytes(b, nslash)
		nslash = 0
		b = append(b, c)
	}
	return appendBSBytes(b, nslash), ""
}

// expandDrive performs Explorer's %V substitution for a drive root.
func expandDrive(command, root string) string {
	return strings.ReplaceAll(command, "%V", root)
}

func TestDriveVerbCommandsYieldTheSelectedDrive(t *testing.T) {
	for _, verb := range Verbs {
		if !strings.Contains(verb.Args, "%V") {
			continue
		}
		action := strings.Fields(verb.Args)[0]
		for _, root := range []string{`X:\`, `x:\`, `X:`} {
			argv := commandLineToArgv(expandDrive(verb.CommandLine(installedExecutable), root))
			if len(argv) != 4 || argv[0] != installedExecutable || argv[1] != action || argv[2] != "--drive" {
				t.Fatalf("%s with %%V=%s parsed as %q", verb.Key, root, argv)
			}
			letter, err := NormalizeDrive(argv[3])
			if err != nil || letter != "X" {
				t.Fatalf("%s with %%V=%s: drive argument %q normalised to %q, %v", verb.Key, root, argv[3], letter, err)
			}
		}
	}
}

func TestQuotedRootRegistrationResolves(t *testing.T) {
	// A verb registered as --drive "%V" hands the program X:" because the
	// root's trailing backslash escapes the closing quote.
	argv := commandLineToArgv(expandDrive(`"`+installedExecutable+`" unmount --drive "%V"`, `X:\`))
	if want := []string{installedExecutable, "unmount", "--drive", `X:"`}; !reflect.DeepEqual(argv, want) {
		t.Fatalf("parsed %q, want %q", argv, want)
	}
	if letter, err := NormalizeDrive(argv[3]); err != nil || letter != "X" {
		t.Fatalf("normalised to %q, %v", letter, err)
	}
}

func TestNormalizeDrive(t *testing.T) {
	valid := map[string]string{
		"X": "X", "x": "X", "X:": "X", `X:\`: "X", `X:\.`: "X", `X:"`: "X",
		`X:\"`: "X", ` z:\ `: "Z", `"Q:"`: "Q", "X:/": "X",
	}
	for input, want := range valid {
		got, err := NormalizeDrive(input)
		if err != nil || got != want {
			t.Errorf("NormalizeDrive(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", ".", `\`, "XY", "X::", `C:\Windows`, `\\server\share`, "1:", "é", `..\X`} {
		if got, err := NormalizeDrive(input); err == nil {
			t.Errorf("NormalizeDrive(%q) = %q, want an error", input, got)
		}
	}
}

func TestDriveAppliesToMatchesMountedFileSystem(t *testing.T) {
	want := `System.Volume.FileSystem:="` + mount.VolumeFileSystem + `"`
	if DriveAppliesTo != want {
		t.Fatalf("DriveAppliesTo = %s, want %s", DriveAppliesTo, want)
	}
	for _, verb := range Verbs {
		if strings.HasPrefix(verb.Key, DriveShell+`\`) && verb.AppliesTo != DriveAppliesTo {
			t.Errorf("drive verb %s has AppliesTo %q", verb.Key, verb.AppliesTo)
		}
	}
}

// registryKey is the set of values an installer writes to one key.
type registryKey map[string]string

// verbKeys returns the registered values below Software\Classes for every
// key that belongs to a MapFTPDrive verb, keyed by the path relative to
// Software\Classes.
func verbKeys(values map[string]registryKey) map[string]registryKey {
	result := make(map[string]registryKey)
	for key, entries := range values {
		relative, ok := strings.CutPrefix(key, `Software\Classes\`)
		if !ok || !strings.Contains(relative, `\shell\MapFTPDrive`) {
			continue
		}
		result[relative] = entries
	}
	return result
}

// checkInstallerVerbs compares an installer's verb registrations with the
// Verbs table. executable is the installer's spelling of the program path.
func checkInstallerVerbs(t *testing.T, installer string, values map[string]registryKey, executable, resolved string) {
	t.Helper()
	keys := verbKeys(values)
	seen := make(map[string]bool)
	for _, verb := range Verbs {
		key, ok := keys[verb.Key]
		if !ok {
			t.Errorf("%s does not register %s", installer, verb.Key)
			continue
		}
		seen[verb.Key] = true
		seen[verb.Key+`\command`] = true
		want := registryKey{"MUIVerb": verb.Label, "Icon": IconLocation(executable)}
		if verb.Extended {
			want["Extended"] = ""
		}
		if verb.Shield {
			want["HasLUAShield"] = ""
		}
		if verb.AppliesTo != "" {
			want["AppliesTo"] = verb.AppliesTo
		}
		if !reflect.DeepEqual(key, want) {
			t.Errorf("%s values for %s = %q, want %q", installer, verb.Key, key, want)
		}
		command := keys[verb.Key+`\command`][""]
		if command != verb.CommandLine(executable) {
			t.Errorf("%s command for %s = %s, want %s", installer, verb.Key, command, verb.CommandLine(executable))
			continue
		}
		argv := commandLineToArgv(expandDrive(strings.ReplaceAll(command, executable, resolved), `X:\`))
		if argv[0] != resolved {
			t.Errorf("%s command for %s starts with %q", installer, verb.Key, argv[0])
		}
		if len(argv) == 4 && argv[2] == "--drive" {
			if letter, err := NormalizeDrive(argv[3]); err != nil || letter != "X" {
				t.Errorf("%s command for %s yields drive %q", installer, verb.Key, argv[3])
			}
		}
	}
	for key := range keys {
		if !seen[key] {
			t.Errorf("%s registers %s, which is not in the Verbs table", installer, key)
		}
	}
}

func repoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// wixRegistryValues collects the HKLM registry values a WiX source writes.
func wixRegistryValues(t *testing.T, data []byte) map[string]registryKey {
	t.Helper()
	values := make(map[string]registryKey)
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "RegistryValue" {
			continue
		}
		attributes := make(map[string]string)
		for _, attribute := range element.Attr {
			attributes[attribute.Name.Local] = attribute.Value
		}
		if attributes["Root"] != "HKLM" {
			continue
		}
		key := attributes["Key"]
		if values[key] == nil {
			values[key] = make(registryKey)
		}
		values[key][attributes["Name"]] = attributes["Value"]
	}
	return values
}

// innoParameters splits one Inno Setup section line into its
// "Name: value" parameters. Quoted values use "" for a literal quote.
func innoParameters(t *testing.T, line string) map[string]string {
	t.Helper()
	parameters := make(map[string]string)
	for rest := strings.TrimSpace(line); rest != ""; {
		name, after, ok := strings.Cut(rest, ":")
		if !ok {
			t.Fatalf("malformed Inno parameter in %q", line)
		}
		after = strings.TrimLeft(after, " ")
		var value strings.Builder
		if strings.HasPrefix(after, `"`) {
			index := 1
			for {
				if index >= len(after) {
					t.Fatalf("unterminated Inno string in %q", line)
				}
				if after[index] == '"' {
					if index+1 < len(after) && after[index+1] == '"' {
						value.WriteByte('"')
						index += 2
						continue
					}
					index++
					break
				}
				value.WriteByte(after[index])
				index++
			}
			after = after[index:]
		} else {
			end := strings.Index(after, ";")
			if end < 0 {
				end = len(after)
			}
			value.WriteString(strings.TrimSpace(after[:end]))
			after = after[end:]
		}
		parameters[strings.TrimSpace(name)] = value.String()
		rest = strings.TrimLeft(strings.TrimPrefix(strings.TrimLeft(after, " "), ";"), " ")
	}
	return parameters
}

// innoRegistryValues collects the HKLM values of an Inno Setup [Registry]
// section, resolving the {{ escape for a literal brace.
func innoRegistryValues(t *testing.T, data []byte) map[string]registryKey {
	t.Helper()
	values := make(map[string]registryKey)
	section := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Registry]" {
			continue
		}
		parameters := innoParameters(t, line)
		if parameters["Root"] != "HKLM" {
			continue
		}
		key := strings.ReplaceAll(parameters["Subkey"], "{{", "{")
		if values[key] == nil {
			values[key] = make(registryKey)
		}
		values[key][parameters["ValueName"]] = parameters["ValueData"]
	}
	return values
}

func TestEXERegistersTheVerbTable(t *testing.T) {
	values := innoRegistryValues(t, repoFile(t, "installer", "MapFTPDrive.iss"))
	checkInstallerVerbs(t, "MapFTPDrive.iss", values, `{app}\MapFTPDrive.exe`, installedExecutable)
}

func TestInstallersRegisterOnlyTheMachineRunEntry(t *testing.T) {
	const run = `Software\Microsoft\Windows\CurrentVersion\Run`
	want := `"` + installedExecutable + `" daemon --scope user`
	for name, values := range map[string]map[string]registryKey{
		"MapFTPDrive.wxs": wixRegistryValues(t, repoFile(t, "installer", "MapFTPDrive.wxs")),
		"MapFTPDrive.iss": innoRegistryValues(t, repoFile(t, "installer", "MapFTPDrive.iss")),
	} {
		entry := values[run]["MapFTPDrive"]
		entry = strings.NewReplacer(`[INSTALLFOLDER]MapFTPDrive.exe`, installedExecutable, `{app}\MapFTPDrive.exe`, installedExecutable).Replace(entry)
		if entry != want {
			t.Errorf("%s HKLM Run entry = %q, want %q", name, entry, want)
		}
	}
	for _, path := range []string{"MapFTPDrive.wxs", "MapFTPDrive.iss"} {
		source := string(repoFile(t, "installer", path))
		for _, hive := range []string{`Root="HKCU"`, "Root: HKCU", "HKCU\\", "HKEY_CURRENT_USER"} {
			if strings.Contains(source, hive) {
				t.Errorf("%s writes to the per-user hive (%s)", path, hive)
			}
		}
	}
}

func TestMSIRegistersTheVerbTable(t *testing.T) {
	values := wixRegistryValues(t, repoFile(t, "installer", "MapFTPDrive.wxs"))
	checkInstallerVerbs(t, "MapFTPDrive.wxs", values, `[INSTALLFOLDER]MapFTPDrive.exe`, installedExecutable)
}
