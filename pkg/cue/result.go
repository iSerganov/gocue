package cue

import (
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Result - CUE calculations data.
//
// All measured quantities are stored as plain float64 numbers (in their natural
// units, noted per field) so callers can consume them arithmetically without
// re-parsing. The unit-suffixed presentation form used by the Liquidsoap
// "autocue:" protocol (e.g. "-18.000 LUFS") is produced only at the
// serialization boundary — see resultDTO / MarshalJSON — so the JSON and YAML
// output remains byte-for-byte identical to the upstream reference.
type Result struct {
	Duration          float64 // seconds
	CueDuration       float64 // seconds
	CueIn             float64 // seconds
	CueOut            float64 // seconds
	CrossStartNext    float64 // seconds
	LongTail          bool
	SustainedEnding   bool
	Loudness          float64 // LUFS
	LoudnessRange     float64 // LU
	Amplify           float64 // dB
	AmplifyAdjustment float64 // dB
	ReferenceLoudness float64 // LUFS
	BlankSkip         float64 // seconds
	BlankSkipped      bool
	TruePeak          float64 // linear
	TruePeakDb        float64 // dBFS
}

type resultDTO struct {
	Duration          float64 `json:"duration" yaml:"duration"`
	CueDuration       float64 `json:"liq_cue_duration" yaml:"liq_cue_duration"`
	CueIn             float64 `json:"liq_cue_in" yaml:"liq_cue_in"`
	CueOut            float64 `json:"liq_cue_out" yaml:"liq_cue_out"`
	CrossStartNext    float64 `json:"liq_cross_start_next" yaml:"liq_cross_start_next"`
	LongTail          bool    `json:"liq_longtail" yaml:"liq_longtail"`
	SustainedEnding   bool    `json:"liq_sustained_ending" yaml:"liq_sustained_ending"`
	Loudness          string  `json:"liq_loudness" yaml:"liq_loudness"`
	LoudnessRange     string  `json:"liq_loudness_range" yaml:"liq_loudness_range"`
	Amplify           string  `json:"liq_amplify" yaml:"liq_amplify"`
	AmplifyAdjustment string  `json:"liq_amplify_adjustment" yaml:"liq_amplify_adjustment"`
	ReferenceLoudness string  `json:"liq_reference_loudness" yaml:"liq_reference_loudness"`
	BlankSkip         float64 `json:"liq_blankskip" yaml:"liq_blankskip"`
	BlankSkipped      bool    `json:"liq_blank_skipped" yaml:"liq_blank_skipped"`
	TruePeak          float64 `json:"liq_true_peak" yaml:"liq_true_peak"`
	TruePeakDb        string  `json:"liq_true_peak_db" yaml:"liq_true_peak_db"`
}

// dto converts the numeric Result into its unit-suffixed wire form. This is the
// single place the presentation strings are built, so JSON, YAML and
// Annotations all stay in sync.
func (r *Result) dto() resultDTO {
	return resultDTO{
		Duration:          r.Duration,
		CueDuration:       r.CueDuration,
		CueIn:             r.CueIn,
		CueOut:            r.CueOut,
		CrossStartNext:    r.CrossStartNext,
		LongTail:          r.LongTail,
		SustainedEnding:   r.SustainedEnding,
		Loudness:          fmt.Sprintf("%.3f LUFS", r.Loudness),
		LoudnessRange:     fmt.Sprintf("%.3f LU", r.LoudnessRange),
		Amplify:           fmt.Sprintf("%.3f dB", r.Amplify),
		AmplifyAdjustment: fmt.Sprintf("%.3f dB", r.AmplifyAdjustment),
		ReferenceLoudness: fmt.Sprintf("%.3f LUFS", r.ReferenceLoudness),
		BlankSkip:         r.BlankSkip,
		BlankSkipped:      r.BlankSkipped,
		TruePeak:          r.TruePeak,
		TruePeakDb:        fmt.Sprintf("%.3f dBFS", r.TruePeakDb),
	}
}

// MarshalYAML - returns yaml
func (r *Result) MarshalYAML() (out []byte, err error) {
	return yaml.Marshal(r.dto())
}

// MarshalJSON - returns json
func (r *Result) MarshalJSON() (out []byte, err error) {
	return json.Marshal(r.dto())
}

// MarshalNiceJSON - returns pretty formatted json
func (r *Result) MarshalNiceJSON() (out []byte, err error) {
	return json.MarshalIndent(r.dto(), " ", " ")
}

// Annotations - result as a map of stringified values, keyed by the same tag
// names used for JSON. Numeric fields without a unit use 3-decimal precision and
// bools "true"/"false"; the loudness/gain fields carry their unit suffix, all
// matching the JSON output.
func (r *Result) Annotations() (map[string]string, error) {
	d := r.dto()
	return map[string]string{
		"duration":               fmt.Sprintf("%.3f", d.Duration),
		"liq_cue_duration":       fmt.Sprintf("%.3f", d.CueDuration),
		"liq_cue_in":             fmt.Sprintf("%.3f", d.CueIn),
		"liq_cue_out":            fmt.Sprintf("%.3f", d.CueOut),
		"liq_cross_start_next":   fmt.Sprintf("%.3f", d.CrossStartNext),
		"liq_longtail":           fmt.Sprintf("%t", d.LongTail),
		"liq_sustained_ending":   fmt.Sprintf("%t", d.SustainedEnding),
		"liq_loudness":           d.Loudness,
		"liq_loudness_range":     d.LoudnessRange,
		"liq_amplify":            d.Amplify,
		"liq_amplify_adjustment": d.AmplifyAdjustment,
		"liq_reference_loudness": d.ReferenceLoudness,
		"liq_blankskip":          fmt.Sprintf("%.3f", d.BlankSkip),
		"liq_blank_skipped":      fmt.Sprintf("%t", d.BlankSkipped),
		"liq_true_peak":          fmt.Sprintf("%.3f", d.TruePeak),
		"liq_true_peak_db":       d.TruePeakDb,
	}, nil
}

// parseTags builds a Result from existing file tags (the cached/fast path that
// skips a full ffmpeg analysis). Kept in sync with the scan() path so both
// produce identical output.
func parseTags(tags map[string]string) *Result {
	duration, _ := strconv.ParseFloat(tags["duration"], 64)
	cueDuration, _ := strconv.ParseFloat(tags["liq_cue_duration"], 64)
	cueIn, _ := strconv.ParseFloat(tags["liq_cue_in"], 64)
	cueOut, _ := strconv.ParseFloat(tags["liq_cue_out"], 64)
	crossStartNext, _ := strconv.ParseFloat(tags["liq_cross_start_next"], 64)
	longtail := tags["liq_longtail"] == "true"
	sustainedEnding := tags["liq_sustained_ending"] == "true"
	blankSkip, _ := strconv.ParseFloat(tags["liq_blankskip"], 64)
	blankSkipped := tags["liq_blank_skipped"] == "true"
	truePeak, _ := strconv.ParseFloat(tags["liq_true_peak"], 64)
	truePeakDb, _ := strconv.ParseFloat(tags["liq_true_peak_db"], 64)
	loudness, _ := strconv.ParseFloat(tags["liq_loudness"], 64)
	loudnessRange, _ := strconv.ParseFloat(tags["liq_loudness_range"], 64)
	amplify, _ := strconv.ParseFloat(tags["liq_amplify"], 64)
	amplifyCorrection, _ := strconv.ParseFloat(tags["liq_amplify_adjustment"], 64)
	referenceLoudness, _ := strconv.ParseFloat(tags["liq_reference_loudness"], 64)
	return &Result{
		Duration:          duration,
		CueDuration:       cueDuration,
		CueIn:             cueIn,
		CueOut:            cueOut,
		CrossStartNext:    crossStartNext,
		LongTail:          longtail,
		SustainedEnding:   sustainedEnding,
		Loudness:          loudness,
		LoudnessRange:     loudnessRange,
		Amplify:           amplify,
		AmplifyAdjustment: amplifyCorrection,
		ReferenceLoudness: referenceLoudness,
		BlankSkip:         blankSkip,
		BlankSkipped:      blankSkipped,
		TruePeak:          truePeak,
		TruePeakDb:        truePeakDb,
	}
}
