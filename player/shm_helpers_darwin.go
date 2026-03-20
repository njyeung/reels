//go:build darwin

package player

/*
#include <fcntl.h>
#include <sys/mman.h>
#include <stdlib.h>
#include <unistd.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// shmTracker keeps track of created shm names so ShmCleanupAll can unlink them,
// since macOS has no /dev/shm directory to glob.
var shmTracker struct {
	mu    sync.Mutex
	names map[string]struct{}
}

func shmTrackAdd(name string) {
	shmTracker.mu.Lock()
	if shmTracker.names == nil {
		shmTracker.names = make(map[string]struct{})
	}
	shmTracker.names[name] = struct{}{}
	shmTracker.mu.Unlock()
}

func shmTrackRemove(name string) {
	shmTracker.mu.Lock()
	delete(shmTracker.names, name)
	shmTracker.mu.Unlock()
}

func shmOpen(name string, oflag int) (int, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	fd, err := C.shm_open(cname, C.int(oflag), 0600)
	if fd < 0 {
		return -1, fmt.Errorf("shm_open %q: %w", name, err)
	}
	return int(fd), nil
}

func shmUnlinkRaw(name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.shm_unlink(cname)
}

// ShmWrite creates a POSIX shared memory object, truncates it to len(data),
// and writes data into it via mmap.
func ShmWrite(name string, data []byte) error {
	fd, err := shmOpen(name, C.O_CREAT|C.O_RDWR|C.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	if err := unix.Ftruncate(fd, int64(len(data))); err != nil {
		unix.Close(fd)
		return err
	}

	mapped, err := unix.Mmap(fd, 0, len(data), unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		return err
	}
	copy(mapped, data)
	unix.Munmap(mapped)
	unix.Close(fd)

	shmTrackAdd(name)
	return nil
}

// ShmUnlink removes a single POSIX shared memory object by name.
func ShmUnlink(name string) {
	shmUnlinkRaw(name)
	shmTrackRemove(name)
}

// ShmCleanupAll unlinks all tracked shared memory objects.
func ShmCleanupAll() {
	shmTracker.mu.Lock()
	names := make([]string, 0, len(shmTracker.names))
	for n := range shmTracker.names {
		names = append(names, n)
	}
	shmTracker.mu.Unlock()

	for _, name := range names {
		ShmUnlink(name)
	}
}
