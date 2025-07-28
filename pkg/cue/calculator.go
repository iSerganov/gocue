package cue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
	// Extract time "t", momentary (last 400ms) loudness "M" and "I" integrated loudness
	// from ebur128 filter. Measured every 100ms.
	// With some file types, like MP3, M can become "nan" (not-a-number),
	// which is a valid float in Python. Usually happens on very silent parts.
	// We convert these to float("-inf") for comparability in silence detection.
	// FIXME: This relies on "I" coming two lines after "M"
	// re              = regexp.MustCompile(`frame:.*pts_time:\s*(?P<t>\d+\.?\d*)\s*lavfi\.r128\.M=(?P<M>nan|[+-]?\d+\.?\d*)\s*.*\s*lavfi\.r128\.I=(?P<I>nan|[+-]?\d+\.?\d*)\s*(?P<rest>(\s*(?!frame:).*)*)`)
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
	blankSkip        float64
	noClip           bool
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
		val, err := strconv.ParseFloat(r128TrackGain, 64)
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
		val, err := strconv.ParseFloat(replayRefLoudness, 64)
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
		cueInVal, err := strconv.ParseFloat(cueIn, 64)
		if err == nil {
			if cueOut, ok := tags["liq_cue_out"]; ok {
				cueOutVal, err := strconv.ParseFloat(cueOut, 64)
				if err == nil {
					tags["liq_cue_duration"] = fmt.Sprintf("%.5f", cueOutVal-cueInVal)
				}
			} else {
				dur, err := strconv.ParseFloat(tags["duration"], 64)
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

// try to avoid re-analysis if we have enough data but different loudness target
func (c *Calculator) doPreAnalysis(tags map[string]string) error {
	for _, bt := range baseTags {
		if _, ok := tags[bt]; !ok {
			return ErrRequireAnalysis{inner: fmt.Errorf("tag '%s' is missing", bt)}
		}
	}

	liqAmplify, liqAmplifyOK := tags["liq_amplify"]
	if !liqAmplifyOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_amplify is missing")}
	}
	refLoudness, refLoudnessOK := tags["liq_reference_loudness"]
	if !refLoudnessOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_reference_loudness is missing")}
	}
	//adjust liq_amplify by loudness target difference, set reference
	amplifyVal, err := strconv.ParseFloat(liqAmplify, 64)
	if err == nil {
		loudnessVal, err := strconv.ParseFloat(refLoudness, 64)
		if err == nil {
			tags["liq_amplify"] = fmt.Sprintf("%.5f", amplifyVal+(c.targetLoudness-loudnessVal))
			tags["liq_reference_loudness"] = fmt.Sprintf("%.5f", c.targetLoudness)
		}
	}

	_, liqTruePeakOK := tags["liq_true_peak"]
	if !liqTruePeakOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_true_peak is missing")}
	}
	liqTruePeakDb, liqTruePeakDbOK := tags["liq_true_peak_db"]
	if !liqTruePeakDbOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_true_peak_db is missing")}
	}
	liqLoudness, liqLoudnessOK := tags["liq_loudness"]
	if !liqLoudnessOK {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_loudness is missing")}
	}
	liqTruePeakDbVal, err := strconv.ParseFloat(liqTruePeakDb, 64)
	if err != nil {
		return ErrRequireAnalysis{inner: fmt.Errorf("cannot parse liq_true_peak_db: %w", err)}
	}
	liqLoudnessVal, err := strconv.ParseFloat(liqLoudness, 64)
	if err != nil {
		return ErrRequireAnalysis{inner: fmt.Errorf("cannot parse liq_loudness: %w", err)}
	}
	liqAmplifyVal, liqAmplifyAdjVal := c.calcAmplify(liqLoudnessVal, liqTruePeakDbVal)
	tags["liq_amplify"] = fmt.Sprintf("%.5f", liqAmplifyVal)
	tags["liq_amplify_adjustment"] = fmt.Sprintf("%.5f", liqAmplifyAdjVal)

	// if liq_blankskip different from requested, we need a re-analysis
	liqBlankSkip, liqBlankSkipOK := tags["liq_blankskip"]
	if liqBlankSkipOK {
		liqBlankSkipVal, err := strconv.ParseFloat(liqBlankSkip, 64)
		if err == nil && liqBlankSkipVal != c.blankSkip {
			return ErrRequireAnalysis{inner: fmt.Errorf("liq_blankskip is different from the requested one")}
		}
	}

	// liq_loudness_range is only informational but we want to show correct values
	// can’t blindly take replaygain_track_range—it might be in different unit
	if _, ok := tags["liq_loudness_range"]; !ok {
		return ErrRequireAnalysis{inner: fmt.Errorf("tag liq_loudness_range is missing")}
	}
	return nil
}

