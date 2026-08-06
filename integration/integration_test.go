//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCuesAndFades validates cue-in/out, cross-start, and fade metadata for a
// synthetic tone with leading/trailing silence, through the real Liquidsoap
// autocue.gocue script.
func TestCuesAndFades(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:    "cues.flac",
		FadeIn:  0.1,
		FadeOut: 2.5,
	})

	if meta["liq_autocue"] != "gocue" {
		t.Fatalf("liq_autocue: got %q", meta["liq_autocue"])
	}

	cueIn := metaFloat(t, meta, "liq_cue_in")
	cueOut := metaFloat(t, meta, "liq_cue_out")
	startNext := metaStartNext(t, meta)
	fadeIn := metaFloat(t, meta, "liq_fade_in")
	fadeOut := metaFloat(t, meta, "liq_fade_out")
	duration := metaFloat(t, meta, "duration")

	assertInDelta(t, 0.8, cueIn, 0.15, "cue_in")
	assertInDelta(t, 5.1, cueOut, 0.2, "cue_out")
	assertInDelta(t, 5.0, startNext, 0.2, "cross_start_next")
	assertInDelta(t, 6.0, duration, 0.05, "duration")

	if fadeIn <= 0 || fadeIn > (cueOut-cueIn) {
		t.Fatalf("fade_in %g not in (0, cue_duration]", fadeIn)
	}
	// Liquidsoap shrinks fade_out to the available overlay window.
	if fadeOut <= 0 {
		t.Fatalf("fade_out must be positive, got %g", fadeOut)
	}
	if startNext+fadeOut > cueOut+0.05 {
		t.Fatalf("start_next (%g) + fade_out (%g) exceeds cue_out (%g)", startNext, fadeOut, cueOut)
	}
	if cueIn >= cueOut {
		t.Fatalf("cue_in (%g) >= cue_out (%g)", cueIn, cueOut)
	}

	direct := runGocueJSON(t, "cues.flac")
	assertInDelta(t, unitFloat(t, direct["liq_cue_in"]), cueIn, 0.05, "cue_in vs gocue CLI")
	assertInDelta(t, unitFloat(t, direct["liq_cross_start_next"]), startNext, 0.05, "cross_start_next vs gocue CLI")
}

// TestBlankSkipEarlyCueOut ensures blankskip via Liquidsoap settings produces
// an early cue-out on the hidden-track fixture.
func TestBlankSkipEarlyCueOut(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:      "blankgap.flac",
		BlankSkip: 3,
		FadeOut:   0.5,
	})

	cueOut := metaFloat(t, meta, "liq_cue_out")
	if meta["liq_blank_skipped"] != "true" {
		t.Fatalf("liq_blank_skipped: want true, got %q", meta["liq_blank_skipped"])
	}
	assertInDelta(t, 3.0, metaFloat(t, meta, "liq_blankskip"), 0.01, "liq_blankskip")
	if cueOut > 5.0 {
		t.Fatalf("expected early cue_out from blankskip, got %g", cueOut)
	}
	assertInDelta(t, 3.4, cueOut, 0.3, "blankskip cue_out")

	direct := runGocueJSON(t, "blankgap.flac", "-b", "3")
	if direct["liq_blank_skipped"] != true {
		t.Fatalf("gocue CLI blank_skipped=%v", direct["liq_blank_skipped"])
	}
}

// TestSustainedEndingAndLoudness checks sustained-ending flag and loudness
// fields for the long soft-fade fixture.
func TestSustainedEndingAndLoudness(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:    "longtail.flac",
		FadeOut: 1.0,
	})

	if meta["liq_sustained_ending"] != "true" {
		t.Fatalf("liq_sustained_ending: got %q", meta["liq_sustained_ending"])
	}
	for _, key := range []string{"liq_loudness", "liq_reference_loudness"} {
		if !strings.Contains(meta[key], "LUFS") {
			t.Fatalf("%s missing LUFS unit: %q", key, meta[key])
		}
	}
	if !strings.Contains(meta["liq_amplify"], "dB") {
		t.Fatalf("liq_amplify missing dB unit: %q", meta["liq_amplify"])
	}

	cueIn := metaFloat(t, meta, "liq_cue_in")
	cueOut := metaFloat(t, meta, "liq_cue_out")
	startNext := metaStartNext(t, meta)
	if cueIn != 0 {
		t.Fatalf("cue_in want 0, got %g", cueIn)
	}
	if startNext >= cueOut {
		t.Fatalf("cross_start_next %g should be before cue_out %g", startNext, cueOut)
	}
}

