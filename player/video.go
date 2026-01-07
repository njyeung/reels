package player

import (
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
)

// VideoDecoder decodes video frames and scales to target size
type VideoDecoder struct {
	codecCtx *astiav.CodecContext
	swsCtx   *astiav.SoftwareScaleContext
	frame    *astiav.Frame
	rgbFrame *astiav.Frame

	srcWidth  int
	srcHeight int
	dstWidth  int
	dstHeight int

	timeBase astiav.Rational

	mu     sync.Mutex
	closed bool
}

// NewVideoDecoder creates a video decoder from codec parameters
func NewVideoDecoder(codecParams *astiav.CodecParameters, timeBase astiav.Rational) (*VideoDecoder, error) {
	v := &VideoDecoder{
		timeBase:  timeBase,
		srcWidth:  codecParams.Width(),
		srcHeight: codecParams.Height(),
		dstWidth:  codecParams.Width(),
		dstHeight: codecParams.Height(),
	}

	// Find decoder
	codec := astiav.FindDecoder(codecParams.CodecID())
	if codec == nil {
		return nil, fmt.Errorf("video codec not found: %s", codecParams.CodecID())
	}

	// Allocate codec context
	v.codecCtx = astiav.AllocCodecContext(codec)
	if v.codecCtx == nil {
		return nil, fmt.Errorf("failed to allocate video codec context")
	}

	// Copy parameters
	if err := codecParams.ToCodecContext(v.codecCtx); err != nil {
		v.Close()
		return nil, fmt.Errorf("failed to copy video codec params: %w", err)
	}

	// Open codec
	if err := v.codecCtx.Open(codec, nil); err != nil {
		v.Close()
		return nil, fmt.Errorf("failed to open video codec: %w", err)
	}

	// Allocate frames
	v.frame = astiav.AllocFrame()
	v.rgbFrame = astiav.AllocFrame()

	return v, nil
}

// SetSize sets the output dimensions for scaling
func (v *VideoDecoder) SetSize(width, height int) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if width == v.dstWidth && height == v.dstHeight {
		return nil
	}

	v.dstWidth = width
	v.dstHeight = height

	// Recreate sws context with new dimensions
	if v.swsCtx != nil {
		v.swsCtx.Free()
		v.swsCtx = nil
	}

	return v.initSwsContext()
}

func (v *VideoDecoder) initSwsContext() error {
	if v.dstWidth == 0 || v.dstHeight == 0 {
		return nil
	}

	// Create scaling context: source format -> RGB24 at target size
	var err error
	v.swsCtx, err = astiav.CreateSoftwareScaleContext(
		v.srcWidth, v.srcHeight, v.codecCtx.PixelFormat(),
		v.dstWidth, v.dstHeight, astiav.PixelFormatRgb24,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
	)
	if err != nil {
		return fmt.Errorf("failed to create sws context: %w", err)
	}

	// Setup RGB frame
	v.rgbFrame.SetWidth(v.dstWidth)
	v.rgbFrame.SetHeight(v.dstHeight)
	v.rgbFrame.SetPixelFormat(astiav.PixelFormatRgb24)

	if err := v.rgbFrame.AllocBuffer(1); err != nil {
		return fmt.Errorf("failed to allocate RGB frame buffer: %w", err)
	}

	return nil
}

// DecodePacket decodes a video packet and returns an RGB frame
func (v *VideoDecoder) DecodePacket(pkt *astiav.Packet) (*Frame, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil, fmt.Errorf("video decoder closed")
	}

	// Send packet to decoder
	if err := v.codecCtx.SendPacket(pkt); err != nil {
		return nil, fmt.Errorf("failed to send video packet: %w", err)
	}

	// Receive decoded frame
	if err := v.codecCtx.ReceiveFrame(v.frame); err != nil {
		if err == astiav.ErrEof || err == astiav.ErrEagain {
			return nil, nil // No frame available yet
		}
		return nil, fmt.Errorf("failed to receive video frame: %w", err)
	}

	// Calculate PTS in seconds
	pts := float64(v.frame.Pts()) * float64(v.timeBase.Num()) / float64(v.timeBase.Den())

	// Calculate duration from packet duration
	var duration = float64(pkt.Duration()) * float64(v.timeBase.Num()) / float64(v.timeBase.Den())

	// Initialize sws context if needed
	if v.swsCtx == nil {
		if err := v.initSwsContext(); err != nil {
			v.frame.Unref()
			return nil, err
		}
	}

	if err := v.swsCtx.ScaleFrame(v.frame, v.rgbFrame); err != nil {
		v.frame.Unref()
		return nil, fmt.Errorf("failed to scale frame: %w", err)
	}

	data := v.rgbFrame.Data()
	rgbBytes, err := data.Bytes(1)
	if err != nil {
		v.frame.Unref()
		return nil, fmt.Errorf("failed to get RGB bytes: %w", err)
	}

	// Copy the data since the frame buffer will be reused
	rgb := make([]byte, len(rgbBytes))
	copy(rgb, rgbBytes)

	v.frame.Unref()

	return &Frame{
		RGB:      rgb,
		Width:    v.dstWidth,
		Height:   v.dstHeight,
		PTS:      pts,
		Duration: duration,
	}, nil
}

// SourceSize returns the original video dimensions
func (v *VideoDecoder) SourceSize() (int, int) {
	return v.srcWidth, v.srcHeight
}

// Close releases all resources
func (v *VideoDecoder) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return
	}
	v.closed = true

	if v.frame != nil {
		v.frame.Free()
		v.frame = nil
	}
	if v.rgbFrame != nil {
		v.rgbFrame.Free()
		v.rgbFrame = nil
	}
	if v.swsCtx != nil {
		v.swsCtx.Free()
		v.swsCtx = nil
	}
	if v.codecCtx != nil {
		v.codecCtx.Free()
		v.codecCtx = nil
	}
}
