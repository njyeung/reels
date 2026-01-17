package player

import (
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/asticode/go-astiav"
)

// VideoDecoder decodes video frames and scales to target size
type VideoDecoder struct {
	codecCtx *astiav.CodecContext
	swsCtx   *astiav.SoftwareScaleContext
	frame    *astiav.Frame
	swFrame  *astiav.Frame // For transferring hardware frames to software
	rgbFrame *astiav.Frame

	// Hardware acceleration
	hwDeviceCtx *astiav.HardwareDeviceContext
	hwPixFmt    astiav.PixelFormat
	useHardware bool

	srcWidth  int
	srcHeight int
	dstWidth  int
	dstHeight int

	timeBase astiav.Rational

	mu     sync.Mutex
	closed bool
}

// getPreferredHardwareTypes returns hardware device types to try, in order of preference
func getPreferredHardwareTypes() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"videotoolbox"}
	case "linux":
		return []string{"cuda", "vaapi", "vdpau"}
	case "windows":
		return []string{"d3d11va", "dxva2", "cuda"}
	default:
		return nil
	}
}

// tryInitHardware attempts to initialize hardware decoding for the given codec
// Returns the hardware device context and pixel format if successful, nil otherwise
func tryInitHardware(codec *astiav.Codec) (*astiav.HardwareDeviceContext, astiav.PixelFormat) {
	preferred := getPreferredHardwareTypes()
	if len(preferred) == 0 {
		return nil, 0
	}

	// Get hardware configs supported by this codec
	configs := codec.HardwareConfigs()
	for _, config := range configs {
		hwType := config.HardwareDeviceType()
		hwTypeName := hwType.String()

		// Check if this hardware type is in our preferred list
		if !slices.Contains(preferred, hwTypeName) {
			continue
		}

		// Try to create the hardware device context
		hwDeviceCtx, err := astiav.CreateHardwareDeviceContext(hwType, "", nil, 0)
		if err != nil {
			continue
		}

		// Success - return this hardware context
		return hwDeviceCtx, config.PixelFormat()
	}

	return nil, 0
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

	// Try to initialize hardware decoding
	hwDeviceCtx, hwPixFmt := tryInitHardware(codec)
	if hwDeviceCtx != nil {
		v.hwDeviceCtx = hwDeviceCtx
		v.hwPixFmt = hwPixFmt
		v.useHardware = true
	}

	// Allocate codec context
	v.codecCtx = astiav.AllocCodecContext(codec)
	if v.codecCtx == nil {
		v.Close()
		return nil, fmt.Errorf("failed to allocate video codec context")
	}

	// Copy parameters
	if err := codecParams.ToCodecContext(v.codecCtx); err != nil {
		v.Close()
		return nil, fmt.Errorf("failed to copy video codec params: %w", err)
	}

	// Set up hardware device context if available
	if v.useHardware {
		v.codecCtx.SetHardwareDeviceContext(v.hwDeviceCtx)
	}

	// Open codec
	if err := v.codecCtx.Open(codec, nil); err != nil {
		v.Close()
		return nil, fmt.Errorf("failed to open video codec: %w", err)
	}

	// Allocate frames
	v.frame = astiav.AllocFrame()
	v.rgbFrame = astiav.AllocFrame()
	if v.useHardware {
		v.swFrame = astiav.AllocFrame() // For GPU -> CPU transfer
	}

	return v, nil
}

// IsHardwareAccelerated returns true if hardware decoding is being used
func (v *VideoDecoder) IsHardwareAccelerated() bool {
	return v.useHardware
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

	return nil
}

func (v *VideoDecoder) initSwsContext(srcPixFmt astiav.PixelFormat) error {
	if v.dstWidth == 0 || v.dstHeight == 0 {
		return nil
	}

	// Create scaling context: source format -> RGB24 at target size
	var err error
	v.swsCtx, err = astiav.CreateSoftwareScaleContext(
		v.srcWidth, v.srcHeight, srcPixFmt,
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

	// Get the frame to scale - either directly or after GPU->CPU transfer
	frameToScale := v.frame

	if v.useHardware && v.frame.PixelFormat() == v.hwPixFmt {
		// Transfer frame from GPU to CPU
		if err := v.frame.TransferHardwareData(v.swFrame); err != nil {
			v.frame.Unref()
			return nil, fmt.Errorf("failed to transfer hardware frame: %w", err)
		}
		frameToScale = v.swFrame
	}

	// Initialize sws context if needed
	if v.swsCtx == nil {
		if err := v.initSwsContext(frameToScale.PixelFormat()); err != nil {
			v.frame.Unref()
			if v.swFrame != nil {
				v.swFrame.Unref()
			}
			return nil, err
		}
	}

	if err := v.swsCtx.ScaleFrame(frameToScale, v.rgbFrame); err != nil {
		v.frame.Unref()
		if v.swFrame != nil {
			v.swFrame.Unref()
		}
		return nil, fmt.Errorf("failed to scale frame: %w", err)
	}

	data := v.rgbFrame.Data()
	rgbBytes, err := data.Bytes(1)
	if err != nil {
		v.frame.Unref()
		if v.swFrame != nil {
			v.swFrame.Unref()
		}
		return nil, fmt.Errorf("failed to get RGB bytes: %w", err)
	}

	// Copy the data since the frame buffer will be reused
	rgb := make([]byte, len(rgbBytes))
	copy(rgb, rgbBytes)

	v.frame.Unref()
	if v.swFrame != nil {
		v.swFrame.Unref()
	}

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
	if v.swFrame != nil {
		v.swFrame.Free()
		v.swFrame = nil
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
	if v.hwDeviceCtx != nil {
		v.hwDeviceCtx.Free()
		v.hwDeviceCtx = nil
	}
}