// TestSampleOggParity runs sample.ogg through Liquidsoap and compares core cue
// points with a direct gocue CLI run.
func TestSampleOggParity(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:    "sample.ogg",
		FadeOut: 2.5,
	})
	direct := runGocueJSON(t, "sample.ogg")

	assertInDelta(t, unitFloat(t, direct["liq_cue_in"]), metaFloat(t, meta, "liq_cue_in"), 0.05, "cue_in")
	assertInDelta(t, unitFloat(t, direct["liq_cue_out"]), metaFloat(t, meta, "liq_cue_out"), 0.15, "cue_out")
	assertInDelta(t, unitFloat(t, direct["liq_cross_start_next"]), metaStartNext(t, meta), 0.15, "cross_start_next")
	assertInDelta(t, unitFloat(t, direct["duration"]), metaFloat(t, meta, "duration"), 0.05, "duration")

	if meta["liq_autocue"] != "gocue" {
		t.Fatalf("liq_autocue=%q", meta["liq_autocue"])
	}
}

// TestTargetAndNoclipParams verifies Liquidsoap settings are forwarded to gocue.
func TestTargetAndNoclipParams(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:   "cues.flac",
		Target: -16,
		NoClip: true,
	})

	assertInDelta(t, -16.0, metaFloat(t, meta, "liq_reference_loudness"), 0.01, "reference loudness")

	direct := runGocueJSON(t, "cues.flac", "-t", "-16", "-k")
	assertInDelta(t, -16.0, unitFloat(t, direct["liq_reference_loudness"]), 0.01, "cli reference")
	assertInDelta(t, unitFloat(t, direct["liq_amplify"]), metaFloat(t, meta, "liq_amplify"), 0.05, "amplify parity")
}

// TestAnnotateFadeOverride checks annotate: metadata overrides fade_out.
func TestAnnotateFadeOverride(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:     "longtail.flac",
		FadeOut:  2.5,
		Annotate: `liq_fade_out="1.0"`,
	})
	fadeOut := metaFloat(t, meta, "liq_fade_out")
	if fadeOut > 1.05 {
		t.Fatalf("expected annotated fade_out≈1.0, got %g", fadeOut)
	}
}

// TestWriteTagsAndCacheRoundTrip writes liq_* tags via the Liquidsoap script
// (ffmpeg), validates them with ffprobe, then re-runs analysis on the tagged
// file for cue consistency.
func TestWriteTagsAndCacheRoundTrip(t *testing.T) {
	meta, path := runHarness(t, harnessOpts{
		File:      "cues.flac",
		WriteTags: true,
		WriteRG:   true,
		WorkCopy:  true,
	})

	tags := probeTags(t, path)
	required := []string{
		"liq_cue_in", "liq_cue_out", "liq_cross_start_next",
		"liq_loudness", "liq_amplify", "liq_reference_loudness",
		"liq_true_peak", "liq_true_peak_db", "liq_fade_in", "liq_fade_out",
		"replaygain_track_gain", "replaygain_reference_loudness",
	}
	for _, k := range required {
		if tags[k] == "" {
			t.Fatalf("written tag %q missing; got %#v", k, tags)
		}
	}

	assertInDelta(t, metaFloat(t, meta, "liq_cue_in"), metaFloat(t, tags, "liq_cue_in"), 0.01, "written cue_in")
	assertInDelta(t, metaFloat(t, meta, "liq_cue_out"), metaFloat(t, tags, "liq_cue_out"), 0.01, "written cue_out")
	assertInDelta(t, metaStartNext(t, meta), metaFloat(t, tags, "liq_cross_start_next"), 0.01, "written cross_start_next")

	meta2, _ := runHarness(t, harnessOpts{
		File:    path,
		FadeOut: 2.5,
	})
	assertInDelta(t, metaFloat(t, meta, "liq_cue_in"), metaFloat(t, meta2, "liq_cue_in"), 0.05, "cached cue_in")
	assertInDelta(t, metaFloat(t, meta, "liq_cue_out"), metaFloat(t, meta2, "liq_cue_out"), 0.15, "cached cue_out")
}

