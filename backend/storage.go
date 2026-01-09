package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

var (
	cacheMu  sync.Mutex
	fifoList []string
	fifoMap  map[string]bool

	liked map[string]bool
)

func (b *ChromeBackend) initStorage() error {
	if CacheSize < 1 {
		return fmt.Errorf("cannot have a cache size < 1")
	}

	fifoMap = make(map[string]bool)
	liked = make(map[string]bool)

	// clear cache on startup
	if err := os.RemoveAll(b.cacheDir); err != nil {
		return fmt.Errorf("could not delete old cache directory")
	}
	if err := os.MkdirAll(b.cacheDir, 0755); err != nil {
		return fmt.Errorf("could not create new cache directory")
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

// Download downloads a reel video to the cache directory
func (b *ChromeBackend) Download(index int) (string, error) {
	b.mu.RLock()
	if index < 1 || index > len(b.orderedReels) {
		b.mu.RUnlock()
		return "", fmt.Errorf("index out of range")
	}
	reel := b.orderedReels[index-1]
	b.mu.RUnlock()

	if reel.VideoURL == "" {
		return "", fmt.Errorf("no video URL")
	}

	filename := filepath.Join(b.cacheDir, fmt.Sprintf("%03d_%s.mp4", index, reel.Code))

	// Check if already downloaded
	cacheMu.Lock()
	if fifoMap[filename] == true {
		cacheMu.Unlock()
		return filename, nil
	}
	cacheMu.Unlock()

	// Download using chromedp fetch
	var data []byte
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
			data = make([]byte, len(arr))
			for i, v := range arr {
				data[i] = byte(v)
			}
			return nil
		}),
	)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return "", err
	}

	if err := add(filename); err != nil {
		return "", err
	}

	return filename, nil
}
