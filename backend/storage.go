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
	ReelWidth   int
	ReelHeight  int
}

var Config Settings

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
		ShowNavbar:  true,
		RetinaScale: 1,
		ReelWidth:   270,
		ReelHeight:  480,
	}

	if goruntime.GOOS == "darwin" {
		s.RetinaScale = 2
	}
	return s
}

// LoadSettings loads reels.conf from configDir into Config. Loads default settings on error
func LoadSettings(configDir string) {
	s := defaultSettings()

	path := filepath.Join(configDir, "reels.conf")
	conf := parseConf(path)

	if v, ok := conf["show_navbar"]; ok {
		s.ShowNavbar = (v == "true")
	}
	if v, ok := conf["retina_scale"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			s.RetinaScale = n
		}
	}
	if v, ok := conf["reel_width"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			s.ReelWidth = n
		}
	}
	if v, ok := conf["reel_height"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			s.ReelHeight = n
		}
	}

	Config = s
}

func writeConf(path string, s Settings) error {
	var b strings.Builder
	b.WriteString("# insta reels TUI config\n\n")
	b.WriteString(fmt.Sprintf("show_navbar = %t\n", s.ShowNavbar))
	b.WriteString(fmt.Sprintf("retina_scale = %d\n", s.RetinaScale))
	b.WriteString("# reels will be scales within this bounding box")
	b.WriteString(fmt.Sprintf("reel_width = %d\n", s.ReelWidth))
	b.WriteString(fmt.Sprintf("reel_height = %d\n", s.ReelHeight))
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func parseConf(path string) map[string]string {
	result := make(map[string]string)
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
			result[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return result
}

func (b *ChromeBackend) ToggleNavbar() (bool, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	Config.ShowNavbar = !Config.ShowNavbar

	path := filepath.Join(b.configDir, "reels.conf")
	return Config.ShowNavbar, writeConf(path, Config)
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