// TestSkipWhenDisabled confirms liq_cue_file=false skips analysis (no
// liq_autocue). autocue.cue_file.liq uses liq_cue_file as the only kill switch,
// so gocue.liq honours the same tag.
func TestSkipWhenDisabled(t *testing.T) {
	meta, _ := runHarness(t, harnessOpts{
		File:     "cues.flac",
		Annotate: `liq_cue_file="false"`,
	})
	if meta["liq_autocue"] != "" {
		t.Fatalf("expected no autocue metadata when disabled, got %#v", meta)
	}
}

// TestFreeTracksHarnessMetadata runs harness.liq over every audio file in
// testdata/free and validates the full autocue metadata set (presence, units,
// cue ordering, and parity with a direct gocue CLI analysis).
func TestFreeTracksHarnessMetadata(t *testing.T) {
	names := listFreeTracks(t)
	freeDir := filepath.Join(testdataDir, freeDirRel)

	cmd := exec.Command("liquidsoap", harnessScript, "--", freeDir)
	cmd.Dir = filepath.Dir(harnessScript)
	cmd.Env = append(os.Environ(),
		"GOCUE_BIN="+gocueBin,
		"GOCUE_TARGET=-18",
		"GOCUE_SILENCE=-42",
		"GOCUE_OVERLAY=-8",
		"GOCUE_LONGTAIL=15",
		"GOCUE_EXTRA=-12",
		"GOCUE_DROP=40",
		"GOCUE_BLANKSKIP=0",
		"GOCUE_FADE_IN=0.1",
		"GOCUE_FADE_OUT=2.5",
		"GOCUE_TIMEOUT=300",
		"GOCUE_NOCLIP=false",
		"GOCUE_WRITE_TAGS=false",
		"GOCUE_WRITE_REPLAYGAIN=false",
		"GOCUE_ANNOTATE=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liquidsoap harness (free dir) failed: %v\n%s", err, out)
	}
	blocks, err := parseHarnessMetas(out)
	if err != nil {
		t.Fatalf("parse harness output: %v\n%s", err, out)
	}
	if len(blocks) != len(names) {
		t.Fatalf("harness returned %d meta blocks, want %d files in free/", len(blocks), len(names))
	}

	byBase := make(map[string]map[string]string, len(blocks))
	for _, m := range blocks {
		file := m["file"]
		if file == "" {
			t.Fatalf("meta block missing file key: %#v", m)
		}
		byBase[filepath.Base(file)] = m
	}

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			meta, ok := byBase[name]
			if !ok {
				t.Fatalf("no harness metadata for %s", name)
			}
			path := freeTrack(t, name)
			direct := runGocueJSON(t, path)
			validateFreeTrackMetadata(t, meta, direct, probeDuration(t, path))
		})
	}
}

