package config

import (
	"os"
	"path/filepath"
)

// writeFileDurable replaces path with data so that a crash or power loss
// leaves either the old or the new content, never a truncated file. The
// temporary file lives in the destination directory so it inherits that
// directory's ACL and the final rename stays on one volume. prepare, when
// set, runs on the closed temporary file before it is moved into place; the
// drive index uses it to apply its own read ACL.
func writeFileDurable(path string, data []byte, prepare func(string) error) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() {
		if err != nil {
			discardFile(temp)
		}
	}()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if prepare != nil {
		if err = prepare(temp); err != nil {
			return err
		}
	}
	if err = replaceFile(temp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// discardFile drops a temporary file left by a failed write; a failure here
// only leaves a stray ".tmp" beside the store, so it is not reported.
func discardFile(path string) {
	_ = os.Remove(path)
}
