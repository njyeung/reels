//go:build linux

package player

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// ShmSupported returns true if /dev/shm is available and the terminal supports
// the kitty graphics protocol t=s (shared memory) transmission.
//
// IMPORTANT: MUST BE CALLED BEFORE BUBBLETEA STARTS
func ShmSupported() bool {
	// FS check
	info, err := os.Stat("/dev/shm")
	if err != nil || !info.IsDir() {
		return false
	}

	// Terminal check
	//
	//	Ask the terminal to transmit and display a 1x1 image from shared memory:
	//	\x1b_Ga=T,f=24,s=1,v=1,i=999,t=s;{base64 name}\x1b\\
	//
	//	No q=, so the terminal needs to respond.
	//	Read response, a supporting terminal responds with \x1b_Gi=999;OK\x1b\\
	//	A non-supporting terminal either errors or times out after 200ms
	//

	stdinFd := int(os.Stdin.Fd())

	// Save terminal settings
	oldTermios, err := unix.IoctlGetTermios(stdinFd, unix.TCGETS)
	if err != nil {
		return false
	}

	// Set raw mode to read the terminal response
	raw := *oldTermios
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG
	raw.Iflag &^= unix.IXON | unix.ICRNL
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 2 // 200ms timeout
	if err := unix.IoctlSetTermios(stdinFd, unix.TCSETS, &raw); err != nil {
		return false
	}
	defer unix.IoctlSetTermios(stdinFd, unix.TCSETS, oldTermios)

	// Drain any pending input
	drain := make([]byte, 256)
	os.Stdin.Read(drain)

	// Create a tiny 1x1 RGB test shm
	const testName = "/kitty-reels-test"
	const testPath = "/dev/shm" + testName
	if err := os.WriteFile(testPath, []byte{0, 0, 0}, 0600); err != nil {
		return false
	}
	defer os.Remove(testPath)

	// Send test image via t=s
	// no q= so terminal responds
	encodedName := base64.StdEncoding.EncodeToString([]byte(testName))
	fmt.Fprintf(os.Stdout, "\x1b_Ga=T,f=24,s=1,v=1,i=999,t=s;%s\x1b\\", encodedName)

	// Read response
	// A supported terminal would respond with \x1b_Gi=999;OK\x1b\\
	buf := make([]byte, 256)
	n, _ := os.Stdin.Read(buf)

	// Clean up the test image from the terminal
	fmt.Fprint(os.Stdout, "\x1b_Ga=d,d=i,i=999,q=2\x1b\\")

	return n > 0 && strings.Contains(string(buf[:n]), "OK")
}
