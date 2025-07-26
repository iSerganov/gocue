package cue

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// Result - CUE calculations data
type Result struct {
	Duration              float32 `json:"duration" yaml:"duration"`
	CueDuration           float32 `json:"liq_cue_duration" yaml:"liq_cue_duration"`
	CueIn                 float32 `json:"liq_cue_in" yaml:"liq_cue_in"`
	CueOut                float32 `json:"liq_cue_out" yaml:"liq_cue_out"`
	CrossStartNext        float32 `json:"liq_cross_start_next" yaml:"liq_cross_start_next"`
	LongTail              bool    `json:"liq_longtail" yaml:"liq_longtail"`
	SustainedLoudnessDrop bool    `json:"liq_sustained_ending" yaml:"liq_sustained_ending"`
	Loudness              string  `json:"liq_loudness" yaml:"liq_loudness"`
	LoudnessRange         string  `json:"liq_loudness_range" yaml:"liq_loudness_range"`
	Amplify               string  `json:"liq_amplify" yaml:"liq_amplify"`
	AmplifyAdjustment     string  `json:"liq_amplify_adjustment" yaml:"liq_amplify_adjustment"`
	ReferenceLoudness     string  `json:"liq_reference_loudness" yaml:"liq_reference_loudness"`
	BlankSkip             float32 `json:"liq_blankskip" yaml:"liq_blankskip"`
	BlankSkipped          bool    `json:"liq_blank_skipped" yaml:"liq_blank_skipped"`
	TruePeak              float32 `json:"liq_true_peak" yaml:"liq_true_peak"`
	TruePeakDb            string  `json:"liq_true_peak_db" yaml:"liq_true_peak_db"`
}

// MarshalYAML - returns yaml
func (r *Result) MarshalYAML() (out []byte, err error) {
	return yaml.Marshal(*r)
}

// MarshalJSON - returns json
func (r *Result) MarshalJSON() (out []byte, err error) {
	return json.Marshal(*r)
}

// MarshalNiceJSON - returns pretty formatted json
func (r *Result) MarshalNiceJSON() (out []byte, err error) {
	return json.MarshalIndent(*r, " ", " ")
}
