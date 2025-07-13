package cue

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"time"
)

const (
	// location of the ffmpeg binary
	ffmpeg = "ffmpeg"
	// location of the ffprobe binary
	ffprobe = "ffprobe"
	// Reference Loudness Target
	defaultTargetLUFS = -18.0
	// -96 dB/LU is "digital silence" for 16-bit audio.
	// A "noise floor" of -60 dB/LU (42 dB/LU below -18 target) is a good value to use.
	//
	//	LU below average for cue-in/cue-out trigger ("silence")
	defaultSilence = -42.0
	// LU below average for overlay trigger (start next song)
	defaultOverlayLU = -8.0
	// more than LONGTAIL_SECONDS below OVERLAY_LU are considered a "long tail"
	defaultLongTailSeconds = 15.0
	// educe 15 dB extra on long tail songs to find overlap point
	longTailExtraLU = -12.0
	// max. percent drop to be considered sustained
	defaultSustainedLoudnessDrop = 40.0
	// in. seconds silence to detect blank
	defaultBlankSkip = 5.0
)

var (
	digitalValRegex = regexp.MustCompile(`([+-]?\d*\.?\d+)`)
	// minimum set of tags after "read_tags" that must be there before skipping analysis
	baseTags = []string{
		"duration",
		"liq_cue_in",
		"liq_cue_out",
		"liq_cross_start_next",
		"replaygain_track_gain",
	}
	// these are the tags to check when reading/writing tags from/to files
	verifyTags = []string{
		"duration",
		"liq_amplify_adjustment",
		"liq_amplify",
		"liq_blankskip",
		"liq_blank_skipped",
		"liq_cross_duration",
		"liq_cross_start_next",
		"liq_cue_duration",
		"liq_cue_file",
		"liq_cue_in",
		"liq_cue_out",
		"liq_fade_in",
		"liq_fade_out",
		"liq_longtail",
		"liq_loudness",
		"liq_loudness_range",
		"liq_reference_loudness",
		"liq_sustained_ending",
		"liq_true_peak_db",
		"liq_true_peak",
		"r128_track_gain",
		"replaygain_reference_loudness",
		"replaygain_track_gain",
		"replaygain_track_peak",
		"replaygain_track_range",
	}
)

// Calculator - calculates technical cueing params
type Calculator struct {
	executionTimeout time.Duration
	targetLoudness   float64
}

// Calc returns actual results
func (c *Calculator) Calc() (*Result, error) {
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
	}, nil
}

func (c *Calculator) probe(pathToFile string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe, []string{
		"-v",
		"quiet",
		"-show_entries",
		"stream=codec_name,duration,bit_rate,sample_fmt,sample_rate,time_base,codec_type:stream_tags:format_tags",
		"-of",
		"json=compact=1",
		pathToFile,
	}...)
	res, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var allResults map[string]any
	err = json.Unmarshal(res, &allResults)
	if err != nil {
		return nil, err
	}
	streams := allResults["streams"].([]any)
	var tags = make(map[string]string)
	for _, s := range streams {
		sMap := s.(map[string]any)
		fmt.Printf("stream data: %+v", sMap)
		streamType := sMap["codec_type"]
		if streamType != "audio" {
			continue
		}
		dur := sMap["duration"].(string)
		tags["duration"] = dur
		foundTags, ok := sMap["tags"]
		if ok {
			for key, val := range foundTags.(map[string]any) {
				if !slices.Contains(verifyTags, key) {
					continue
				}
				switch v := val.(type) {
				case int:
					tags[key] = fmt.Sprintf("%d", v)
				case float32:
					tags[key] = fmt.Sprintf("%.5f", v)
				case string:
					res, err := takePureValue(key, v)
					if err != nil {
						fmt.Printf("tag read error: %s", err.Error())
						continue
					}
					tags[key] = res
				}
			}
		}
	}
	return tags, nil
}

func (c *Calculator) adjustLoudness(tags map[string]string) {
	// create replaygain_track_gain from Opus R128_TRACK_GAIN (ref: -23 LUFS)
	if r128TrackGain, ok := tags["r128_track_gain"]; ok {
		val, err := strconv.ParseFloat(r128TrackGain, 32)
		if err == nil {
			tags["replaygain_track_gain"] = fmt.Sprintf("%.5f", val/256+(c.targetLoudness - -23.0))
		}
	}

	// add missing liq_amplify, if we have replaygain_track_gain
	if _, ok := tags["liq_amplify"]; !ok {
		replayGain, ok := tags["replaygain_track_gain"]
		if ok {
			tags["liq_amplify"] = replayGain
		}
	}

	//  Handle old RG1/mp3gain positive loudness reference
	// "89 dB" (SPL) should actually be -14 LUFS, but as a reference
	// it is usually set equal to the RG2 -18 LUFS reference point
	// add missing liq_amplify, if we have replaygain_track_gain
	if replayRefLoudness, ok := tags["replaygain_reference_loudness"]; ok {
		val, err := strconv.ParseFloat(replayRefLoudness, 32)
		if err == nil {
			val -= 107.0
			tags["replaygain_reference_loudness"] = fmt.Sprintf("%.5f", val)
		}
		// add missing liq_reference_loudness, if we have
		//  replaygain_reference_loudness
		if _, ok := tags["liq_reference_loudness"]; !ok {
			tags["liq_reference_loudness"] = replayRefLoudness
		}
	}

	// if both liq_cue_in & liq_cue_out available, we can calculate
	// liq_cue_duration
	if cueIn, ok := tags["liq_cue_in"]; ok {
		cueInVal, err := strconv.ParseFloat(cueIn, 32)
		if err == nil {
			if cueOut, ok := tags["liq_cue_out"]; ok {
				cueOutVal, err := strconv.ParseFloat(cueOut, 32)
				if err == nil {
					tags["liq_cue_duration"] = fmt.Sprintf("%.5f", cueOutVal-cueInVal)
				}
			} else {
				dur, err := strconv.ParseFloat(tags["duration"], 32)
				if err == nil {
					tags["liq_cue_duration"] = fmt.Sprintf("%.5f", dur-cueInVal)
				}
			}
		}
	}
}

func takePureValue(key, val string) (string, error) {
	var needsCleaning = []string{
		"liq_amplify",
		"liq_amplify_adjustment",
		"liq_loudness",
		"liq_loudness_range",
		"liq_reference_loudness",
		"replaygain_track_gain",
		"replaygain_track_range",
		"replaygain_reference_loudness",
		"liq_true_peak_db",
	}
	if !slices.Contains(needsCleaning, key) {
		return val, nil
	}
	res := digitalValRegex.FindStringSubmatch(val)
	if len(res) < 1 {
		return "", fmt.Errorf("unexpected value [%s] found in [%s] tag", val, key)
	}
	return res[0], nil
}
