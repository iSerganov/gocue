package cue

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Result - CUE calculations data
type Result struct {
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

// Annotations - unmarshaled JSON as map of strings
func (r *Result) Annotations() (out map[string]string, err error) {
	// marshal annotations into bytes
	calcBytes, err := r.MarshalJSON()
	if err != nil {
		return nil, err
	}

	// convert bytes annotations into map
	anyMap := map[string]any{}
	err = json.Unmarshal(calcBytes, &anyMap)
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	for key, val := range anyMap {
		switch v := val.(type) {
		case string:
			res[key] = v
		case float64:
			res[key] = fmt.Sprintf("%.3f", v)
		case bool:
			res[key] = fmt.Sprintf("%t", v)
		}
	}
	return res, nil
}
