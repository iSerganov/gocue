package cue

// Frame - ebur128 filter data for a frame
type Frame struct {
	PTSTime            float64
	Loudness           float64
	IntegratedLoudness float64
	TruePeak           float64
	LoudnessRange      float64
	TPLRString         string
}