func (c *Calculator) populate(tags map[string]string) {
	// we need not check those in tags_mandatory and those calculated by doPreAnalysis

	if _, ok := tags["liq_longtail"]; !ok {
		tags["liq_longtail"] = "false"
	}

	if _, ok := tags["liq_sustained_ending"]; !ok {
		tags["liq_sustained_ending"] = "false"
	}

	if _, ok := tags["liq_amplify"]; !ok {
		tags["liq_amplify"] = tags["replaygain_track_gain"]
	}

	if _, ok := tags["liq_amplify_adjustment"]; !ok {
		tags["liq_amplify_adjustment"] = "0.0" // dB
	}

	if _, ok := tags["liq_loudness"]; !ok {
		replayGain, _ := strconv.ParseFloat(tags["replaygain_track_gain"], 64)
		tags["liq_loudness"] = fmt.Sprintf("%.5f", c.targetLoudness-replayGain)
	}

	if _, ok := tags["liq_blankskip"]; !ok {
		tags["liq_blankskip"] = fmt.Sprintf("%.5f", c.blankSkip)
	}

	if _, ok := tags["liq_blank_skipped"]; !ok {
		tags["liq_blank_skipped"] = "false"
	}

	if _, ok := tags["liq_reference_loudness"]; !ok {
		tags["liq_reference_loudness"] = fmt.Sprintf("%.5f", c.targetLoudness)
	}

	// for RG tag writing
	if _, ok := tags["replaygain_track_gain"]; !ok {
		// ??? on line 319 we set liq_amplify from replaygain_track_gain - re-visit later!!!
		tags["replaygain_track_gain"] = tags["liq_amplify"]
	}
	if _, ok := tags["replaygain_track_peak"]; !ok {
		tags["replaygain_track_peak"] = tags["liq_true_peak"]
	}
	if _, ok := tags["replaygain_track_range"]; !ok {
		tags["replaygain_track_range"] = tags["liq_loudness_range"]
	}
	if _, ok := tags["replaygain_reference_loudness"]; !ok {
		tags["replaygain_reference_loudness"] = tags["liq_reference_loudness"]
	}
}

func (c *Calculator) calcAmplify(loudness, liqTruePeakDb float64) (amplify, amplifyCorrection float64) {
	// check if we need to reduce the gain for true peaks
	amplify = c.targetLoudness - loudness
	if c.noClip {
		maxAmplify := -1.0 - liqTruePeakDb // difference to EBU recommended -1 dBFS
		if amplify > maxAmplify {
			amplifyCorrection = maxAmplify - amplify
			amplify = maxAmplify
		}
	}
	return
}

func (c Calculator) scan(filename string) (*Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.executionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, []string{
		"-v",
		"info",
		"-nostdin",
		"-y",
		"-i",
		filename,
		"-vn",
		"-af",
		fmt.Sprintf("ebur128=target=%.5f:peak=true:metadata=1,ametadata=mode=print:file=-", c.targetLoudness),
		"-f",
		"null",
		"null",
	}...)
	filterOutput, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = filterOutput.Close()
	}()

	if err = cmd.Start(); err != nil {
		return nil, err
	}

	// Create a new scanner from the reader
	scanner := bufio.NewScanner(filterOutput)
	frames := []*Frame{}

	// Iterate over each line
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "frame:") {
			ptsStr := line[strings.Index(line, "pts_time:")+9:]
			pts, err := strconv.ParseFloat(ptsStr, 64)
			if err != nil {
				fmt.Printf("error reading pts_time: %s", err)
			}
			frames = append(frames, &Frame{PTSTime: pts})
			continue
		}
		if strings.HasPrefix(line, "lavfi.r128.M=") {
			loudnessStr := line[strings.Index(line, "M=")+2:]
			loudness, err := strconv.ParseFloat(loudnessStr, 64)
			if err != nil {
				fmt.Printf("error reading loudness: %s", err)
			}
			frames[len(frames)-1].Loudness = loudness
			continue
		}
		if strings.HasPrefix(line, "lavfi.r128.I=") {
			iLoudnessStr := line[strings.Index(line, "I=")+2:]
			iLoudness, err := strconv.ParseFloat(iLoudnessStr, 64)
			if err != nil {
				fmt.Printf("error reading integrated loudness: %s", err)
			}
			frames[len(frames)-1].IntegratedLoudness = iLoudness
			continue
		}
		if strings.HasPrefix(line, "lavfi.r128.true_peaks_ch") || strings.HasPrefix(line, "lavfi.r128.LRA=") {
			if frames[len(frames)-1].TPLRString != "" {
				frames[len(frames)-1].TPLRString += fmt.Sprintf(";%s", line)
			} else {
				frames[len(frames)-1].TPLRString = line
			}
			continue
		}
	}

	lastFrame := frames[len(frames)-1]

	strVals := strings.Split(lastFrame.TPLRString, ";")

	var (
		truePeak      float64
		loudnessRange float64
		truePeakDb    float64
	)
	for _, val := range strVals {
		if strings.HasPrefix(val, "lavfi.r128.true_peaks_ch") {
			truePeakVal, err := strconv.ParseFloat(val[strings.Index(val, "_ch")+5:], 64)
			if err == nil {
				truePeak = max(truePeak, truePeakVal)
			}
			continue
		}
		if strings.HasPrefix(val, "lavfi.r128.LRA=") {
			lra, err := strconv.ParseFloat(val[strings.Index(val, "LRA=")+4:], 64)
			if err == nil {
				loudnessRange = lra
			}
			continue
		}
	}

	if truePeak > 0 {
		truePeakDb = 20 * math.Log10(truePeak)
	} else {
		truePeakDb = math.Inf(-1)
	}

	duration := frames[len(frames)-1].PTSTime + 0.1
	return &Result{
		Duration:      duration,
		TruePeak:      truePeak,
		LoudnessRange: fmt.Sprintf("%.5f LU", loudnessRange),
		TruePeakDb:    fmt.Sprintf("%.5f dBFS", truePeakDb),
	}, nil
}
