//go:build integration

package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var (
	repoRoot      string
	gocueBin      string
	harnessScript string
	testdataDir   string
	setupOnce     sync.Once
	setupErr      error
)

func TestMain(m *testing.M) {
	setupOnce.Do(func() { setupErr = prepareEnv() })
	if setupErr != nil {
		fmt.Fprintf(os.Stderr, "integration setup failed: %v\n", setupErr)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func prepareEnv() error {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("cannot locate integration package")
	}
	repoRoot = filepath.Clean(filepath.Join(filepath.Dir(thisFile), ".."))
	testdataDir = filepath.Join(repoRoot, "integration", "testdata")
	harnessScript = filepath.Join(repoRoot, "integration", "scripts", "harness.liq")

	for _, bin := range []string{"liquidsoap", "ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found on PATH (required for integration tests)", bin)
		}
	}

	outDir := filepath.Join(repoRoot, "integration", ".bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	gocueBin = filepath.Join(outDir, "gocue")
	build := exec.Command("go", "build", "-o", gocueBin, repoRoot)
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build gocue: %w\n%s", err, out)
	}
	return nil
}

// harnessOpts configures a Liquidsoap harness run.
type harnessOpts struct {
	File       string // relative to testdata/, or absolute
	Target     float64
	Silence    float64
	Overlay    float64
	Longtail   float64
	Extra      float64
	Drop       float64
	BlankSkip  float64
	FadeIn     float64
	FadeOut    float64
	NoClip     bool
	WriteTags  bool
	WriteRG    bool
	Annotate   string
	TimeoutSec float64
	WorkCopy   bool // copy fixture to temp so tag writes cannot mutate testdata
}

func (o harnessOpts) withDefaults() harnessOpts {
	if o.Target == 0 {
		o.Target = -18
	}
	if o.Silence == 0 {
		o.Silence = -42
	}
	if o.Overlay == 0 {
		o.Overlay = -8
	}
	if o.Longtail == 0 {
		o.Longtail = 15
	}
	if o.Extra == 0 {
		o.Extra = -12
	}
	if o.Drop == 0 {
		o.Drop = 40
	}
	if o.FadeIn == 0 {
		o.FadeIn = 0.1
	}
	if o.FadeOut == 0 {
		o.FadeOut = 2.5
	}
	if o.TimeoutSec == 0 {
		o.TimeoutSec = 60
	}
	return o
}

func resolveAudio(t *testing.T, opts harnessOpts) string {
	t.Helper()
	path := opts.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(testdataDir, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audio %s: %v", path, err)
	}
	if opts.WorkCopy || opts.WriteTags {
		tmp := filepath.Join(t.TempDir(), filepath.Base(path))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return tmp
	}
	return path
}

func runHarness(t *testing.T, opts harnessOpts) (meta map[string]string, audioPath string) {
	t.Helper()
	opts = opts.withDefaults()
	audioPath = resolveAudio(t, opts)

	cmd := exec.Command("liquidsoap", harnessScript, "--", audioPath)
	cmd.Dir = filepath.Dir(harnessScript)
	cmd.Env = append(os.Environ(),
		"GOCUE_BIN="+gocueBin,
		fmt.Sprintf("GOCUE_TARGET=%g", opts.Target),
		fmt.Sprintf("GOCUE_SILENCE=%g", opts.Silence),
		fmt.Sprintf("GOCUE_OVERLAY=%g", opts.Overlay),
		fmt.Sprintf("GOCUE_LONGTAIL=%g", opts.Longtail),
		fmt.Sprintf("GOCUE_EXTRA=%g", opts.Extra),
		fmt.Sprintf("GOCUE_DROP=%g", opts.Drop),
		fmt.Sprintf("GOCUE_BLANKSKIP=%g", opts.BlankSkip),
		fmt.Sprintf("GOCUE_FADE_IN=%g", opts.FadeIn),
		fmt.Sprintf("GOCUE_FADE_OUT=%g", opts.FadeOut),
		fmt.Sprintf("GOCUE_TIMEOUT=%g", opts.TimeoutSec),
		fmt.Sprintf("GOCUE_NOCLIP=%t", opts.NoClip),
		fmt.Sprintf("GOCUE_WRITE_TAGS=%t", opts.WriteTags),
		fmt.Sprintf("GOCUE_WRITE_REPLAYGAIN=%t", opts.WriteRG),
		"GOCUE_ANNOTATE="+opts.Annotate,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liquidsoap harness failed: %v\n%s", err, out)
	}
	meta, err = parseHarnessMeta(out)
	if err != nil {
		t.Fatalf("parse harness output: %v\n%s", err, out)
	}
	return meta, audioPath
}

