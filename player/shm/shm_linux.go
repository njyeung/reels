//go:build linux

package shm

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ShmWrite creates (or overwrites) a POSIX shared memory object and writes
// data to it. On Linux this is a file write to /dev/shm using raw syscalls.
func ShmWrite(name string, data []byte) error {
	path := "/dev/shm" + name
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_WRONLY|unix.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := unix.Ftruncate(fd, int64(len(data))); err != nil {
		unix.Close(fd)
		return err
	}
	_, err = unix.Write(fd, data)
	if err2 := unix.Close(fd); err == nil {
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
