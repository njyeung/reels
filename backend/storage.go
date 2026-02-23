package backend

import (
	"bufio"
	"context"
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
	ShowNavbar  bool
	RetinaScale int
	ReelWidth    int
	ReelHeight   int
	ReelSizeStep int
	Volume       float64

	KeysNext        []string
	KeysPrevious    []string
	KeysMute        []string
	KeysPause       []string
	KeysLike        []string
	KeysComments    []string
	KeysNavbar      []string
	KeysReelSizeInc []string
	KeysReelSizeDec []string
	KeysVolUp       []string
	KeysVolDown     []string
	KeysQuit        []string
	KeysShare       []string
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

var (
	cacheMu  sync.Mutex
	fifoList []string
	fifoMap  map[string]bool

	// inProgress tracks downloads currently in flight; channel is closed when done
	inProgress map[string]chan struct{}

	liked map[string]bool

	settingsMu sync.RWMutex
)

func (b *ChromeBackend) initStorage() error {
	if CacheSize < 1 {
		return fmt.Errorf("cannot have a cache size < 1")
	}

	fifoMap = make(map[string]bool)
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

func add(filepath string) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	fifoList = append(fifoList, filepath)
	fifoMap[filepath] = true

	if len(fifoList) > CacheSize {
		if err := os.Remove(fifoList[0]); err != nil {
			return fmt.Errorf("could not remove cached reel")
		}
		delete(fifoMap, fifoList[0])
		fifoList = fifoList[1:]
	}

	return nil
}

func defaultSettings() Settings {
	s := Settings{
		ShowNavbar:      true,
		RetinaScale:     1,
		ReelWidth:       270,
		ReelHeight:      480,
		ReelSizeStep:    30,
		Volume:          1,
		KeysNext:        []string{"j"},
		KeysPrevious:    []string{"k"},
		KeysPause:       []string{" "},
		KeysMute:        []string{"m"},
		KeysLike:        []string{"l"},
		KeysComments:    []string{"c"},
		KeysNavbar:      []string{"e"},
		KeysVolUp:       []string{"]"},
		KeysVolDown:     []string{"["},
		KeysReelSizeInc: []string{"="},
		KeysReelSizeDec: []string{"-"},
		KeysShare:       []string{"s"},
		KeysQuit:        []string{"q", "ctrl+c"},
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

	loadKey(conf, "key_next", &s.KeysNext)
	loadKey(conf, "key_previous", &s.KeysPrevious)
	loadKey(conf, "key_pause", &s.KeysPause)
	loadKey(conf, "key_mute", &s.KeysMute)
	loadKey(conf, "key_like", &s.KeysLike)
	loadKey(conf, "key_comments", &s.KeysComments)
	loadKey(conf, "key_navbar", &s.KeysNavbar)
	loadKey(conf, "key_vol_up", &s.KeysVolUp)
	loadKey(conf, "key_vol_down", &s.KeysVolDown)
	loadKey(conf, "key_reel_size_inc", &s.KeysReelSizeInc)
	loadKey(conf, "key_reel_size_dec", &s.KeysReelSizeDec)
	loadKey(conf, "key_quit", &s.KeysQuit)
	loadKey(conf, "key_share", &s.KeysShare)

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
	b.WriteString("\n")
	b.WriteString("# configurable keybinds\n")
	writeKeys(&b, "key_next", s.KeysNext)
	writeKeys(&b, "key_previous", s.KeysPrevious)
	writeKeys(&b, "key_pause", s.KeysPause)
	writeKeys(&b, "key_mute", s.KeysMute)
	writeKeys(&b, "key_like", s.KeysLike)
	writeKeys(&b, "key_comments", s.KeysComments)
	writeKeys(&b, "key_navbar", s.KeysNavbar)
	writeKeys(&b, "key_vol_up", s.KeysVolUp)
	writeKeys(&b, "key_vol_down", s.KeysVolDown)
	writeKeys(&b, "key_reel_size_inc", s.KeysReelSizeInc)
	writeKeys(&b, "key_reel_size_dec", s.KeysReelSizeDec)
	writeKeys(&b, "key_share", s.KeysShare)
	writeKeys(&b, "key_quit", s.KeysQuit)

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
func (b *ChromeBackend) ToggleNavbar() (bool, error) {
	settingsMu.Lock()
	Config.ShowNavbar = !Config.ShowNavbar
	showNavbar := Config.ShowNavbar
	snapshot := Config
	settingsMu.Unlock()

	path := filepath.Join(b.configDir, "reels.conf")
	go writeConf(path, snapshot)
	return showNavbar, nil
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

// Download downloads a reel video and profile picture to the cache directory
func (b *ChromeBackend) Download(index int) (string, string, error) {
	b.reelsMu.RLock()
	if index < 1 || index > len(b.orderedReels) {
		b.reelsMu.RUnlock()
		return "", "", fmt.Errorf("index out of range")
	}
	reel := b.orderedReels[index-1]
	b.reelsMu.RUnlock()

	if reel.VideoURL == "" {
		return "", "", fmt.Errorf("no video URL")
	}

	videoFile := filepath.Join(b.cacheDir, fmt.Sprintf("%03d_%s.mp4", index, reel.Code))
	pfpFile := filepath.Join(b.cacheDir, fmt.Sprintf("%03d_%s_pfp.jpg", index, reel.Code))

	// check cache to see if already downloaded
	cacheMu.Lock()
	if fifoMap[videoFile] {
		cacheMu.Unlock()
		return videoFile, pfpFile, nil
	}

	// check if in the progress of being downloaded
	if ch, ok := inProgress[videoFile]; ok {
		cacheMu.Unlock()
		<-ch // Wait for the other download to complete

		// re-check cache
		cacheMu.Lock()
		if fifoMap[videoFile] {
			cacheMu.Unlock()
			return videoFile, pfpFile, nil
		}
		// else fall through to download
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

	// Download video using chromedp fetch
	var videoData []byte
	err := chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			js := fmt.Sprintf(`
				(async () => {
					const r = await fetch("%s");
					const buf = await r.arrayBuffer();
					return Array.from(new Uint8Array(buf));
				})()
			`, reel.VideoURL)
			var arr []int
			if err := chromedp.Evaluate(js, &arr, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			}).Do(ctx); err != nil {
				return err
			}
			videoData = make([]byte, len(arr))
			for i, v := range arr {
				videoData[i] = byte(v)
			}
			return nil
		}),
	)
	if err != nil {
		return "", "", err
	}

	if err := os.WriteFile(videoFile, videoData, 0644); err != nil {
		return "", "", err
	}

	// Download profile picture using chromedp fetch
	if reel.ProfilePicUrl != "" {
		var pfpData []byte
		err := chromedp.Run(b.ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				js := fmt.Sprintf(`
					(async () => {
						const r = await fetch("%s");
						const buf = await r.arrayBuffer();
						return Array.from(new Uint8Array(buf));
					})()
				`, reel.ProfilePicUrl)
				var arr []int
				if err := chromedp.Evaluate(js, &arr, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
					return p.WithAwaitPromise(true)
				}).Do(ctx); err != nil {
					return err
				}
				pfpData = make([]byte, len(arr))
				for i, v := range arr {
					pfpData[i] = byte(v)
				}
				return nil
			}),
		)
		if err == nil {
			os.WriteFile(pfpFile, pfpData, 0644)
		}
		// Don't fail the whole download if profile pic fails
	}

	if err := add(videoFile); err != nil {
		return "", "", err
	}

	return videoFile, pfpFile, nil
}
