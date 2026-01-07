package player

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
)

func init() {
	// Initialize speaker at startup to trigger audio permission prompts early
	// (better UX than prompting after user has logged in and expects playback)
	format := beep.Format{
		SampleRate: beep.SampleRate(AudioSampleRate),
	}
	speaker.Init(format.SampleRate, format.SampleRate.N(50*1000000)) // 50ms buffer
}

// AudioPlayer decodes and plays audio, providing the master clock
type AudioPlayer struct {
	codecCtx *astiav.CodecContext
	swrCtx   *astiav.SoftwareResampleContext
	frame    *astiav.Frame

	// Clock tracking
	clock   atomic.Value // float64
	clockMu sync.RWMutex
	playing atomic.Bool
	paused  atomic.Bool

	// Beep streamer
	streamer *audioStreamer
	ctrl     *beep.Ctrl

	// Sample buffer for decoded audio
	sampleBuf []byte
	buffMu    sync.Mutex

	closed bool
	mu     sync.Mutex
}

// audioStreamer implements beep.Streamer for our decoded audio
type audioStreamer struct {
	player *AudioPlayer
	buf    []byte
	pos    int
	format beep.Format
}

func (s *audioStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	s.player.buffMu.Lock()
	defer s.player.buffMu.Unlock()

	// when paused, fill buff with silence
	if s.player.paused.Load() {

		for i := range samples {
			samples[i][0] = 0
			samples[i][1] = 0
		}
		return len(samples), true
	}

	// sampleBuf (raw bytes from FFmpeg):
	// ┌────┬────┬────┬────┬────┬────┬────┬────┬─...
	// │ L0 │ L0 │ R0 │ R0 │ L1 │ L1 │ R1 │ R1 │
	// │ lo │ hi │ lo │ hi │ lo │ hi │ lo │ hi │
	// └────┴────┴────┴────┴────┴────┴────┴────┴─...
	// 	b0 	 b1   b2   b3
	//
	// (4 bytes = 1 stereo sample)
	bytesPerSample := 4 // (s16le stereo)
	samplesPlayed := 0

	for i := range samples {

		if len(s.player.sampleBuf) < bytesPerSample {
			// no more data, fill rest with silence but keep streaming
			for j := i; j < len(samples); j++ {
				samples[j][0] = 0
				samples[j][1] = 0
			}
			break
		}

		// Convert sampleBuf to float64 and normalize
		// to [-1, 1] that beep expects
		//
		// left low byte | left high byte << 8
		// 	  8 bits            8 bits
		const MAX_INT_16 = int16(32767)
		left := int16(s.player.sampleBuf[0]) | int16(s.player.sampleBuf[1])<<8
		right := int16(s.player.sampleBuf[2]) | int16(s.player.sampleBuf[3])<<8
		samples[i][0] = float64(left) / float64(MAX_INT_16)
		samples[i][1] = float64(right) / float64(MAX_INT_16)

		// consume
		s.player.sampleBuf = s.player.sampleBuf[bytesPerSample:]
		samplesPlayed++
	}

	// update clock based on actual samples played
	// time elapsed = samples played / audio sample rate
	if samplesPlayed > 0 {
		s.player.clock.Store(s.player.clock.Load().(float64) + float64(samplesPlayed)/float64(AudioSampleRate))
	}

	return len(samples), true
}

func (s *audioStreamer) Err() error {
	return nil
}

// NewAudioPlayer creates an audio player from codec parameters
func NewAudioPlayer(codecParams *astiav.CodecParameters) (*AudioPlayer, error) {
	a := &AudioPlayer{
		sampleBuf: make([]byte, 0, 192000), // ~1 second buffer
	}
	a.clock.Store(float64(0))

	// Find decoder
	codec := astiav.FindDecoder(codecParams.CodecID())
	if codec == nil {
		return nil, fmt.Errorf("audio codec not found: %s", codecParams.CodecID())
	}

	// Allocate codec context
	a.codecCtx = astiav.AllocCodecContext(codec)
	if a.codecCtx == nil {
		return nil, fmt.Errorf("failed to allocate audio codec context")
	}

	// Copy parameters
	if err := codecParams.ToCodecContext(a.codecCtx); err != nil {
		a.Close()
		return nil, fmt.Errorf("failed to copy audio codec params: %w", err)
	}

	// Open codec
	if err := a.codecCtx.Open(codec, nil); err != nil {
		a.Close()
		return nil, fmt.Errorf("failed to open audio codec: %w", err)
	}

	// Allocate decode frame
	a.frame = astiav.AllocFrame()

	// Setup resampler - we'll configure it on first frame
	a.swrCtx = astiav.AllocSoftwareResampleContext()
	if a.swrCtx == nil {
		a.Close()
		return nil, fmt.Errorf("failed to allocate swr context")
	}

	format := beep.Format{
		SampleRate: beep.SampleRate(AudioSampleRate),
	}

	// Create streamer
	a.streamer = &audioStreamer{
		player: a,
		format: format,
	}
	a.ctrl = &beep.Ctrl{Streamer: a.streamer}

	return a, nil
}

