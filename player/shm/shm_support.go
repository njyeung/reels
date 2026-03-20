package shm

import (
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// ShmSupported returns true if the terminal supports the kitty graphics
// protocol t=s (shared memory) transmission.
//
// IMPORTANT: MUST BE CALLED BEFORE BUBBLETEA STARTS
func ShmSupported() bool {
	// On Linux, /dev/shm must exist for the filesystem-based approach.
	if runtime.GOOS == "linux" {
		info, err := os.Stat("/dev/shm")
		if err != nil || !info.IsDir() {
			return false
		}
	}

	// Terminal probe: put stdin in raw mode, send a t=s test image,
	// check if the terminal responds with OK.
	stdinFd := int(os.Stdin.Fd())

	oldTermios, err := unix.IoctlGetTermios(stdinFd, ioctlGetTermios)
	if err != nil {
		return false
	}

	raw := *oldTermios
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG
	raw.Iflag &^= unix.IXON | unix.ICRNL
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 2 // 200ms timeout
	if err := unix.IoctlSetTermios(stdinFd, ioctlSetTermios, &raw); err != nil {
		return false
	}
	defer unix.IoctlSetTermios(stdinFd, ioctlSetTermios, oldTermios)

	// Drain any pending input
	drain := make([]byte, 256)
	os.Stdin.Read(drain)

	// Create a tiny 1x1 RGB test shm
	const testName = "/kitty-reels-test"
	if err := ShmWrite(testName, []byte{0, 0, 0}); err != nil {
		return false
	}
	defer ShmUnlink(testName)

	// Send test image via t=s (no q= so terminal responds)
	encodedName := base64.StdEncoding.EncodeToString([]byte(testName))
	fmt.Fprintf(os.Stdout, "\x1b_Ga=T,f=24,s=1,v=1,i=999,t=s;%s\x1b\\", encodedName)

	// Read response — a supported terminal responds with \x1b_Gi=999;OK\x1b\\
	buf := make([]byte, 256)
	n, _ := os.Stdin.Read(buf)

	// Clean up the test image from the terminal
	fmt.Fprint(os.Stdout, "\x1b_Ga=d,d=i,i=999,q=2\x1b\\")

	return n > 0 && strings.Contains(string(buf[:n]), "OK")
}
