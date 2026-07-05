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

	"github.com/gammazero/deque"
)

// shmNames tracks created shm object names so ShmCleanupAll can unlink them,
// since macOS has no /dev/shm directory to glob.
var shmNames struct {
	mu sync.Mutex
	q  deque.Deque[string]
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
	shmNames.q.PushBack(name)
	// KGP unlinks after read. By the time 500 frames have been written, we can assume
	// the shm file has already been unlinked.
	if shmNames.q.Len() > 500 {
		shmNames.q.PopFront()
	}
	shmNames.mu.Unlock()

	return nil
}

// ShmUnlink removes a single POSIX shared memory object by name.
func ShmUnlink(name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.shm_unlink(cname)
}

// ShmCleanupAll unlinks all tracked shared memory objects.
func ShmCleanupAll() {
	for shmNames.q.Len() != 0 {
		name := shmNames.q.PopFront()
		cname := C.CString(name)
		C.shm_unlink(cname)
		C.free(unsafe.Pointer(cname))
	}
}
