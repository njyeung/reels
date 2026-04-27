package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// CheckAndSelfUpdate. When reels was installed via npm and a newer release is
// available, downloads the new platform binary, atomically swaps it into place
// over the running executable, then re-execs the new binary with the same
// argv/env. On success this function does not return, the new program takes over.
//
// On any failure (not npm, no update, network error, no write perms, etc.),
// it returns silently and lets the app continue normally.
//
// Must be called BEFORE the TUI starts and BEFORE any child processes (Chrome)
// are spawned, since exec replaces the current image but does not touch
// children.
func CheckAndSelfUpdate(currentVersion string) {
	if currentVersion == "dev" {
		return
	}
	exePath, ok := detectNpmInstall()
	if !ok {
		return
	}
	asset, ok := releaseAssetName()
	if !ok {
		return
	}
	latest, ok := fetchLatestVersion()
	if !ok || latest == "" || latest == currentVersion {
		return
	}

	url := fmt.Sprintf("https://github.com/njyeung/reels/releases/download/v%s/%s", latest, asset)
	if err := downloadAndReplace(url, exePath); err != nil {
		fmt.Fprintf(os.Stderr, "reels: self-update failed: %v\n", err)
		return
	}

	// Since we're on UNIX, we can re-exec the new binary with the same argv and env.
	// On success this replaces the running process with a new process and does not return
	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "reels: failed to relaunch after update: %v\n", err)
		os.Exit(1)
	}
}

// detectNpmInstall returns the resolved binary path if reels was installed via
// npm (i.e. lives under a node_modules/@reels/<plat>/bin/reels path).
func detectNpmInstall() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	norm := filepath.ToSlash(resolved)
	if !strings.Contains(norm, "/node_modules/@reels/") {
		return "", false
	}
	if !strings.HasSuffix(norm, "/bin/reels") {
		return "", false
	}
	return resolved, true
}

// fetchLatestVersion queries the GitHub releases API for the most recent tag
// (with the leading "v" stripped)
func fetchLatestVersion() (string, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/njyeung/reels/releases/latest")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", false
	}
	return strings.TrimPrefix(release.TagName, "v"), true
}

// releaseAssetName maps the current GOOS/GOARCH to the asset name uploaded
// by the GitHub Actions release workflow.
func releaseAssetName() (string, bool) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "reels-linux-amd64", true
	case "linux/arm64":
		return "reels-linux-arm64", true
	case "darwin/arm64":
		return "reels-darwin-arm64", true
	}
	return "", false
}

func downloadAndReplace(url, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	return writeAtomic(target, resp.Body)
}

// writeAtomic streams body into a temp file in the same directory as target,
// then renames it onto target. On Unix this is atomic and works even if the
// target file is currently being executed (the kernel keeps the old inode
// alive until the process exits).
func writeAtomic(target string, body io.Reader) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".reels-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}
