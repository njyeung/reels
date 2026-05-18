package backend

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// InitLogger configures the default slog logger to write to logDir/reels.log.
func InitLogger(logDir string) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(logDir, "reels.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		AddSource:   true,
		ReplaceAttr: shortenSource,
	})))
	return nil
}

// source=file:line:function_name
func shortenSource(groups []string, a slog.Attr) slog.Attr {
	if a.Key != slog.SourceKey {
		return a
	}
	src, ok := a.Value.Any().(*slog.Source)
	if !ok || src == nil {
		return a
	}
	file := src.File
	if idx := strings.LastIndex(file, "/reels/"); idx >= 0 {
		file = file[idx+len("/reels/"):]
	}
	fn := src.Function
	if idx := strings.LastIndex(fn, "."); idx >= 0 {
		fn = fn[idx+1:]
	}
	return slog.String(slog.SourceKey, fmt.Sprintf("%s:%d:%s", file, src.Line, fn))
}
