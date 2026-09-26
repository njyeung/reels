package signature

// SampleRate represents Shazam supported audio sample rates in Hz.
type SampleRate int

const (
	SampleRate8000  SampleRate = 1
	SampleRate11025 SampleRate = 2
	SampleRate16000 SampleRate = 3
	SampleRate32000 SampleRate = 4
	SampleRate44100 SampleRate = 5
	SampleRate48000 SampleRate = 6
)

// Hz returns the sample rate in Hertz.
func (sr SampleRate) Hz() int {
	switch sr {
	case SampleRate8000:
		return 8000
	case SampleRate11025:
		return 11025
	case SampleRate16000:
		return 16000
	case SampleRate32000:
		return 32000
	case SampleRate44100:
		return 44100
	case SampleRate48000:
		return 48000
	default:
		return 16000
	}
}

// SampleRateFromHz returns the SampleRate enum corresponding to the given Hz.
func SampleRateFromHz(hz int) (SampleRate, bool) {
	switch hz {
	case 8000:
		return SampleRate8000, true
	case 11025:
		return SampleRate11025, true
	case 16000:
		return SampleRate16000, true
	case 32000:
		return SampleRate32000, true
	case 44100:
		return SampleRate44100, true
	case 48000:
		return SampleRate48000, true
	default:
		return SampleRate16000, false
	}
}

// FrequencyBand represents frequency ranges in Hz used in Shazam fingerprinting.
type FrequencyBand int

const (
	FrequencyBand0_250     FrequencyBand = -1 // 0 Hz - 250 Hz (not stored)
	FrequencyBand250_520   FrequencyBand = 0  // 250 Hz - 520 Hz
	FrequencyBand520_1450  FrequencyBand = 1  // 520 Hz - 1450 Hz
	FrequencyBand1450_3500 FrequencyBand = 2  // 1450 Hz - 3500 Hz
	FrequencyBand3500_5500 FrequencyBand = 3  // 3500 Hz - 5500 Hz
)
