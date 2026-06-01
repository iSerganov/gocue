package cue

// Frame - per-frame ebur128 momentary loudness data.
// Only the fields used by every cue/overlay scan are kept here so the slice
// stays small and contiguous; "last frame only" values (integrated loudness and
// the true-peak/LRA line) are returned separately by parseFFmpegOutput.
type Frame struct {
	PTSTime  float64
	Loudness float64
}
