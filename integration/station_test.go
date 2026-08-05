//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const freeDirRel = "free"

func freeAudioExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".flac", ".ogg", ".mp3", ".wav", ".m4a", ".opus":
		return true
	default:
		return false
	}
}

func listFreeTracks(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(testdataDir, freeDirRel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read free dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !freeAudioExt(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		t.Fatal("no audio files in integration/testdata/free")
	}
	sort.Strings(names)
	return names
}

func freeTrack(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(testdataDir, freeDirRel, name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("free track %s missing", name)
	}
	// sanity: > 4 minutes
	out, err := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "format=duration",
		"-of", "default=nk=1:nw=1", p).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", name, err)
	}
	var dur float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	if dur < 240 {
		t.Fatalf("%s duration %g < 4 minutes", name, dur)
	}
	return p
}

func probeDuration(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "format=duration",
		"-of", "default=nk=1:nw=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe duration %s: %v", path, err)
	}
	var dur float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	return dur
}

type stationMeta struct {
	Track                    int    `json:"track"`
	Artist                   string `json:"artist"`
	Title                    string `json:"title"`
	URI                      string `json:"uri"`
	LiqAmplify               string `json:"liq_amplify"`
	LiqLoudness              string `json:"liq_loudness"`
	LiqCueIn                 string `json:"liq_cue_in"`
	LiqCueOut                string `json:"liq_cue_out"`
	LiqFadeIn                string `json:"liq_fade_in"`
	LiqFadeOut               string `json:"liq_fade_out"`
	LiqCrossStartNext        string `json:"liq_cross_start_next"`
	LiqCrossMaxStartDuration string `json:"liq_cross_max_start_duration"`
	LiqAutocue               string `json:"liq_autocue"`
}

// TestStationPlaylistPlayback boots a minimal radio station, plays a playlist of long PD classical
// 1920s jazz & blues tracks with short annotated cue windows, and verifies
// track order + autocue metadata.
func TestStationPlaylistPlayback(t *testing.T) {
	tracks := []string{
		freeTrack(t, "bach_brandenburg_2.ogg"),
		freeTrack(t, "gershwin_rhapsody_in_blue_1924.ogg"),
		freeTrack(t, "virginia_blues_1922.mp3"),
	}
	// Short cue windows so we advance without waiting full track lengths.
	// Request annotations override gocue cue points after analysis
	const cueOut = 18.0
	const cross = 15.0
	const fadeOut = 2.5

	work := t.TempDir()
	playlistPath := filepath.Join(work, "station.m3u")
	var b strings.Builder
	// Plain URI lines (no #EXTINF). Liquidsoap m3u parsing is happier this way.
	for _, tr := range tracks {
		b.WriteString(fmt.Sprintf(
			"annotate:liq_cue_in=\"0\",liq_cue_out=\"%g\",liq_cross_start_next=\"%g\",liq_fade_in=\"0.1\",liq_fade_out=\"%g\":%s\n",
			cueOut, cross, fadeOut, tr,
		))
	}
	if err := os.WriteFile(playlistPath, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	port := 19000 + (os.Getpid() % 1000)
	outWav := filepath.Join(work, "out.wav")
	stationScript := filepath.Join(repoRoot, "integration", "scripts", "station.liq")

	cmd := exec.Command("liquidsoap", stationScript)
	cmd.Dir = filepath.Dir(stationScript)
	cmd.Env = append(os.Environ(),
		"GOCUE_BIN="+gocueBin,
		"STATION_PLAYLIST="+playlistPath,
		fmt.Sprintf("STATION_HARBOR_PORT=%d", port),
		"STATION_OUTPUT="+outWav,
		"GOCUE_TIMEOUT=180",
		"GOCUE_FADE_IN=0.1",
		"GOCUE_FADE_OUT=2.5",
		"GOCUE_NOCLIP=true",
	)
	logPath := filepath.Join(work, "station.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start station: %v", err)
	}
	defer func() {
		_, _ = http.Post(fmt.Sprintf("http://127.0.0.1:%d/shutdown", port), "", nil)
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
		_ = logFile.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("station log:\n%s", data)
		}
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitHTTP(t, base+"/health", 3*time.Minute)

	seen := make([]stationMeta, 0, len(tracks))
	deadline := time.Now().Add(10 * time.Minute)
	lastTrack := 0

	for len(seen) < len(tracks) && time.Now().Before(deadline) {
		m, err := fetchStationMeta(base + "/metadata")
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if m.Track > lastTrack && m.LiqAutocue != "" {
			seen = append(seen, m)
			lastTrack = m.Track
			t.Logf("track %d: artist=%q title=%q cue_out=%s autocue=%s uri=%q",
				m.Track, m.Artist, m.Title, m.LiqCueOut, m.LiqAutocue, m.URI)

			if m.LiqAutocue != "gocue" {
				t.Fatalf("expected liq_autocue=gocue, got %q", m.LiqAutocue)
			}
			if m.LiqAmplify == "" || m.LiqLoudness == "" {
				t.Fatalf("missing loudness metadata: %+v", m)
			}
			if m.LiqCueIn == "" || m.LiqCueOut == "" || m.LiqFadeOut == "" {
				t.Fatalf("missing cue/fade metadata: %+v", m)
			}
			// Liquidsoap may shrink cue_out to cross_start_next + fade_out.
			gotCueOut := metaFloat(t, map[string]string{"v": m.LiqCueOut}, "v")
			wantCueOut := cross + fadeOut // 15 + 2.5
			if gotCueOut > cueOut+0.5 || gotCueOut < wantCueOut-0.5 {
				t.Fatalf("cue_out=%g; expected ~%g (annotate) or ~%g (cross+fade)", gotCueOut, cueOut, wantCueOut)
			}

			if len(seen) < len(tracks) {
				resp, err := http.Get(base + "/skip")
				if err != nil {
					t.Fatalf("skip: %v", err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("skip status %d", resp.StatusCode)
				}
				_ = resp.Body.Close()
				// Next track may need a full gocue analysis (~tens of seconds).
				time.Sleep(2 * time.Second)
			}
		} else {
			time.Sleep(500 * time.Millisecond)
		}
	}

	if len(seen) < len(tracks) {
		data, _ := os.ReadFile(logPath)
		t.Fatalf("only saw %d/%d tracks before timeout\nlog:\n%s", len(seen), len(tracks), data)
	}

	// Confirm playlist order by filename fragment in uri.
	wantFrags := []string{"bach_brandenburg_2", "gershwin_rhapsody_in_blue_1924", "virginia_blues_1922"}
	for i, frag := range wantFrags {
		if !strings.Contains(strings.ToLower(seen[i].URI), frag) {
			t.Errorf("track %d order: want uri containing %q, got uri=%q title=%q",
				i+1, frag, seen[i].URI, seen[i].Title)
		}
	}

	// Output file should have received audio (non-trivial size).
	fi, err := os.Stat(outWav)
	if err != nil {
		t.Fatalf("station output missing: %v", err)
	}
	if fi.Size() < 10_000 {
		t.Fatalf("station output too small: %d bytes", fi.Size())
	}
}

// TestFreeTracksLongEnough guards that committed PD fixtures exceed 4 minutes.
func TestFreeTracksLongEnough(t *testing.T) {
	for _, name := range listFreeTracks(t) {
		freeTrack(t, name)
	}
}

func waitHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s: %v", url, lastErr)
}

func fetchStationMeta(url string) (stationMeta, error) {
	var m stationMeta
	resp, err := http.Get(url)
	if err != nil {
		return m, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return m, fmt.Errorf("status %d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&m)
	return m, err
}
