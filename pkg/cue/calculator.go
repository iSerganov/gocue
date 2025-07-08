package cue

import "fmt"

// Calculator - calculates technical cueing params
type Calculator struct {
}

// Calc returns actual results
func (c *Calculator) Calc() *Result {
	return &Result{
		AmplifyAdjustment:     fmt.Sprintf("%.2f dB", 10.0),
		Amplify:               fmt.Sprintf("%.2f dB", 2.2),
		SustainedLoudnessDrop: false,
		BlankSkip:             5.0,
		BlankSkipped:          true,
		CrossStartNext:        4.0,
		CueDuration:           200.2,
		Duration:              401.2,
		CueIn:                 1.0,
		CueOut:                3.3,
		LongTail:              false,
		Loudness:              fmt.Sprintf("%.2f LUFS", -12.2),
		LoudnessRange:         fmt.Sprintf("%.2f LU", -9.9),
		ReferenceLoudness:     fmt.Sprintf("%.2f LUFS", -16.0),
		TruePeak:              74.11,
		TruePeakDb:            fmt.Sprintf("%.2f dBFS", -2.52),
	}
}
