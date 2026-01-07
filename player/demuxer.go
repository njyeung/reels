package player

import (
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
)

// Demuxer handles opening media and reading packets
type Demuxer struct {
	formatCtx   *astiav.FormatContext
	videoStream *astiav.Stream
	audioStream *astiav.Stream
	videoIdx    int
	audioIdx    int

	// Time base for PTS conversion
	videoTimeBase astiav.Rational
	audioTimeBase astiav.Rational

	mu     sync.Mutex
	closed bool
}

// NewDemuxer creates a demuxer for the given URL
func NewDemuxer(url string) (*Demuxer, error) {
	d := &Demuxer{
		videoIdx: -1,
		audioIdx: -1,
	}

	// Allocate format context
	d.formatCtx = astiav.AllocFormatContext()
	if d.formatCtx == nil {
		return nil, fmt.Errorf("failed to allocate format context")
	}

	// Open input (url is filepath)
	if err := d.formatCtx.OpenInput(url, nil, nil); err != nil {
		d.formatCtx.Free()
		return nil, fmt.Errorf("failed to open input: %w", err)
	}

	// Find stream info
	if err := d.formatCtx.FindStreamInfo(nil); err != nil {
		d.Close()
		return nil, fmt.Errorf("failed to find stream info: %w", err)
	}

	// Find video and audio streams
	for _, stream := range d.formatCtx.Streams() {
		switch stream.CodecParameters().MediaType() {
		case astiav.MediaTypeVideo:
			if d.videoIdx == -1 {
				d.videoIdx = stream.Index()
				d.videoStream = stream
				d.videoTimeBase = stream.TimeBase()
			}
		case astiav.MediaTypeAudio:
			if d.audioIdx == -1 {
				d.audioIdx = stream.Index()
				d.audioStream = stream
				d.audioTimeBase = stream.TimeBase()
			}
		}
	}

	if d.videoIdx == -1 {
		d.Close()
		return nil, fmt.Errorf("no video stream found")
	}

	return d, nil
}

// VideoCodecParameters returns the video codec parameters
func (d *Demuxer) VideoCodecParameters() *astiav.CodecParameters {
	if d.videoStream == nil {
		return nil
	}
	return d.videoStream.CodecParameters()
}

// AudioCodecParameters returns the audio codec parameters
func (d *Demuxer) AudioCodecParameters() *astiav.CodecParameters {
	if d.audioStream == nil {
		return nil
	}
	return d.audioStream.CodecParameters()
}

// HasAudio returns true if there's an audio stream
func (d *Demuxer) HasAudio() bool {
	return d.audioIdx != -1
}

// VideoTimeBase returns the video stream time base
func (d *Demuxer) VideoTimeBase() astiav.Rational {
	return d.videoTimeBase
}

// AudioTimeBase returns the audio stream time base
func (d *Demuxer) AudioTimeBase() astiav.Rational {
	return d.audioTimeBase
}

// ReadPacket reads the next packet from the stream
// Returns the packet and whether it's a video packet (true) or audio packet (false)
// Returns nil, false, io.EOF when stream ends
func (d *Demuxer) ReadPacket() (*astiav.Packet, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, false, fmt.Errorf("demuxer closed")
	}

	pkt := astiav.AllocPacket()
	if pkt == nil {
		return nil, false, fmt.Errorf("failed to allocate packet")
	}

	if err := d.formatCtx.ReadFrame(pkt); err != nil {
		pkt.Free()
		return nil, false, err
	}

	isVideo := pkt.StreamIndex() == d.videoIdx
	return pkt, isVideo, nil
}

// PTSToSeconds converts a PTS value to seconds using the appropriate time base
func (d *Demuxer) PTSToSeconds(pts int64, isVideo bool) float64 {
	var tb astiav.Rational
	if isVideo {
		tb = d.videoTimeBase
	} else {
		tb = d.audioTimeBase
	}
	return float64(pts) * float64(tb.Num()) / float64(tb.Den())
}

// Close releases all resources
func (d *Demuxer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}
	d.closed = true

	if d.formatCtx != nil {
		d.formatCtx.CloseInput()
		d.formatCtx.Free()
		d.formatCtx = nil
	}
}
