package signature

import (
	"math"
	"math/cmplx"
)

const (
	// WindowSize is the FFT window size (2048 samples).
	WindowSize = 2048
	// StrideSize is the step size between FFT passes (128 samples).
	StrideSize = 128
	// OutputBins is the number of real FFT output bins (N/2 + 1 = 1025).
	OutputBins = 1025
	// MinPower is the floor value for power spectral density.
	MinPower = 1e-10
	// PowerScale is 1 << 17 (131072.0).
	PowerScale = 131072.0
)

var (
	// hanningTable is precomputed Hanning window of length 2048 (np.hanning(2050)[1:-1]).
	hanningTable [WindowSize]float64
	// bitRevTable is precomputed bit-reversal table for size 2048.
	bitRevTable [WindowSize]int
	// twiddleTable is precomputed twiddle factors for size 2048.
	twiddleTable [WindowSize / 2]complex128
)

func init() {
	// Initialize Hanning window: np.hanning(2050)[1:-1]
	// w[i] = 0.5 * (1 - cos(2 * pi * (i + 1) / 2049))
	for i := range WindowSize {
		hanningTable[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i+1)/2049.0))
	}

	// Initialize bit-reversal table for N = 2048 (11 bits)
	const bits = 11
	for i := range WindowSize {
		rev := 0
		n := i
		for range bits {
			rev = (rev << 1) | (n & 1)
			n >>= 1
		}
		bitRevTable[i] = rev
	}

	// Initialize twiddle factors: exp(-2 * pi * i * k / N)
	for k := range WindowSize / 2 {
		angle := -2.0 * math.Pi * float64(k) / float64(WindowSize)
		twiddleTable[k] = cmplx.Rect(1.0, angle)
	}
}

// HanningWindow returns the precomputed Hanning window table.
func HanningWindow() [WindowSize]float64 {
	return hanningTable
}

// ComputePowerSpectrum applies the Hanning window to 2048 samples and calculates
// the 1025 power spectral density bins scaled by 1/(1<<17) and floored at 1e-10.
func ComputePowerSpectrum(samples []float64, out []float64) {
	if len(samples) < WindowSize || len(out) < OutputBins {
		return
	}

	var buf [WindowSize]complex128

	// Windowing and bit-reversal permutation
	for i := range WindowSize {
		rev := bitRevTable[i]
		buf[rev] = complex(samples[i]*hanningTable[i], 0)
	}

	// In-place Cooley-Tukey Radix-2 FFT
	for size := 2; size <= WindowSize; size <<= 1 {
		halfSize := size / 2
		step := WindowSize / size

		for i := 0; i < WindowSize; i += size {
			k := 0
			for j := i; j < i+halfSize; j++ {
				u := buf[j]
				v := buf[j+halfSize] * twiddleTable[k]
				buf[j] = u + v
				buf[j+halfSize] = u - v
				k += step
			}
		}
	}

	// Extract power spectrum for bins 0..1024
	for k := range OutputBins {
		c := buf[k]
		power := (real(c)*real(c) + imag(c)*imag(c)) / PowerScale
		if power < MinPower {
			power = MinPower
		}
		out[k] = power
	}
}
