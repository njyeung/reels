package signature

import (
	"math"
)

const (
	// DefaultMaxTimeSeconds is the default maximum duration in seconds for a signature snippet (12s).
	DefaultMaxTimeSeconds = 12.0
	// DefaultMaxPeaks is the default target maximum peaks per signature.
	DefaultMaxPeaks = 255
	// SampleRateDefault is 16000 Hz.
	SampleRateDefault = 16000
	// RingBufferSizeSamples is 2048.
	RingBufferSizeSamples = 2048
	// RingBufferSizeFFT is 256.
	RingBufferSizeFFT = 256
)

var (
	neighborOffsets = [8]int{-10, -7, -4, -3, 1, 2, 5, 8}
	otherFFTOffsets = [14]int{
		-53, -45,
		165, 172, 179, 186, 193, 200,
		214, 221, 228, 235, 242, 249,
	}
)

// RingBufferFloat is a circular buffer of float64 slices (e.g. FFT outputs).
type RingBufferFloat struct {
	data       [][]float64
	position   int
	size       int
	numWritten int
}

// NewRingBufferFloat creates a new RingBufferFloat with given capacity and element size.
func NewRingBufferFloat(size int, elemSize int) *RingBufferFloat {
	data := make([][]float64, size)
	for i := range size {
		data[i] = make([]float64, elemSize)
	}
	return &RingBufferFloat{
		data:       data,
		position:   0,
		size:       size,
		numWritten: 0,
	}
}

// Append appends a slice of float64 to the ring buffer.
func (rb *RingBufferFloat) Append(val []float64) {
	copy(rb.data[rb.position], val)
	rb.position = (rb.position + 1) % rb.size
	rb.numWritten++
}

// GetRelative returns the element at (position + offset) % size.
func (rb *RingBufferFloat) GetRelative(offset int) []float64 {
	idx := ((rb.position+offset)%rb.size + rb.size) % rb.size
	return rb.data[idx]
}

// SignatureGenerator converts 16-bit 16 kHz mono PCM samples into Shazam signatures.
type SignatureGenerator struct {
	InputPendingProcessing []int16
	SamplesProcessed       int

	ringBufferSamples [RingBufferSizeSamples]float64
	ringBufferPos     int
	ringBufferWritten int

	fftOutputs       *RingBufferFloat
	spreadFFTOutput  *RingBufferFloat
	powerSpectrumBuf []float64
	excerptBuf       []float64

	MaxTimeSeconds float64
	MaxPeaks       int

	nextSignature *DecodedMessage
}

// NewSignatureGenerator creates a new initialized SignatureGenerator.
func NewSignatureGenerator() *SignatureGenerator {
	sg := &SignatureGenerator{
		MaxTimeSeconds:   DefaultMaxTimeSeconds,
		MaxPeaks:         DefaultMaxPeaks,
		fftOutputs:       NewRingBufferFloat(RingBufferSizeFFT, OutputBins),
		spreadFFTOutput:  NewRingBufferFloat(RingBufferSizeFFT, OutputBins),
		powerSpectrumBuf: make([]float64, OutputBins),
		excerptBuf:       make([]float64, RingBufferSizeSamples),
		nextSignature:    NewDecodedMessage(),
	}
	return sg
}

// FeedInput appends 16-bit 16 kHz mono PCM samples to be processed.
func (sg *SignatureGenerator) FeedInput(samples []int16) {
	sg.InputPendingProcessing = append(sg.InputPendingProcessing, samples...)
}

// Reset clears state for generating a new signature while retaining buffers.
func (sg *SignatureGenerator) Reset() {
	sg.ringBufferSamples = [RingBufferSizeSamples]float64{}
	sg.ringBufferPos = 0
	sg.ringBufferWritten = 0
	sg.fftOutputs = NewRingBufferFloat(RingBufferSizeFFT, OutputBins)
	sg.spreadFFTOutput = NewRingBufferFloat(RingBufferSizeFFT, OutputBins)
	sg.nextSignature = NewDecodedMessage()
}

// GetNextSignature consumes fed PCM samples and returns a Shazam DecodedMessage signature.
// Returns nil if insufficient samples remain.
func (sg *SignatureGenerator) GetNextSignature() *DecodedMessage {
	remaining := len(sg.InputPendingProcessing) - sg.SamplesProcessed
	if remaining < StrideSize {
		return nil
	}

	for remaining >= StrideSize {
		durSec := float64(sg.nextSignature.NumberSamples) / float64(sg.nextSignature.SampleRateHz)
		if durSec >= sg.MaxTimeSeconds || sg.nextSignature.TotalPeaks() >= sg.MaxPeaks {
			break
		}

		chunk := sg.InputPendingProcessing[sg.SamplesProcessed : sg.SamplesProcessed+StrideSize]
		sg.processChunk(chunk)
		sg.SamplesProcessed += StrideSize
		remaining = len(sg.InputPendingProcessing) - sg.SamplesProcessed
	}

	res := sg.nextSignature
	sg.Reset()
	return res
}

