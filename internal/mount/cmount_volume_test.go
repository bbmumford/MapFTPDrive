package mount

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolveVolumePrefix(t *testing.T) {
	const id = "0f8fad5b-d9cb-469f-a165-70867728950e"
	tests := []struct {
		name         string
		flags        []string
		networkMode  bool
		uncMountPath string
		wantFlags    []string
		wantNetwork  bool
		wantErr      bool
	}{
		{
			name:      "disk volume without prefix keeps flags",
			flags:     []string{"--FileSystemName=x"},
			wantFlags: []string{"--FileSystemName=x"},
		},
		{
			name:        "network volume with single-backslash prefix",
			flags:       []string{"--FileSystemName=x", `--VolumePrefix=\mapftpdrive\` + id},
			networkMode: true,
			wantFlags:   []string{"--FileSystemName=x", `--VolumePrefix=\mapftpdrive\` + id},
			wantNetwork: true,
		},
		{
			name:        "UNC form is shortened to one leading backslash",
			flags:       []string{`--VolumePrefix=\\mapftpdrive\` + id},
			networkMode: true,
			wantFlags:   []string{`--VolumePrefix=\mapftpdrive\` + id},
			wantNetwork: true,
		},
		{
			name:        "prefix forces network mode",
			flags:       []string{`--VolumePrefix=\mapftpdrive\` + id},
			wantFlags:   []string{`--VolumePrefix=\mapftpdrive\` + id},
			wantNetwork: true,
		},
		{
			name:         "UNC mount path replaces a prefix flag",
			flags:        []string{`--VolumePrefix=\other\share`, "--x"},
			uncMountPath: `\server\share`,
			wantFlags:    []string{"--x", `--VolumePrefix=\server\share`},
			wantNetwork:  true,
		},
		{name: "network mode without prefix", networkMode: true, wantErr: true},
		{name: "two prefixes", flags: []string{`--VolumePrefix=\a\b`, `--VolumePrefix=\c\d`}, wantErr: true},
		{name: "prefix without share", flags: []string{`--VolumePrefix=\mapftpdrive`}, wantErr: true},
		{name: "prefix with extra component", flags: []string{`--VolumePrefix=\a\b\c`}, wantErr: true},
		{name: "prefix with empty host", flags: []string{`--VolumePrefix=\\\b`}, wantErr: true},
		{name: "prefix without leading backslash", flags: []string{`--VolumePrefix=a\b`}, wantErr: true},
		{name: "prefix too long", flags: []string{`--VolumePrefix=\a\` + strings.Repeat("b", 189)}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flags, network, err := resolveVolumePrefix(test.flags, test.networkMode, test.uncMountPath)
			if test.wantErr {
				if err == nil {
					t.Fatalf("resolveVolumePrefix() = %q, %v; want error", flags, network)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveVolumePrefix() error = %v", err)
			}
			if !reflect.DeepEqual(flags, test.wantFlags) || network != test.wantNetwork {
				t.Fatalf("resolveVolumePrefix() = %q, %v; want %q, %v", flags, network, test.wantFlags, test.wantNetwork)
			}
		})
	}
}

func TestResolveVolumePrefixMissingNamesLabel(t *testing.T) {
	_, _, err := resolveVolumePrefix(nil, true, "")
	if !errors.Is(err, errNetworkPrefixMissing) {
		t.Fatalf("error = %v, want errNetworkPrefixMissing", err)
	}
}

func TestResolveVolumePrefixLengthLimit(t *testing.T) {
	prefix := `\a\` + strings.Repeat("b", volumePrefixMaxLength-3)
	if _, _, err := resolveVolumePrefix([]string{winfspVolumePrefixFlag + prefix}, true, ""); err != nil {
		t.Fatalf("prefix of %d code units rejected: %v", volumePrefixMaxLength, err)
	}
}

func TestVolumeLabelOptions(t *testing.T) {
	tests := []struct {
		name  string
		label string
		extra []string
		want  []string
	}{
		{name: "plain", label: "Work files", want: []string{"-o", "volname=Work files"}},
		{name: "comma and backslash are escaped", label: `a,b\c`, want: []string{"-o", `volname=a\,b\\c`}},
		{name: "empty label adds nothing", label: ""},
		{name: "caller volname wins", label: "x", extra: []string{"volname=y"}},
		{name: "partial trailing character is dropped", label: "Caf\xc3", want: []string{"-o", "volname=Caf"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := volumeLabelOptions(test.label, test.extra)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("volumeLabelOptions(%q) = %q, want %q", test.label, got, test.want)
			}
		})
	}
}

// TestEscapeOptionValueRoundTrip unescapes with the rule WinFsp's "-o"
// parser applies (a backslash copies the next byte, an unescaped comma ends
// the entry) and checks that every value survives as one entry.
func TestEscapeOptionValueRoundTrip(t *testing.T) {
	for _, value := range []string{"", "plain", `a,b`, `a\b`, `\,\\,`, `trailing\`, "ünïcødé, 名前"} {
		escaped := escapeOptionValue(value)
		var entries []string
		var current []byte
		for i := 0; i < len(escaped); i++ {
			switch escaped[i] {
			case '\\':
				i++
				current = append(current, escaped[i])
			case ',':
				entries = append(entries, string(current))
				current = nil
			default:
				current = append(current, escaped[i])
			}
		}
		entries = append(entries, string(current))
		if len(entries) != 1 || entries[0] != value {
			t.Fatalf("escapeOptionValue(%q) = %q parses as %q", value, escaped, entries)
		}
	}
}
