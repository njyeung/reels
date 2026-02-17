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
	// Play starts playing from a cache URL (local), blocks until stopped or finished
	Play(url string) error

	// Stop stops current playback
	Stop()

	// Pause toggles pause state
	Pause()

	// IsPaused returns current pause state
	IsPaused() bool

	// IsMuted returns current mute state
	IsMuted() bool

	// Close releases all resources
	Close()

	// SetOutput sets the writer for video frames (terminal output)
	SetOutput(w io.Writer)

	// SetSize sets the video display dimensions in pixels
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
	// RenderImage renders image data at a cell position with a Kitty image ID
	RenderImage(data []byte, format, width, height, id, row, col int, sync bool) error

	// DeleteImage removes a specific Kitty image by ID
	DeleteImage(id int) error

	// ClearTerminal deletes all kitty images from the terminal
	ClearTerminal() error
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
	// SyncThreshold is the max drift before we skip/wait frames in video
	SyncThreshold = 0.1 // 100ms

	// AudioSampleRate for resampling
	AudioSampleRate = 44100

	// Kitty image IDs
	VideoImageID = 1
	PfpImageID   = 101
)
