//go:build darwin

package shm

/*
#include <fcntl.h>
#include <sys/mman.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

// Returns 0 on success, negative on error.
static int shm_write(const char *name, const void *data, size_t len) {
	int fd = shm_open(name, O_CREAT | O_RDWR | O_TRUNC, 0600);
	if (fd < 0) return -1;

	if (ftruncate(fd, (off_t)len) != 0) {
		close(fd);
		shm_unlink(name);
		return -2;
	}

	void *mapped = mmap(NULL, len, PROT_WRITE, MAP_SHARED, fd, 0);
	close(fd);
	if (mapped == MAP_FAILED) {
		shm_unlink(name);
		return -3;
	}

	memcpy(mapped, data, len);
	munmap(mapped, len);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe" // for passing Go memory to C without allocation
)

// shmNames tracks created shm object names so ShmCleanupAll can unlink them,
// since macOS has no /dev/shm directory to glob.
var shmNames struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// ShmWrite creates a POSIX shared memory object and writes data into it.
// Uses a CGO call to perform shm_open, ftruncate, mmap, memcpy, and munmap
func ShmWrite(name string, data []byte) error {
	// nameBuf is zero initialized, so nameBuf[len(name)] is the null terminator
	var nameBuf [64]byte
	if len(name) >= len(nameBuf) {
		return fmt.Errorf("shm name %q exceeds %d bytes", name, len(nameBuf)-1)
	}
	copy(nameBuf[:], name)

	ret := C.shm_write((*C.char)(unsafe.Pointer(&nameBuf[0])), unsafe.Pointer(&data[0]), C.size_t(len(data)))
	if ret != 0 {
		return fmt.Errorf("shm_write %q failed (code %d)", name, ret)
	}

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
