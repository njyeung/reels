//go:build darwin

package player

/*
#include <fcntl.h>
#include <sys/mman.h>
#include <stdlib.h>
#include <unistd.h>

// Wrapper needed because shm_open is variadic on macOS and cgo cannot call
// variadic C functions directly.
static int shm_open_wrapper(const char *name, int oflag, mode_t mode) {
	return shm_open(name, oflag, mode);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// shmNames tracks created shm object names so ShmCleanupAll can unlink them,
// since macOS has no /dev/shm directory to glob.
var shmNames struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// ShmWrite creates a POSIX shared memory object, truncates it to len(data),
// and writes data into it via mmap.
func ShmWrite(name string, data []byte) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	fd, err := C.shm_open_wrapper(cname, C.O_CREAT|C.O_RDWR|C.O_TRUNC, 0600)
	if fd < 0 {
		return fmt.Errorf("shm_open %q: %w", name, err)
	}
	goFd := int(fd)

	if err := unix.Ftruncate(goFd, int64(len(data))); err != nil {
		unix.Close(goFd)
		C.shm_unlink(cname)
		return err
	}

	mapped, err := unix.Mmap(goFd, 0, len(data), unix.PROT_WRITE, unix.MAP_SHARED)
	unix.Close(goFd)
	if err != nil {
		C.shm_unlink(cname)
		return err
	}
	copy(mapped, data)
	unix.Munmap(mapped)

	shmNames.mu.Lock()
	if shmNames.m == nil {
		shmNames.m = make(map[string]struct{})
	}
	shmNames.m[name] = struct{}{}
	shmNames.mu.Unlock()

	return nil
}

// ShmUnlink removes a single POSIX shared memory object by name.
func ShmUnlink(name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.shm_unlink(cname)

	shmNames.mu.Lock()
	delete(shmNames.m, name)
	shmNames.mu.Unlock()
}

// ShmCleanupAll unlinks all tracked shared memory objects.
func ShmCleanupAll() {
	shmNames.mu.Lock()
	m := shmNames.m
	shmNames.m = nil
	shmNames.mu.Unlock()

	for name := range m {
		cname := C.CString(name)
		C.shm_unlink(cname)
		C.free(unsafe.Pointer(cname))
	}
}