func (sg *SignatureGenerator) processChunk(chunk []int16) {
	sg.nextSignature.NumberSamples += len(chunk)

	// Write chunk to ring buffer of samples
	for _, sample := range chunk {
		sg.ringBufferSamples[sg.ringBufferPos] = float64(sample)
		sg.ringBufferPos = (sg.ringBufferPos + 1) % RingBufferSizeSamples
		sg.ringBufferWritten++
	}

	// Reconstruct chronological 2048-sample window: oldest to newest
	n1 := copy(sg.excerptBuf, sg.ringBufferSamples[sg.ringBufferPos:])
	copy(sg.excerptBuf[n1:], sg.ringBufferSamples[:sg.ringBufferPos])

	// Compute FFT and power spectrum
	ComputePowerSpectrum(sg.excerptBuf, sg.powerSpectrumBuf)
	sg.fftOutputs.Append(sg.powerSpectrumBuf)

	// Peak spreading
	sg.doPeakSpreading()

	// Peak recognition
	if sg.spreadFFTOutput.numWritten >= 46 {
		sg.doPeakRecognition()
	}
}

func (sg *SignatureGenerator) doPeakSpreading() {
	originLastFFT := sg.fftOutputs.GetRelative(-1)

	var spreadLastFFT [OutputBins]float64

	// Frequency-domain spreading across 3 adjacent bins: max(origin[i], origin[i+1], origin[i+2])
	for i := range OutputBins - 3 {
		v0 := originLastFFT[i]
		v1 := originLastFFT[i+1]
		v2 := originLastFFT[i+2]
		m := v0
		if v1 > m {
			m = v1
		}
		if v2 > m {
			m = v2
		}
		spreadLastFFT[i] = m
	}
	// Last 3 elements remain unchanged
	spreadLastFFT[OutputBins-3] = originLastFFT[OutputBins-3]
	spreadLastFFT[OutputBins-2] = originLastFFT[OutputBins-2]
	spreadLastFFT[OutputBins-1] = originLastFFT[OutputBins-1]

	// Time-domain spreading to previous FFT passes at offsets -1, -3, -6
	r1 := sg.spreadFFTOutput.GetRelative(-1)
	r2 := sg.spreadFFTOutput.GetRelative(-3)
	r3 := sg.spreadFFTOutput.GetRelative(-6)

	for k := range OutputBins {
		v0 := spreadLastFFT[k]
		if v0 > r1[k] {
			r1[k] = v0
		}
		if r1[k] > r2[k] {
			r2[k] = r1[k]
		}
		if r2[k] > r3[k] {
			r3[k] = r2[k]
		}
	}

	sg.spreadFFTOutput.Append(spreadLastFFT[:])
}

func (sg *SignatureGenerator) doPeakRecognition() {
	fftMinus46 := sg.fftOutputs.GetRelative(-46)
	fftMinus49 := sg.spreadFFTOutput.GetRelative(-49)

	const minPeakMagnitude = 1.0 / 64.0

	for binPosition := 10; binPosition < 1015; binPosition++ {
		curVal := fftMinus46[binPosition]
		if curVal < minPeakMagnitude || curVal < fftMinus49[binPosition-1] {
			continue
		}

		// Frequency-domain local minimum check
		maxNeighborInMinus49 := 0.0
		for _, offset := range neighborOffsets {
			val := fftMinus49[binPosition+offset]
			if val > maxNeighborInMinus49 {
				maxNeighborInMinus49 = val
			}
		}

		if curVal <= maxNeighborInMinus49 {
			continue
		}

		// Time-domain local minimum check across other adjacent FFT passes
		maxNeighborInOtherFFTs := maxNeighborInMinus49
		for _, otherOffset := range otherFFTOffsets {
			otherFFT := sg.spreadFFTOutput.GetRelative(otherOffset)
			val := otherFFT[binPosition-1]
			if val > maxNeighborInOtherFFTs {
				maxNeighborInOtherFFTs = val
			}
		}

		if curVal <= maxNeighborInOtherFFTs {
			continue
		}

		// Peak confirmed! Calculate interpolated peak parameters
		fftNumber := sg.spreadFFTOutput.numWritten - 46

		vMid := math.Max(minPeakMagnitude, curVal)
		vBefore := math.Max(minPeakMagnitude, fftMinus46[binPosition-1])
		vAfter := math.Max(minPeakMagnitude, fftMinus46[binPosition+1])

		peakMag := math.Log(vMid)*1477.3 + 6144.0
		peakMagBefore := math.Log(vBefore)*1477.3 + 6144.0
		peakMagAfter := math.Log(vAfter)*1477.3 + 6144.0

		peakVar1 := peakMag*2.0 - peakMagBefore - peakMagAfter
		var peakVar2 float64
		if peakVar1 > 0 {
			peakVar2 = (peakMagAfter - peakMagBefore) * 32.0 / peakVar1
		}

		correctedBin := float64(binPosition)*64.0 + peakVar2
		frequencyHz := correctedBin * (16000.0 / 2.0 / 1024.0 / 64.0)

		var band FrequencyBand
		switch {
		case frequencyHz > 250 && frequencyHz < 520:
			band = FrequencyBand250_520
		case frequencyHz >= 520 && frequencyHz < 1450:
			band = FrequencyBand520_1450
		case frequencyHz >= 1450 && frequencyHz < 3500:
			band = FrequencyBand1450_3500
		case frequencyHz >= 3500 && frequencyHz <= 5500:
			band = FrequencyBand3500_5500
		default:
			continue
		}

		sg.nextSignature.FrequencyBandToSoundPeaks[band] = append(
			sg.nextSignature.FrequencyBandToSoundPeaks[band],
			FrequencyPeak{
				FFTPassNumber:             fftNumber,
				PeakMagnitude:             int(peakMag),
				CorrectedPeakFrequencyBin: int(correctedBin),
				SampleRateHz:              SampleRateDefault,
			},
		)
	}
}