// Start begins audio playback
func (a *AudioPlayer) Start() {
	a.playing.Store(true)
	speaker.Play(a.ctrl)
}

// DecodePacket decodes an audio packet and queues samples for playback
func (a *AudioPlayer) DecodePacket(pkt *astiav.Packet, pts float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return fmt.Errorf("audio player closed")
	}

	// Send packet to decoder
	if err := a.codecCtx.SendPacket(pkt); err != nil {
		return fmt.Errorf("failed to send audio packet: %w", err)
	}

	// Receive decoded frames
	for {
		if err := a.codecCtx.ReceiveFrame(a.frame); err != nil {
			if err == astiav.ErrEof || err == astiav.ErrEagain {
				break
			}
			return fmt.Errorf("failed to receive audio frame: %w", err)
		}

		// Create output frame for resampled audio
		outFrame := astiav.AllocFrame()
		outFrame.SetSampleFormat(astiav.SampleFormatS16)
		outFrame.SetSampleRate(AudioSampleRate)
		outFrame.SetChannelLayout(astiav.ChannelLayoutStereo)
		outFrame.SetNbSamples(a.frame.NbSamples())

		// Allocate buffer for output frame
		if err := outFrame.AllocBuffer(0); err != nil {
			a.frame.Unref()
			outFrame.Free()
			continue
		}

		// Resample frame
		if err := a.swrCtx.ConvertFrame(a.frame, outFrame); err != nil {
			a.frame.Unref()
			outFrame.Free()
			// Skip frames that fail to resample instead of erroring
			continue
		}

		// Get resampled data - plane 0 for interleaved S16
		data := outFrame.Data()
		if data != nil {
			// Calculate actual byte size: samples * channels * bytes_per_sample
			numSamples := outFrame.NbSamples()
			byteSize := numSamples * 2 * 2 // 2 channels * 2 bytes per sample (S16)
			plane, err := data.Bytes(0)
			if err == nil && plane != nil && len(plane) >= byteSize {
				a.buffMu.Lock()
				a.sampleBuf = append(a.sampleBuf, plane[:byteSize]...)
				a.buffMu.Unlock()
			}
		}

		a.frame.Unref()
		outFrame.Free()
	}

	return nil
}

// Time returns the current playback time (master clock)
func (a *AudioPlayer) Time() float64 {
	return a.clock.Load().(float64)
}

// BufferSize returns the current size of the audio buffer in bytes
func (a *AudioPlayer) BufferSize() int {
	a.buffMu.Lock()
	defer a.buffMu.Unlock()
	return len(a.sampleBuf)
}

// IsPlaying returns true if audio is playing
func (a *AudioPlayer) IsPlaying() bool {
	return a.playing.Load() && !a.paused.Load()
}

// Pause toggles pause state
func (a *AudioPlayer) Pause() {
	a.paused.Store(!a.paused.Load())
}

// IsPaused returns current pause state
func (a *AudioPlayer) IsPaused() bool {
	return a.paused.Load()
}

// ResetClock resets the audio clock to zero
func (a *AudioPlayer) ResetClock() {
	a.clock.Store(float64(0))
}

// Close releases all resources
func (a *AudioPlayer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return
	}
	a.closed = true

	a.playing.Store(false)
	speaker.Clear()

	if a.frame != nil {
		a.frame.Free()
		a.frame = nil
	}
	if a.swrCtx != nil {
		a.swrCtx.Free()
		a.swrCtx = nil
	}
	if a.codecCtx != nil {
		a.codecCtx.Free()
		a.codecCtx = nil
	}
}