func parseHarnessMeta(out []byte) (map[string]string, error) {
	all, err := parseHarnessMetas(out)
	if err != nil {
		return nil, err
	}
	if len(all) != 1 {
		return nil, fmt.Errorf("expected 1 meta block, got %d", len(all))
	}
	return all[0], nil
}

func parseHarnessMetas(out []byte) ([]map[string]string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	// Long free tracks can produce large stdout; raise token size just in case.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var all []map[string]string
	var meta map[string]string
	in := false
	for sc.Scan() {
		line := sc.Text()
		switch line {
		case "BEGIN_META":
			if in {
				return nil, fmt.Errorf("nested BEGIN_META")
			}
			in = true
			meta = make(map[string]string)
			continue
		case "END_META":
			if !in {
				return nil, fmt.Errorf("END_META without BEGIN_META")
			}
			in = false
			all = append(all, meta)
			meta = nil
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("bad meta line %q", line)
		}
		meta[k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if in {
		return nil, fmt.Errorf("unclosed BEGIN_META")
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("BEGIN_META/END_META markers missing from harness output")
	}
	return all, nil
}

func runGocueJSON(t *testing.T, file string, args ...string) map[string]any {
	t.Helper()
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(testdataDir, file)
	}
	cmdArgs := append(append([]string{}, args...), path)
	cmd := exec.Command(gocueBin, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gocue %v: %v\n%s", cmdArgs, err, stderr.String())
	}
	var m map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("gocue json: %v\n%s", err, stdout.String())
	}
	return m
}

func probeTags(t *testing.T, path string) map[string]string {
	t.Helper()
	cmd := exec.Command("ffprobe", "-v", "quiet",
		"-show_entries", "format_tags:stream_tags",
		"-of", "json=compact=1", path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var probed struct {
		Streams []struct {
			Tags map[string]string `json:"tags"`
		} `json:"streams"`
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probed); err != nil {
		t.Fatalf("ffprobe json: %v", err)
	}
	tags := make(map[string]string)
	for k, v := range probed.Format.Tags {
		tags[k] = v
	}
	for _, s := range probed.Streams {
		for k, v := range s.Tags {
			tags[k] = v
		}
	}
	return tags
}

func metaFloat(t *testing.T, meta map[string]string, key string) float64 {
	t.Helper()
	v, ok := meta[key]
	if !ok {
		t.Fatalf("missing metadata key %q", key)
	}
	fields := strings.Fields(v)
	if len(fields) == 0 {
		t.Fatalf("metadata key %q has no value", key)
	}
	f, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, v, err)
	}
	return f
}

// metaStartNext reconstructs the crossfade start point from the metadata that
// Liquidsoap's autocue reconcile actually emits. Like autocue.cue_file.liq, the
// script hands `start_next` to Liquidsoap through the autocue record rather than
// as a `liq_cross_start_next` tag, and reconcile turns it into
// liq_cross_end_duration (= fade_out + fade_out_delay = cue_out - start_next).
func metaStartNext(t *testing.T, meta map[string]string) float64 {
	t.Helper()
	return metaFloat(t, meta, "liq_cue_out") - metaFloat(t, meta, "liq_cross_end_duration")
}

func assertInDelta(t *testing.T, want, got, delta float64, msg string) {
	t.Helper()
	if d := want - got; d > delta || d < -delta {
		t.Fatalf("%s: want %g ± %g, got %g", msg, want, delta, got)
	}
}

func unitFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch x := v.(type) {
	case float64:
		return x
	case string:
		return metaFloat(t, map[string]string{"v": x}, "v")
	default:
		t.Fatalf("unexpected type %T", v)
		return 0
	}
}
