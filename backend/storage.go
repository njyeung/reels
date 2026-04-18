package backend

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type Settings struct {
	ShowNavbar       bool
	RetinaScale      int
	ReelWidth        int
	ReelHeight       int
	ReelSizeStep     int
	Volume           float64
	GifCellHeight    int
	PanelShrinkSteps int

	KeysNext         []string
	KeysPrevious     []string
	KeysMute         []string
	KeysPause        []string
	KeysLike         []string
	KeysNavbar       []string
	KeysReelSizeInc  []string
	KeysReelSizeDec  []string
	KeysVolUp        []string
	KeysVolDown      []string
	KeysQuit         []string
	KeysCopyLink     []string
	KeysSave         []string
	KeysSeekForward  []string
	KeysSeekBackward []string

	KeysSelect []string

	KeysShareOpen  []string
	KeysShareClose []string

	KeysCommentsOpen  []string
	KeysCommentsClose []string

	KeysHelpOpen  []string
	KeysHelpClose []string

	KeysFriendsOpen  []string
	KeysFriendsClose []string
}

var Config Settings

// confToKey maps key names in reels.conf to bubbletea KeyMsg.String() values.
var ConfToKey = map[string]string{
	"space":  " ",
	"escape": "esc",
}

// KeyToConf maps bubbletea KeyMsg.String() values to key names in reels.conf.
var KeyToConf = map[string]string{
	" ":   "space",
	"esc": "escape",
}

// GetSettings returns a snapshot copy of the current settings.
func GetSettings() Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return Config
}

// fifoCache is a bounded FIFO that evicts the oldest entry (and its file) when full.
type fifoCache struct {
	mu   sync.Mutex
	list []string
	set  map[string]bool
	max  int
}

func newFIFOCache(max int) *fifoCache {
	return &fifoCache{
		set: make(map[string]bool),
		max: max,
	}
}

func (c *fifoCache) has(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.set[path]
}

func (c *fifoCache) add(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.set[path] {
		return
	}
	c.list = append(c.list, path)
	c.set[path] = true
	for len(c.list) > c.max {
		os.Remove(c.list[0])
		delete(c.set, c.list[0])
		c.list = c.list[1:]
	}
}

var (
	videoCache    *fifoCache
	reelPfpCache  *fifoCache
	sharePfpCache *fifoCache
	gifCache      *fifoCache

	cacheMu sync.Mutex
	// inProgress tracks downloads currently in flight; channel is closed when done
	inProgress map[string]chan struct{}

	liked map[string]bool

	settingsMu sync.RWMutex
)

func (b *ChromeBackend) initStorage() error {
	videoCache = newFIFOCache(ReelCacheSize)
	reelPfpCache = newFIFOCache(ReelCacheSize)
	sharePfpCache = newFIFOCache(SharePfpCacheSize)
	gifCache = newFIFOCache(GifCacheSize)
	inProgress = make(map[string]chan struct{})
	liked = make(map[string]bool)

	// clear cache on startup
	if err := os.RemoveAll(b.cacheDir); err != nil {
		return fmt.Errorf("could not delete old cache directory")
	}
	if err := os.MkdirAll(b.cacheDir, 0755); err != nil {
		return fmt.Errorf("could not create new cache directory")
	}

	// ensure config directory exists
	if err := os.MkdirAll(b.configDir, 0755); err != nil {
		return fmt.Errorf("could not create config directory")
	}

	// write default settings if settings file doesn't exist
	settingsPath := filepath.Join(b.configDir, "reels.conf")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		writeConf(settingsPath, defaultSettings())
	}

	return nil
}

// cacheGif writes GIF data to the cache directory with FIFO eviction.
func (b *ChromeBackend) cacheGif(pk string, data []byte) string {
	path := filepath.Join(b.cacheDir, fmt.Sprintf("gif_%s.gif", pk))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return ""
	}
	gifCache.add(path)
	return path
}

// cacheReelPfp writes a reel profile picture to the cache directory with FIFO eviction.
func (b *ChromeBackend) cacheReelPfp(name string, data []byte) string {
	path := filepath.Join(b.cacheDir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return ""
	}
	reelPfpCache.add(path)
	return path
}

// cacheSharePfp writes a share panel avatar to the cache directory with FIFO eviction.
func (b *ChromeBackend) cacheSharePfp(name string, data []byte) string {
	path := filepath.Join(b.cacheDir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return ""
	}
	sharePfpCache.add(path)
	return path
}