func validateFreeTrackMetadata(t *testing.T, meta map[string]string, direct map[string]any, probeDur float64) {
	t.Helper()

	required := []string{
		"liq_autocue",
		"liq_cue_in", "liq_cue_out",
		"liq_cross_end_duration", "liq_cross_max_start_duration",
		"liq_fade_in", "liq_fade_out", "liq_cue_duration",
		"liq_loudness", "liq_loudness_range", "liq_amplify", "liq_amplify_adjustment",
		"liq_reference_loudness", "liq_true_peak", "liq_true_peak_db",
		"liq_blankskip", "liq_blank_skipped", "liq_longtail", "liq_sustained_ending",
		"duration",
		"replaygain_track_gain", "replaygain_reference_loudness",
	}
	for _, k := range required {
		if meta[k] == "" {
			t.Fatalf("missing metadata %q", k)
		}
	}
	if meta["liq_autocue"] != "gocue" {
		t.Fatalf("liq_autocue=%q", meta["liq_autocue"])
	}

	for _, key := range []string{"liq_loudness", "liq_reference_loudness", "replaygain_reference_loudness"} {
		if !strings.Contains(meta[key], "LUFS") {
			t.Fatalf("%s missing LUFS unit: %q", key, meta[key])
		}
	}
	if !strings.Contains(meta["liq_loudness_range"], "LU") {
		t.Fatalf("liq_loudness_range missing LU unit: %q", meta["liq_loudness_range"])
	}
	for _, key := range []string{"liq_amplify", "liq_amplify_adjustment", "replaygain_track_gain"} {
		if !strings.Contains(meta[key], "dB") {
			t.Fatalf("%s missing dB unit: %q", key, meta[key])
		}
	}
	if !strings.Contains(meta["liq_true_peak_db"], "dBFS") {
		t.Fatalf("liq_true_peak_db missing dBFS unit: %q", meta["liq_true_peak_db"])
	}

	cueIn := metaFloat(t, meta, "liq_cue_in")
	cueOut := metaFloat(t, meta, "liq_cue_out")
	startNext := metaStartNext(t, meta)
	fadeIn := metaFloat(t, meta, "liq_fade_in")
	fadeOut := metaFloat(t, meta, "liq_fade_out")
	cueDur := metaFloat(t, meta, "liq_cue_duration")
	duration := metaFloat(t, meta, "duration")

	if cueIn < 0 {
		t.Fatalf("cue_in < 0: %g", cueIn)
	}
	if cueIn >= cueOut {
		t.Fatalf("cue_in (%g) >= cue_out (%g)", cueIn, cueOut)
	}
	if startNext < cueIn || startNext > cueOut+0.05 {
		t.Fatalf("cross_start_next %g not in [cue_in=%g, cue_out=%g]", startNext, cueIn, cueOut)
	}
	if fadeIn <= 0 || fadeIn > cueOut-cueIn+0.05 {
		t.Fatalf("fade_in %g out of range for cue window %g", fadeIn, cueOut-cueIn)
	}
	if fadeOut <= 0 {
		t.Fatalf("fade_out must be positive, got %g", fadeOut)
	}
	assertInDelta(t, cueOut-cueIn, cueDur, 0.05, "liq_cue_duration")
	assertInDelta(t, startNext+fadeOut, cueOut, 0.05, "cross_start_next+fade_out == cue_out")
	if cueOut > duration+0.25 {
		t.Fatalf("cue_out %g exceeds duration %g", cueOut, duration)
	}
	assertInDelta(t, probeDur, duration, 0.5, "duration vs ffprobe")

	assertInDelta(t, unitFloat(t, direct["liq_cue_in"]), cueIn, 0.05, "cue_in vs gocue CLI")
	assertInDelta(t, unitFloat(t, direct["duration"]), duration, 0.05, "duration vs gocue CLI")
	assertInDelta(t, unitFloat(t, direct["liq_cross_start_next"]), startNext, 0.15, "cross_start_next vs gocue CLI")
	assertInDelta(t, unitFloat(t, direct["liq_loudness"]), metaFloat(t, meta, "liq_loudness"), 0.05, "loudness vs gocue CLI")
	assertInDelta(t, -18.0, metaFloat(t, meta, "liq_reference_loudness"), 0.01, "reference loudness")

	cliCueOut := unitFloat(t, direct["liq_cue_out"])
	if cueOut > cliCueOut+0.25 {
		t.Fatalf("harness cue_out %g exceeds gocue CLI cue_out %g", cueOut, cliCueOut)
	}

	switch meta["liq_blank_skipped"] {
	case "true", "false":
	default:
		t.Fatalf("liq_blank_skipped=%q", meta["liq_blank_skipped"])
	}
	switch meta["liq_longtail"] {
	case "true", "false":
	default:
		t.Fatalf("liq_longtail=%q", meta["liq_longtail"])
	}
	switch meta["liq_sustained_ending"] {
	case "true", "false":
	default:
		t.Fatalf("liq_sustained_ending=%q", meta["liq_sustained_ending"])
	}
}
