package config

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// EndpointKey derives the on-disk name used for an endpoint's files (pinned
// host key, VFS cache, copied private key). File names never carry the raw
// ID, so a store entry cannot steer a write outside its directory even if
// ID validation were bypassed.
func EndpointKey(id string) string {
	sum := sha256.Sum256([]byte("mapftpdrive-endpoint:" + id))
	return hex.EncodeToString(sum[:8])
}

// Layout names every file a scope keeps beside its endpoint store. The
// directory is the scope's directory: %LOCALAPPDATA%\MapFTPDrive for users
// and the protected %ProgramData%\MapFTPDrive for the system service, both
// derived from DefaultPath.
type Layout struct {
	Dir string
}

func LayoutFor(storePath string) Layout {
	return Layout{Dir: filepath.Dir(storePath)}
}

func (l Layout) KnownHostsDir() string { return filepath.Join(l.Dir, "known_hosts") }

func (l Layout) KnownHostsFile(id string) string {
	return filepath.Join(l.KnownHostsDir(), EndpointKey(id))
}

func (l Layout) KeysDir() string { return filepath.Join(l.Dir, "keys") }

// KeyFile is where the host keeps its own copy of an endpoint's private key,
// so the mount does not depend on the file the user picked staying in place.
func (l Layout) KeyFile(id string) string { return filepath.Join(l.KeysDir(), EndpointKey(id)) }

// CacheRoot is handed to rclone as its cache directory; rclone places each
// endpoint's VFS data under vfs\<name> and vfsMeta\<name>, where the engine
// uses EndpointKey as the remote name.
func (l Layout) CacheRoot() string { return filepath.Join(l.Dir, "cache") }

// CacheDirs lists the directories rclone creates for one endpoint's VFS
// cache; Remove deletes them.
func (l Layout) CacheDirs(id string) []string {
	key := EndpointKey(id)
	return []string{
		filepath.Join(l.CacheRoot(), "vfs", key),
		filepath.Join(l.CacheRoot(), "vfsMeta", key),
	}
}

func (l Layout) LogDir() string { return filepath.Join(l.Dir, "logs") }