func defaultSettings() Settings {
	s := Settings{
		ShowNavbar:       true,
		RetinaScale:      1,
		ReelWidth:        270,
		ReelHeight:       480,
		ReelSizeStep:     30,
		Volume:           1,
		GifCellHeight:    5,
		PanelShrinkSteps: 4,
		KeysNext:         []string{"j"},
		KeysPrevious:     []string{"k"},
		KeysPause:        []string{"p"},
		KeysMute:         []string{"m"},
		KeysLike:         []string{" "},
		KeysNavbar:       []string{"e"},
		KeysReelSizeInc:  []string{"="},
		KeysReelSizeDec:  []string{"-"},
		KeysVolUp:        []string{"]"},
		KeysVolDown:      []string{"["},
		KeysQuit:         []string{"q", "ctrl+c"},
		KeysCopyLink:     []string{"y"},
		KeysSave:         []string{"b"},
		KeysSeekForward:  []string{"l"},
		KeysSeekBackward: []string{"h"},

		KeysSelect: []string{" "},

		KeysShareOpen:  []string{"s"},
		KeysShareClose: []string{"S"},

		KeysCommentsOpen:  []string{"c"},
		KeysCommentsClose: []string{"C"},

		KeysHelpOpen:  []string{"?"},
		KeysHelpClose: []string{"?"},

		KeysFriendsOpen:  []string{"d"},
		KeysFriendsClose: []string{"D"},
	}

	if goruntime.GOOS == "darwin" {
		s.RetinaScale = 2
	}
	return s
}

// LoadSettings loads reels.conf from configDir into Config. Loads default settings on error
func LoadSettings(configDir string) {

	loadKey := func(conf map[string][]string, name string, dest *[]string) {
		if vals, ok := conf[name]; ok {
			resolved := make([]string, len(vals))
			for i, v := range vals {
				if r, ok := ConfToKey[v]; ok {
					resolved[i] = r
				} else {
					resolved[i] = v
				}
			}
			*dest = resolved
		}
	}

	s := defaultSettings()

	path := filepath.Join(configDir, "reels.conf")
	conf := parseConf(path)

	if vals, ok := conf["show_navbar"]; ok {
		s.ShowNavbar = (vals[len(vals)-1] == "true")
	}
	if vals, ok := conf["retina_scale"]; ok {
		if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
			s.RetinaScale = n
		}
	}
	if vals, ok := conf["reel_width"]; ok {
		if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
			s.ReelWidth = n
		}
	}
	if vals, ok := conf["reel_height"]; ok {
		if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
			s.ReelHeight = n
		}
	}
	if vals, ok := conf["reel_size_step"]; ok {
		if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
			s.ReelSizeStep = n
		}
	}
	if vals, ok := conf["volume"]; ok {
		if n, err := strconv.ParseFloat(vals[len(vals)-1], 64); err == nil {
			s.Volume = n
		}
	}
	if vals, ok := conf["gif_cell_height"]; ok {
		if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
			s.GifCellHeight = n
		}
	}
	if vals, ok := conf["panel_shrink_steps"]; ok {
		if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
			s.PanelShrinkSteps = n
		}
	}

	loadKey(conf, "key_next", &s.KeysNext)
	loadKey(conf, "key_previous", &s.KeysPrevious)
	loadKey(conf, "key_pause", &s.KeysPause)
	loadKey(conf, "key_mute", &s.KeysMute)
	loadKey(conf, "key_like", &s.KeysLike)
	loadKey(conf, "key_navbar", &s.KeysNavbar)
	loadKey(conf, "key_vol_up", &s.KeysVolUp)
	loadKey(conf, "key_vol_down", &s.KeysVolDown)
	loadKey(conf, "key_reel_size_inc", &s.KeysReelSizeInc)
	loadKey(conf, "key_reel_size_dec", &s.KeysReelSizeDec)
	loadKey(conf, "key_quit", &s.KeysQuit)
	loadKey(conf, "key_copy_link", &s.KeysCopyLink)
	loadKey(conf, "key_save", &s.KeysSave)
	loadKey(conf, "key_seek_forward", &s.KeysSeekForward)
	loadKey(conf, "key_seek_backward", &s.KeysSeekBackward)
	loadKey(conf, "key_share_open", &s.KeysShareOpen)
	loadKey(conf, "key_share_close", &s.KeysShareClose)
	loadKey(conf, "key_select", &s.KeysSelect)
	loadKey(conf, "key_comments_open", &s.KeysCommentsOpen)
	loadKey(conf, "key_comments_close", &s.KeysCommentsClose)
	loadKey(conf, "key_help_open", &s.KeysHelpOpen)
	loadKey(conf, "key_help_close", &s.KeysHelpClose)
	loadKey(conf, "key_friends_open", &s.KeysFriendsOpen)
	loadKey(conf, "key_friends_close", &s.KeysFriendsClose)

	Config = s
}

