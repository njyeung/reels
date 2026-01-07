package player

import (
	"io"

	"github.com/asticode/go-astiav"
)

func init() {
	// Suppress FFmpeg log messages
	astiav.SetLogLevel(astiav.LogLevelQuiet)
}

// Player defines the interface for video playback
type Player interface {
	// Play starts playing from a URL, blocks until stopped or finished
	Play(url string) error

	// Stop stops current playback
	Stop()

	// Pause toggles pause state
	Pause()

	// IsPaused returns current pause state
	IsPaused() bool

	// Close releases all resources
	Close()

	// SetOutput sets the writer for video frames (terminal output)
	SetOutput(w io.Writer)

	// SetSize sets the video display dimensions in terminal cells
	SetSize(width, height int)
}

// Clock provides the audio clock for video sync
type Clock interface {
	// Time returns current playback time in seconds
	Time() float64

	// IsPlaying returns true if audio is actively playing
	IsPlaying() bool
}

// Renderer handles terminal graphics output
type Renderer interface {
	// RenderFrame renders an RGB frame to the terminal
	RenderFrame(rgb []byte, width, height int) error

	// Clear clears the video area
	Clear() error
}

// Frame represents a decoded video frame
type Frame struct {
	RGB      []byte  // RGB24 pixel data
	Width    int     // Frame width in pixels
	Height   int     // Frame height in pixels
	PTS      float64 // Presentation timestamp in seconds
	Duration float64 // Frame duration in seconds
}

// AudioSamples represents decoded audio data
type AudioSamples struct {
	Data       []byte  // PCM samples (s16le stereo)
	SampleRate int     // Sample rate (e.g., 44100)
	Channels   int     // Number of channels (e.g., 2)
	PTS        float64 // Presentation timestamp in seconds
}

const (
	// SyncThreshold is the max drift before we skip/wait frames
	SyncThreshold = 0.1 // 100ms

	// TargetFPS for video playback
	TargetFPS = 30

	// AudioSampleRate for resampling
	AudioSampleRate = 44100

	// AudioChannels (stereo)
	AudioChannels = 2
)
