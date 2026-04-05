package backend

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	chromeVersion = "147.0.7727.50"
	chromeBaseURL = "https://storage.googleapis.com/chrome-for-testing-public"
)

// platformString returns the Chrome for Testing platform identifier,
// or an error if our app or chrome-for-testing does not support the platform
func platformString() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if runtime.GOARCH == "amd64" {
			return "linux64", nil
		}
		// linux arm64 support coming Q2 2026
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "mac-arm64", nil
		}
	}
	return "", fmt.Errorf("Platform not supported")
}

// chromeBinaryName returns the name of the Chrome executable inside the zip.
// macOS arm64: chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing
// Linux amd64: chrome-linux64/chrome
func chromeBinaryName(platform string) string {
	switch platform {
	case "mac-arm64":
		return filepath.Join("chrome-mac-arm64", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing")
	case "linux64":
		return filepath.Join("chrome-linux64", "chrome")
	// linux arm64 support coming Q2 2026
	default:
		return ""
	}
}

// EnsureChromium returns the path to a Chrome binary, using the following priority:
//  1. Our managed Chrome download in ~/.local/share/reels/chromium/
//  2. A system-installed Chrome/Chromium found via PATH or well-known locations
//  3. Auto-download Chrome for Testing if no binary was found in either locations (linux64, mac-arm64)
func EnsureChromium(userDataDir string) (string, error) {
	chromiumDir := filepath.Join(filepath.Dir(userDataDir), "chromium")

	platform, err := platformString()

	// Check for our managed download
	if err == nil {
		path := filepath.Join(chromiumDir, chromeBinaryName(platform))
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Check for system Chrome
	if path := findSystemChrome(); path != "" {
		return path, nil
	}

	// Download Chrome for Testing (only if platform is supported)
	if err != nil {
		return "", fmt.Errorf(
			"no Chrome/Chromium found and auto-download is not available for %s/%s\n"+
				"Please install Chromium via your package manager",
			runtime.GOOS, runtime.GOARCH,
		)
	}
	if err := downloadChrome(chromiumDir, platform); err != nil {
		return "", fmt.Errorf("failed to download Chrome: %w", err)
	}

	return filepath.Join(chromiumDir, chromeBinaryName(platform)), nil
}

// findSystemChrome looks for an existing Chrome/Chromium installation.
func findSystemChrome() string {
	var locations []string

	switch runtime.GOOS {
	case "darwin":
		locations = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"google-chrome",
			"chromium",
			"chrome",
		}
	default: // linux
		locations = []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"brave-browser",
			"chrome",
			"/usr/bin/google-chrome",
			"/usr/local/bin/chrome",
			"/snap/bin/chromium",
		}
	}

	for _, loc := range locations {
		if path, err := exec.LookPath(loc); err == nil {
			return path
		}
	}
	return ""
}

// downloadChrome downloads and extracts Chrome for Testing into destDir.
func downloadChrome(destDir, platform string) error {
	url := fmt.Sprintf("%s/%s/%s/chrome-%s.zip", chromeBaseURL, chromeVersion, platform, platform)

	fmt.Fprintf(os.Stderr, "Chrome not found — downloading Chrome for Testing v%s (%s)...\n", chromeVersion, platform)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// Write to a temp file so we can use archive/zip (needs random access)
	tmp, err := os.CreateTemp("", "chrome-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	_, err = io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download interrupted: %w", err)
	}
	tmp.Close()

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	// Extract zip
	r, err := zip.OpenReader(tmpPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)

		// Guard against zip slip
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