func writeConf(path string, s Settings) error {
	writeKeys := func(b *strings.Builder, name string, keys []string) {
		for _, key := range keys {
			if v, ok := KeyToConf[key]; ok {
				b.WriteString(fmt.Sprintf("%s = %s\n", name, v))
			} else {
				b.WriteString(fmt.Sprintf("%s = %s\n", name, key))
			}
		}
	}

	var b strings.Builder
	b.WriteString("# insta reels TUI config\n\n")
	b.WriteString(fmt.Sprintf("show_navbar = %t\n", s.ShowNavbar))
	b.WriteString(fmt.Sprintf("retina_scale = %d\n", s.RetinaScale))
	b.WriteString("\n")
	b.WriteString("# reels will be scales within this bounding box\n")
	b.WriteString(fmt.Sprintf("reel_width = %d\n", s.ReelWidth))
	b.WriteString(fmt.Sprintf("reel_height = %d\n", s.ReelHeight))
	b.WriteString(fmt.Sprintf("reel_size_step = %d\n", s.ReelSizeStep))
	b.WriteString(fmt.Sprintf("volume = %g\n", s.Volume))
	b.WriteString(fmt.Sprintf("gif_cell_height = %d\n", s.GifCellHeight))
	b.WriteString(fmt.Sprintf("panel_shrink = %d\n", s.PanelShrinkSteps))
	b.WriteString("\n")
	b.WriteString("# configurable keybinds\n")
	writeKeys(&b, "key_next", s.KeysNext)
	writeKeys(&b, "key_previous", s.KeysPrevious)
	writeKeys(&b, "key_pause", s.KeysPause)
	writeKeys(&b, "key_mute", s.KeysMute)
	writeKeys(&b, "key_like", s.KeysLike)
	writeKeys(&b, "key_navbar", s.KeysNavbar)
	writeKeys(&b, "key_vol_up", s.KeysVolUp)
	writeKeys(&b, "key_vol_down", s.KeysVolDown)
	writeKeys(&b, "key_reel_size_inc", s.KeysReelSizeInc)
	writeKeys(&b, "key_reel_size_dec", s.KeysReelSizeDec)
	writeKeys(&b, "key_copy_link", s.KeysCopyLink)
	writeKeys(&b, "key_save", s.KeysSave)
	writeKeys(&b, "key_quit", s.KeysQuit)
	writeKeys(&b, "key_seek_forward", s.KeysSeekForward)
	writeKeys(&b, "key_seek_backward", s.KeysSeekBackward)
	writeKeys(&b, "key_share_open", s.KeysShareOpen)
	writeKeys(&b, "key_share_close", s.KeysShareClose)
	writeKeys(&b, "key_select", s.KeysSelect)
	writeKeys(&b, "key_comments_open", s.KeysCommentsOpen)
	writeKeys(&b, "key_comments_close", s.KeysCommentsClose)
	writeKeys(&b, "key_help_open", s.KeysHelpOpen)
	writeKeys(&b, "key_help_close", s.KeysHelpClose)
	writeKeys(&b, "key_friends_open", s.KeysFriendsOpen)
	writeKeys(&b, "key_friends_close", s.KeysFriendsClose)

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func parseConf(path string) map[string][]string {
	result := make(map[string][]string)
	file, err := os.Open(path)
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			key := strings.TrimSpace(k)
			result[key] = append(result[key], strings.TrimSpace(v))
		}
	}
	return result
}

// SetReelSize updates the reel bounding box dimensions and persists to disk.
func (b *ChromeBackend) SetReelSize(width, height int) error {
	settingsMu.Lock()
	Config.ReelWidth = width
	Config.ReelHeight = height
	snapshot := Config
	settingsMu.Unlock()

	path := filepath.Join(b.configDir, "reels.conf")
	go writeConf(path, snapshot)
	return nil
}

// ToggleNavbar updates navbar state to !state, persists to disk, and returns the new state of the navbar
func (b *ChromeBackend) ToggleNavbar() bool {
	settingsMu.Lock()
	Config.ShowNavbar = !Config.ShowNavbar
	showNavbar := Config.ShowNavbar
	snapshot := Config
	settingsMu.Unlock()

	path := filepath.Join(b.configDir, "reels.conf")
	go writeConf(path, snapshot)
	return showNavbar
}

// SetVolume updates volume and persists to disk
func (b *ChromeBackend) SetVolume(vol float64) error {
	settingsMu.Lock()
	Config.Volume = vol
	snapshot := Config
	settingsMu.Unlock()

	path := filepath.Join(b.configDir, "reels.conf")
	go writeConf(path, snapshot)
	return nil
}

