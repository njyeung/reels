package tui

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"
)

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
