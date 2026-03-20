//go:build linux

package shm

import (
	"os"
	"path/filepath"
)

// ShmWrite creates (or overwrites) a POSIX shared memory object and writes
// data to it. On Linux this is a file write to /dev/shm.
func ShmWrite(name string, data []byte) error {
	path := "/dev/shm" + name
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err2 := f.Close(); err == nil {
		err = err2
	}
	return err
}

// ShmUnlink removes a POSIX shared memory object by name.
func ShmUnlink(name string) {
	os.Remove("/dev/shm" + name)
}

// ShmCleanupAll removes all shared memory objects matching the kitty-reels prefix.
func ShmCleanupAll() {
	matches, _ := filepath.Glob("/dev/shm/kitty-reels-*")
	for _, m := range matches {
		os.Remove(m)
	}
}