// fetchURLs fetches multiple URLs in parallel via a single chromedp Promise.all call,
// returning the decoded bytes for each URL (nil if the fetch failed).
func (b *ChromeBackend) fetchURLs(ctx context.Context, urls []string) [][]byte {
	if len(urls) == 0 {
		return nil
	}

	urlsJSON, _ := json.Marshal(urls)

	js := fmt.Sprintf(`
		(async () => {
			const urls = %s;
			const results = await Promise.all(urls.map(async (url) => {
				if (!url) return "";
				try {
					const r = await fetch(url);
					const buf = await r.arrayBuffer();
					const bytes = new Uint8Array(buf);
					let binary = '';
					for (let i = 0; i < bytes.length; i += 8192) {
						binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
					}
					return btoa(binary);
				} catch(e) { return ""; }
			}));
			return JSON.stringify(results);
		})()
	`, string(urlsJSON))

	var result string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		return make([][]byte, len(urls))
	}

	var b64s []string
	if err := json.Unmarshal([]byte(result), &b64s); err != nil {
		return make([][]byte, len(urls))
	}

	data := make([][]byte, len(urls))
	for i, s := range b64s {
		if s == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err == nil {
			data[i] = decoded
		}
	}
	return data
}

// Download downloads a reel video and profile picture to the cache directory.
// In friend mode, the index is into the active friend's entries and the reel is
// pulled from friendReels (populated on navigation) rather than orderedReels.
func (b *ChromeBackend) Download(index int) (string, string, error) {
	b.modeMu.RLock()
	friendMode := b.viewMode == ViewModeFriend
	username := b.activeFriend
	b.modeMu.RUnlock()

	var reel Reel
	var fileStem string // unique stem for cache filenames
	var pfpName string

	if friendMode {
		entries, ok := b.findFriend(username)
		if !ok {
			return "", "", fmt.Errorf("active friend %q no longer present", username)
		}
		if index < 1 || index > len(entries) {
			return "", "", fmt.Errorf("index out of range")
		}
		pk := entries[index-1].TargetID
		b.modeMu.RLock()
		r, ok := b.friendReels[pk]
		b.modeMu.RUnlock()
		if !ok {
			return "", "", fmt.Errorf("reel pk=%s not yet captured", pk)
		}
		reel = *r
		fileStem = fmt.Sprintf("dm_%s_%s", username, reel.Code)
		pfpName = fmt.Sprintf("dm_%s_%s_pfp.jpg", username, reel.Code)
	} else {
		b.reelsMu.RLock()
		if index < 1 || index > len(b.orderedReels) {
			b.reelsMu.RUnlock()
			return "", "", fmt.Errorf("index out of range")
		}
		reel = b.orderedReels[index-1]
		b.reelsMu.RUnlock()
		fileStem = fmt.Sprintf("%03d_%s", index, reel.Code)
		pfpName = fmt.Sprintf("%03d_%s_pfp.jpg", index, reel.Code)
	}

	if reel.VideoURL == "" {
		return "", "", fmt.Errorf("no video URL")
	}

	videoFile := filepath.Join(b.cacheDir, fileStem+".mp4")
	pfpFile := filepath.Join(b.cacheDir, pfpName)

	// check cache to see if already downloaded
	if videoCache.has(videoFile) {
		return videoFile, pfpFile, nil
	}

	cacheMu.Lock()
	// re-check under lock
	if videoCache.has(videoFile) {
		cacheMu.Unlock()
		return videoFile, pfpFile, nil
	}

	// check if in the progress of being downloaded
	if ch, ok := inProgress[videoFile]; ok {
		cacheMu.Unlock()
		<-ch // Wait for the other download to complete
		if videoCache.has(videoFile) {
			return videoFile, pfpFile, nil
		}
		cacheMu.Lock()
	}

	// Mark as in progress
	done := make(chan struct{})
	inProgress[videoFile] = done
	cacheMu.Unlock()
	// cleanup: remove from inProgress and signal waiters when done
	defer func() {
		cacheMu.Lock()
		delete(inProgress, videoFile)
		cacheMu.Unlock()
		close(done)
	}()

	// Download video and profile pic in parallel
	urls := []string{reel.VideoURL}
	if reel.ProfilePicUrl != "" {
		urls = append(urls, reel.ProfilePicUrl)
	}

	data := b.fetchURLs(b.activeCtx(), urls)
	if data[0] == nil {
		return "", "", fmt.Errorf("failed to download video")
	}

	if err := os.WriteFile(videoFile, data[0], 0644); err != nil {
		return "", "", err
	}
	videoCache.add(videoFile)

	if len(data) > 1 && data[1] != nil {
		b.cacheReelPfp(pfpName, data[1])
	}

	return videoFile, pfpFile, nil
}
