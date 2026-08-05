# gocue

[![CI](https://github.com/iSerganov/gocue/actions/workflows/ci.yml/badge.svg)](https://github.com/iSerganov/gocue/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/iSerganov/gocue/badges/coverage.json)](https://github.com/iSerganov/gocue/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.26+-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-1.1.1-blue.svg)](Makefile)

**gocue** is a Go audio analysis tool for professional playout workflows. It detects cue-in, cue-out, and overlay points and measures EBU R128 loudness, then prints JSON on stdout for Liquidsoap’s `autocue:` protocol.

gocue is a stand-alone analyzer: it **reads** existing `liq_*` / ReplayGain tags when they are complete (to skip a full ffmpeg scan) and **does not write** tags back to files. Tag write-back, fades, and playlist wiring belong in a Liquidsoap `.liq` script. You can start from [autocue.gocue.liq](https://github.com/iSerganov/autocue/blob/master/autocue.gocue.liq) (adapted from [Moonbase59’s autocue](https://github.com/Moonbase59/autocue/blob/master/autocue.cue_file.liq)). See the [Liquidsoap autocue docs](https://www.liquidsoap.info/doc-dev/settings.html#all-available-autocue-implementations) for details.

For algorithm background, see [Moonbase59’s autocue presentation](https://moonbase59.github.io/autocue/presentation/autocue.html).

## Features

- **Audio analysis** — cue-in / cue-out and crossfade overlay points
- **EBU R128** — integrated loudness, loudness range, and true peak via ffmpeg
- **Tag cache (read-only)** — reuses existing file tags when complete; recomputes amplify for a new target LUFS without re-decoding
- **Formats** — WAV, OGG, MP3, FLAC, M4A, WMA, ASF, AIFF, and more (anything ffmpeg/ffprobe support)
- **Liquidsoap-oriented JSON** — unit-suffixed loudness/gain fields matching the autocue protocol
- **Configurable thresholds** — silence, overlay, longtail, blankskip, clipping prevention

## Quick Start

### Prerequisites

- Go 1.26 or higher (to build from source)
- FFmpeg and FFprobe on `PATH`
- An audio file to analyze

### Installation

#### From source

```bash
git clone https://github.com/iSerganov/gocue.git
cd gocue
make build
# binary: ./dist/gocue  (version injected via ldflags from Makefile VERSION)
```

#### Using Go

```bash
go install github.com/iSerganov/gocue@latest
```

### Basic usage

```bash
# Analyze with defaults (JSON on stdout; diagnostics on stderr)
./gocue audio_file.wav

# Pretty-print JSON
./gocue -n audio_file.wav

# Custom loudness target (-16 LUFS instead of -18)
./gocue -t -16 audio_file.wav
```

## Usage

### Command-line options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--target` | `-t` | `-18.0` | LUFS reference target (-23.0 to 0.0) |
| `--silence` | `-s` | `-42.0` | LU below integrated loudness for cue-in & cue-out |
| `--overlay` | `-o` | `-8.0` | LU below integrated loudness to start the next track |
| `--longtail` | `-l` | `15.0` | Overlay duration (s) that triggers long-tail recalculation (0–60) |
| `--extra` | `-x` | `-12.0` | Extra LU below overlay for long-tail / sustained endings |
| `--drop` | `-d` | `40.0` | Max % loudness drop still counted as sustained ending (0–100; `0` disables) |
| `--noclip` | `-k` | `false` | Lower gain if needed so true peak stays at or below −1 dBFS |
| `--nice` | `-n` | `false` | Pretty-print JSON |
| `--blankskip` | `-b` | `0.0` | Early cue-out on in-track silence longer than this many seconds (`0` = off) |
| `--exec_timeout` | `-e` | `20s` | Timeout for ffprobe/ffmpeg |
| `--print_flags` | `-p` | `false` | Log flag values to **stderr** (stdout stays JSON-only) |

### Parameter ranges

- **Target LUFS**: −23.0 to 0.0
- **Silence / overlay / extra**: −96.0 to 0.0
- **Longtail / blankskip**: 0.0 to 60.0 seconds
- **Sustained drop**: 0.0 to 100.0 percent

## Output format

JSON is written to **stdout**. Progress/analysis lines go to **stderr**, so pipes and Liquidsoap stay machine-readable.

Loudness and gain fields are **unit-suffixed strings** (as required by the autocue protocol). Time fields and true peak (linear) are JSON numbers; booleans are JSON booleans.

```json
{
  "duration": 180.5,
  "liq_cue_duration": 175.2,
  "liq_cue_in": 2.8,
  "liq_cue_out": 178.0,
  "liq_cross_start_next": 173.5,
  "liq_longtail": false,
  "liq_sustained_ending": true,
  "liq_loudness": "-14.200 LUFS",
  "liq_loudness_range": "8.500 LU",
  "liq_amplify": "3.800 dB",
  "liq_amplify_adjustment": "0.000 dB",
  "liq_reference_loudness": "-18.000 LUFS",
  "liq_blankskip": 0.0,
  "liq_blank_skipped": false,
  "liq_true_peak": 0.95,
  "liq_true_peak_db": "-0.400 dBFS"
}
```

### Output fields

| Field | Meaning |
|-------|---------|
| `duration` | File duration in seconds (from ffprobe when available) |
| `liq_cue_duration` | Playout length (`cue_out − cue_in`), seconds |
| `liq_cue_in` / `liq_cue_out` | Cue points from start of file, seconds |
| `liq_cross_start_next` | Suggested next-track overlay point, seconds |
| `liq_longtail` | Long fade-out handling was applied |
| `liq_sustained_ending` | Sustained ending handling was applied |
| `liq_loudness` | Integrated loudness (`"… LUFS"`) |
| `liq_loudness_range` | Loudness range (`"… LU"`) |
| `liq_amplify` | Gain to reach target (`"… dB"`) |
| `liq_amplify_adjustment` | Extra reduction from `--noclip` (`"… dB"`) |
| `liq_reference_loudness` | Target used (`"… LUFS"`) |
| `liq_blankskip` | Blank-skip setting used, seconds |
| `liq_blank_skipped` | Early cue-out due to in-track silence |
| `liq_true_peak` | Linear true peak |
| `liq_true_peak_db` | True peak (`"… dBFS"`) |

Note: gocue picks the **latest** of the normal / sustained / longtail overlay candidates so special endings stay intact.

## Real-world examples

### Hidden track

The Nirvana track *Something in the Way / Endless, Nameless* (Nevermind, 1991) has ~3:48 of song, ~10 minutes of silence, then the hidden track *Endless, Nameless*:

![Screenshot of Nirvana song waveform, showing a 10-minute silent gap in the middle](https://github.com/Moonbase59/autocue/assets/3706922/fa7e66e9-ccd8-42f3-8051-fa2fc060a939)

**Normal mode (blankskip off, default):**

```bash
$ gocue "Nirvana - Something in the Way _ Endless, Nameless.mp3"
# diagnostics on stderr, e.g.:
# Overlay: -18.47 LUFS, Longtail: -33.47 LUFS, Measured end avg: -41.05 LUFS, Drop: 38.91%
# Overlay times: 1222.30/1228.10/0.00 s (normal/sustained/longtail), using: 1228.10s.
# Cue out time: 1232.20 s
{"duration":1235.1,"liq_cue_duration":1232.2,"liq_cue_in":0,"liq_cue_out":1232.2,"liq_cross_start_next":1228.1,"liq_longtail":false,"liq_sustained_ending":true,"liq_loudness":"-10.470 LUFS","liq_loudness_range":"7.900 LU","liq_amplify":"-7.530 dB","liq_amplify_adjustment":"0.000 dB","liq_reference_loudness":"-18.000 LUFS","liq_blankskip":0,"liq_blank_skipped":false,"liq_true_peak":1.632,"liq_true_peak_db":"4.250 dBFS"}
```

**With blank detection (cue-out at start of silence):**

```bash
$ gocue -b 5 -- "Nirvana - Something in the Way _ Endless, Nameless.mp3"
# Cue out near the start of the long silence (~227.5 s) instead of the end of the file
{"duration":1235.1,"liq_cue_duration":227.5,"liq_cue_in":0,"liq_cue_out":227.5,"liq_cross_start_next":224.1,"liq_longtail":false,"liq_sustained_ending":false,"liq_loudness":"-10.470 LUFS","liq_loudness_range":"7.900 LU","liq_amplify":"-7.530 dB","liq_amplify_adjustment":"0.000 dB","liq_reference_loudness":"-18.000 LUFS","liq_blankskip":5,"liq_blank_skipped":true,"liq_true_peak":1.632,"liq_true_peak_db":"4.250 dBFS"}
```

### Long tail handling

*Bohemian Rhapsody* has a long ending that should not be cut by an early overlay:

![Screenshot of Queen's Bohemian Rhapsody waveform, showing the almost 40 second long silent ending](https://github.com/Moonbase59/autocue/assets/3706922/28f82f63-6341-4064-aaed-36339b0a2d4d)

```bash
$ gocue "Queen - Bohemian Rhapsody.flac"
# Overlay times: 336.50/348.50/348.50 s (normal/sustained/longtail), using: 348.50s.
{"duration":355.1,"liq_cue_duration":353,"liq_cue_in":0,"liq_cue_out":353,"liq_cross_start_next":348.5,"liq_longtail":true,"liq_sustained_ending":true,"liq_loudness":"-15.500 LUFS","liq_loudness_range":"15.960 LU","liq_amplify":"-2.500 dB","liq_amplify_adjustment":"0.000 dB","liq_reference_loudness":"-18.000 LUFS","liq_blankskip":0,"liq_blank_skipped":false,"liq_true_peak":0.99,"liq_true_peak_db":"-0.090 dBFS"}
```

How that result is chosen:

1. **Cue-out** — scan backwards from the end for momentary loudness above `loudness + --silence` (default −42 LU → ~−60 LU noise floor at −18 LUFS target).
2. **Normal overlay** — scan backwards from cue-out for loudness above `loudness + --overlay` (default −8 LU). Here that yields ~336.5 s (too early).
3. **Long tail** — if `cue_out − overlay` exceeds `--longtail` (default 15 s), recalculate with `--extra` (default −12 LU). Sustained endings use a similar path via `--drop`.
4. **Final overlay** — the latest of normal / sustained / longtail is used (`348.5` s here, `liq_longtail: true`).

Fade length after overlay is configured in Liquidsoap (not by gocue), e.g.:

```ruby
settings.autocue.gocue.fade_out := 2.5  # seconds
```

### Blank (silence) detection

Use `--blankskip` for “hidden track” gaps. It is **off by default** (`0.0`) in gocue—enable it only when you want early cue-outs on long in-track silence. Avoid it for spoken word, jingles, ads, DJ sets, or podcasts.

```bash
./gocue -b 5 audio_file.mp3     # cue-out after ≥5 s of silence
./gocue -b 0 audio_file.mp3     # explicit disable (same as default)
./gocue -b 10 audio_file.mp3    # require longer silence
```

## Liquidsoap protocol

**Requires [Liquidsoap 2.2.5+](https://github.com/savonet/liquidsoap/releases).**

Prefix a playlist or request with `autocue:`:

```ruby
radio = playlist(prefix="autocue:", "/path/to/playlist.m3u")
```

Or use `enable_autocue_metadata()` for all files—use one approach, not both. For video streams, exclude media with `liq_gocue=false` (full video analysis is expensive).

Typical settings (defaults shown; exact keys depend on your `.liq` script):

```ruby
settings.autocue.gocue.path := "gocue"
settings.autocue.gocue.fade_in := 0.1  # seconds
settings.autocue.gocue.fade_out := 2.5  # seconds
settings.autocue.gocue.timeout := 60.0  # seconds
settings.autocue.gocue.target := -18.0  # LUFS
settings.autocue.gocue.silence := -42.0  # LU below track loudness
settings.autocue.gocue.overlay := -8.0  # LU below track loudness
settings.autocue.gocue.longtail := 15.0  # seconds
settings.autocue.gocue.overlay_longtail := -12.0  # extra LU
settings.autocue.gocue.sustained_loudness_drop := 40.0
settings.autocue.gocue.noclip := false
settings.autocue.gocue.blankskip := 0.0
settings.autocue.gocue.unify_loudness_correction := true
settings.autocue.gocue.write_tags := false       # Liquidsoap writes liq_* tags (not gocue itself)
settings.autocue.gocue.write_replaygain := false
settings.autocue.gocue.force_analysis := false
settings.autocue.gocue.nice := false
```

## Advanced configuration

```bash
# Long tail: longer threshold, deeper extra LU
./gocue -l 20 -x -15 audio_file.wav

# Sustained ending sensitivity
./gocue -d 30 -x -10 audio_file.wav

# Clipping prevention (true peak ≤ −1 dBFS)
./gocue -k audio_file.wav

# Blank skip (hidden tracks)
./gocue -b 5 audio_file.wav
```

## Use cases

### Radio automation

Wire gocue through your Liquidsoap autocue script (see links above). A minimal sketch:

```ruby
radio = playlist(prefix="autocue:", "~/music/")
```

### Batch analysis

```bash
for file in *.wav; do
  ./gocue -t -16 -s -45 -o -6 "$file" > "${file%.wav}.json"
done
```

### Strict EBU R128 target

```bash
./gocue -t -23 -s -60 -o -10 content.wav
```

## Architecture

| Piece | Role |
|-------|------|
| `cmd/cue` | Cobra CLI: flags, validation, JSON on stdout |
| `pkg/cue.Calculator` | ffprobe tag probe + ffmpeg ebur128 scan |
| `pkg/cue.Result` | Numeric result; unit suffixes only at JSON/YAML marshal |
| `pkg/cue.Frame` | Per-frame momentary loudness (~100 ms) |

**Dependencies:** FFmpeg/FFprobe (external), [Cobra](https://github.com/spf13/cobra), [yaml.v3](https://github.com/go-yaml/yaml) (library marshal helper).

**Performance notes:**

- Streams ffmpeg stdout; does not load the whole audio file into Go memory
- Reuses complete tags from the file when possible (read-only cache)
- Configurable `--exec_timeout` for probe/scan

## Testing

```bash
go test ./...
go test -race -count=1 ./...
go test ./pkg/cue -v
```

Requires `ffmpeg` / `ffprobe` on `PATH` for scan/probe tests.

## Project structure

```
gocue/
├── cmd/cue/                 # CLI
├── pkg/cue/                 # Library
│   ├── calculator.go
│   ├── scan.go
│   ├── result.go
│   ├── frame.go
│   ├── error.go
│   └── test_data/           # Audio fixtures
├── main.go
├── Makefile
└── go.mod
```

## Contributing

Contributions are welcome. For larger changes, open an issue first.

1. Fork and create a feature branch
2. Add tests where practical
3. Run `go test -race ./...`
4. Open a pull request

## License

Apache 2.0 — see [LICENSE](LICENSE).

## Acknowledgments

Inspired by [Moonbase59/autocue](https://github.com/Moonbase59/autocue) (Python). That project is no longer maintained and drifted from current Liquidsoap; gocue is a Go port with a focus on performance and a cleaner library/CLI split.

- **FFmpeg** — analysis pipeline
- **Liquidsoap** — autocue protocol
- **EBU** — R128 loudness standard

## Support

- Open a GitHub issue
- `./gocue --help`
