//go:build !linux

package player

// ShmSupported returns false on darwin.
func ShmSupported() bool {
	return false
}
