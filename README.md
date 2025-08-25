# gocue

[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-1.0.0-blue.svg)](Makefile)

**gocue** is a high-performance Go-based audio analysis tool designed for professional audio workflows. It analyzes audio files to detect cue-in, cue-out, overlay points, and provides comprehensive EBU R128 loudness measurements. The tool is optimized for integration with Liquidsoap's "autocue:" protocol and supports writing metadata tags to avoid unnecessary re-analysis.

Note that this is a a stand-alone tool which only does pre-computed analysis. In order to integrate it with Liquidsoap, you need to write a `.liq` script which will use the output of this tool to generate the final playlist with `autocue` annotations. E.g., you can use [this script](https://github.com/iSerganov/autocue/blob/master/autocue.gocue.liq) as a starting point. The script is a modified version of the original [autocue.gocue.liq](https://github.com/Moonbase59/autocue/blob/master/autocue.cue_file.liq) script.
See [Liquidsoap documentation](https://www.liquidsoap.info/doc-dev/settings.html#all-available-autocue-implementations) for more details.

For the internal details see the [presentation](https://moonbase59.github.io/autocue/presentation/autocue.html) made by [Moonbase59](https://github.com/Moonbase59).

## 🎯 Features

- **Audio Analysis**: Detect cue-in and cue-out points for seamless track transitions
- **EBU R128 Compliance**: Full loudness measurement according to EBU R128 standards
- **Smart Caching**: Writes analysis results as metadata tags to avoid re-analysis
- **Multiple Formats**: Supports WAV, OGG, MP3, FLAC, M4A, WMA, ASF, AIFF, and more
- **Liquidsoap Integration**: Optimized for use with Liquidsoap autocue protocol
- **Performance Optimized**: Fast analysis with intelligent caching strategies
- **Comprehensive Output**: JSON output with detailed audio metrics
- **Configurable Thresholds**: Adjustable parameters for different audio scenarios

## 🚀 Quick Start

### Prerequisites

- Go 1.24 or higher
- FFmpeg and FFprobe installed and available in PATH
- Audio files to analyze

### Installation

#### From Source

```bash
# Clone the repository
git clone https://github.com/iSerganov/gocue.git
cd gocue

# Build the binary
make build

# The binary will be available at ./dist/gocue
```

#### Using Go

```bash
go install github.com/iSerganov/gocue@latest
```

### Basic Usage

```bash
# Analyze an audio file with default settings
./gocue audio_file.wav

# Get pretty-printed JSON output
./gocue -n audio_file.wav

# Use custom loudness target (-16 LUFS instead of default -18)
./gocue -t -16 audio_file.wav
```

## 📖 Usage

### Command Line Options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--target` | `-t` | `-18.0` | LUFS reference target (-23.0 to 0.0) |
| `--silence` | `-s` | `-42.0` | LU below integrated track loudness for cue-in & cue-out points |
| `--overlay` | `-o` | `-8.0` | LU below integrated track loudness to trigger next track |
| `--longtail` | `-l` | `15.0` | Seconds threshold for long tail detection (0.0 to 60.0) |
| `--extra` | `-x` | `-12.0` | Extra LU below overlay loudness for long tail songs |
| `--drop` | `-d` | `40.0` | Max percent loudness drop for sustained ending (0.0 to 100.0) |
| `--noclip` | `-k` | `false` | Prevent clipping by lowering track gain if needed |
| `--nice` | `-n` | `false` | Pretty-print JSON output |
| `--blankskip` | `-b` | `0.0` | Skip blank silence longer than specified seconds |
| `--exec_timeout` | `-e` | `20s` | Script execution timeout |
| `--print_flags` | `-p` | `false` | Log all flag values |

### Parameter Ranges

- **Target LUFS**: -23.0 to 0.0
- **Silence Threshold**: -96.0 to 0.0
- **Overlay Threshold**: -96.0 to 0.0
- **Longtail Duration**: 0.0 to 60.0 seconds
- **Extra LU**: -96.0 to 0.0
- **Sustained Drop**: 0.0 to 100.0 percent
- **Blank Skip**: 0.0 to 60.0 seconds

## 📊 Output Format

The tool outputs JSON data containing comprehensive audio analysis results:

```json
{
  "duration": 180.5,
  "liq_cue_duration": 175.2,
  "liq_cue_in": 2.8,
  "liq_cue_out": 178.0,
  "liq_cross_start_next": 173.5,
  "liq_longtail": false,
  "liq_sustained_ending": true,
  "liq_loudness": "-14.2",
  "liq_loudness_range": "8.5",
  "liq_amplify": "3.8",
  "liq_amplify_adjustment": "0.0",
  "liq_reference_loudness": "-18.0",
  "liq_blankskip": 0.0,
  "liq_blank_skipped": false,
  "liq_true_peak": 0.95,
  "liq_true_peak_db": "-0.4"
}
```

### Output Fields

- **duration**: Total audio file duration in seconds
- **liq_cue_duration**: Effective cue duration (cue_out - cue_in)
- **liq_cue_in**: Cue-in point in seconds from start
- **liq_cue_out**: Cue-out point in seconds from start
- **liq_cross_start_next**: Overlay point for next track
- **liq_longtail**: Whether the track has a long tail
- **liq_sustained_ending**: Whether the track has a sustained ending
- **liq_loudness**: Integrated loudness in LUFS
- **liq_loudness_range**: Loudness range in LU
- **liq_amplify**: Required amplification in dB
- **liq_true_peak**: True peak value (0.0 to 1.0)
- **liq_true_peak_db**: True peak in dBFS

## <a name="examples"></a>Real-world examples<a href="#toc" class="goToc">⇧</a>

### <a name="hidden-track"></a>Hidden track <a href="#toc" class="goToc">⇧</a>

The well-known _Nirvana_ song _Something in the Way / Endless, Nameless_ from their 1991 album _Nevermind_:

![Screenshot of Nirvana song waveform, showing a 10-minute silent gap in the middle](https://github.com/Moonbase59/autocue/assets/3706922/fa7e66e9-ccd8-42f3-8051-fa2fc060a939)

It contains the 3:48 song _Something in the Way_, followed by 10:03 of silence, followed by the "hidden track" _Endless, Nameless_.

**Normal mode (no blank detection):**

```
$ gocue -f "Nirvana - Something in the Way _ Endless, Nameless.mp3"
Overlay: -18.47 LUFS, Longtail: -33.47 LUFS, Measured end avg: -41.05 LUFS, Drop: 38.91%
Overlay times: 1222.30/1228.10/0.00 s (normal/sustained/longtail), using: 1228.10 s.
Cue out time: 1232.20 s
{"duration": 1235.1, "liq_cue_duration": 1232.2, "liq_cue_in": 0.0, "liq_cue_out": 1232.2, "liq_cross_start_next": 1228.1, "liq_longtail": false, "liq_sustained_ending": true, "liq_loudness": "-10.47 LUFS", "liq_loudness_range": "7.90 LU", "liq_amplify": "-7.53 dB", "liq_amplify_adjustment": "0.00 dB", "liq_reference_loudness": "-18.00 LUFS", "liq_blankskip": 0.0, "liq_blank_skipped": false, "liq_true_peak": 1.632, "liq_true_peak_db": "4.25 dBFS"}
```

**With blank detection (cue-out at start of silence):**

```
$ gocue -fb -- "Nirvana - Something in the Way _ Endless, Nameless.mp3"
Overlay: -18.47 LUFS, Longtail: -33.47 LUFS, Measured end avg: -41.80 LUFS, Drop: 43.05%
Overlay times: 224.10/0.00/0.00 s (normal/sustained/longtail), using: 224.10 s.
Cue out time: 227.50 s
{"duration": 1235.1, "liq_cue_duration": 227.5, "liq_cue_in": 0.0, "liq_cue_out": 227.5, "liq_cross_start_next": 224.1, "liq_longtail": false, "liq_sustained_ending": false, "liq_loudness": "-10.47 LUFS", "liq_loudness_range": "7.90 LU", "liq_amplify": "-7.53 dB", "liq_amplify_adjustment": "0.00 dB", "liq_reference_loudness": "-18.00 LUFS", "liq_blankskip": 5.0, "liq_blank_skipped": true, "liq_true_peak": 1.632, "liq_true_peak_db": "4.25 dBFS"}
```

where
- _duration_ — the real file duration (including silence at start/end of song), in seconds
- _liq_cue_duration_ — the actual playout duration (cue-in to cue-out), in seconds
- _liq_cue_in_ — cue-in point, in seconds
- _liq_cue_out_ — cue-out point, in seconds
- _liq_cross_start_next_ — suggested start point of next song, in seconds (counting from beginning of file)
- _liq_longtail_ — flag to show if song has a "long tail", i.e. a very long fade-out (true/false)
- _liq_sustained_ending_ — flag to show if song has a "sustained ending", i.e. not a "hard end" (true/false)
- _liq_loudness_ — song’s EBU R128 integrated loudness, in LUFS
- _liq_loudness_range_ — song’s loudness range (dynamics), in LU
- _liq_amplify_ — "ReplayGain"-like value, offset to desired loudness target (i.e., -18 LUFS), in dB. This is intentionally _not_ called _replaygain_track_gain_, since that tag might already exist and have been calculated using more exact tools like [`loudgain`](https://github.com/Moonbase59/loudgain).
- _liq_amplify_adjustment_ — shows adjustment done by clipping prevention, in dB
- _liq_reference_loudness_ — loudness reference target used, in LUFS, like _replaygain_reference_loudness_
- _liq_blankskip_ — shows blank (silence) skipping used, in seconds (0.00=disabled)
- _liq_blank_skipped_ — flag to show that we have an early cue-out, caused by silence in the song (true/false)
- _liq_true_peak_ — linear true peak, much like _replaygain_track_peak_, but using a true peak algorithm
- _liq_true_peak_db_ — true peak in dBFS (dBTP)

### <a name="long-tail-handling"></a>Long tail handling <a href="#toc" class="goToc">⇧</a>

_Bohemian Rhapsody_ by _Queen_ has a rather long ending, which we don’t want to destroy by overlaying the next song too early. This is where `gocue`’s automatic "long tail" handling comes into play. Let’s see how the end of the song looks like:

![Screenshot of Queen's Bohemian Rhapsody waveform, showing the almost 40 second long silent ending](https://github.com/Moonbase59/autocue/assets/3706922/28f82f63-6341-4064-aaed-36339b0a2d4d)

Here are the values we get from `gocue`:

```
$ gocue -f "Queen - Bohemian Rhapsody.flac"
Overlay: -23.50 LUFS, Longtail: -38.50 LUFS, Measured end avg: -44.31 LUFS, Drop: 23.62%
Overlay times: 336.50/348.50/348.50 s (normal/sustained/longtail), using: 348.50 s.
Cue out time: 353.00 s
{"duration": 355.1, "liq_cue_duration": 353.0, "liq_cue_in": 0.0, "liq_cue_out": 353.0, "liq_cross_start_next": 348.5, "liq_longtail": true, "liq_sustained_ending": true, "liq_loudness": "-15.50 LUFS", "liq_loudness_range": "15.96 LU", "liq_amplify": "-2.50 dB", "liq_amplify_adjustment": "0.00 dB", "liq_reference_loudness": "-18.00 LUFS", "liq_blankskip": 0.0, "liq_blank_skipped": false, "liq_true_peak": 0.99, "liq_true_peak_db": "-0.09 dBFS"}
```

We notice the `liq_longtail` flag is `true`, and the "normal" overlay time would be `336.50`.

Let’s follow the steps `gocue` took to arrive at this result.

#### <a name="cue-out-point"></a>Cue-out point <a href="#toc" class="goToc">⇧</a>

`gocue` uses the `-s`/`--silence` parameter value (-42 LU default) to scan _backwards from the end_ for something that is louder than -42 LU below the _average (integrated) song loudness_, using the EBU R128 momentary loudness algorithm. This is _not_ a simple "level check"! Using the default (playout) reference loudness target of `-18 LUFS` (`-t`/`--target` parameter), we thus arrive at a noise floor of -60 LU, which is a good silence level to use.

![Screenshot of Bohemian Rhapsody waveform, showing calculated cue-out point at 353.0 seconds (2 seconds before end)](https://github.com/Moonbase59/autocue/assets/3706922/c745989a-5f32-4aa1-a5b7-ac4bc955e568)

`gocue` has determined the _cue-out point_ at `353.0` seconds (5:53).

#### <a name="cross-duration-where-the-next-track-could-start-and-be-overlaid"></a>Cross duration (where the next track could start and be overlaid) <a href="#toc" class="goToc">⇧</a>

`gocue` uses the `-o`/`--overlay` parameter value (-8 LU default) to scan _backwards from the cue-out point_ for something that is louder than -8 LU below the _average (integrated) song loudness_, thus finding a good point where the next song could start and be overlaid.

![Screenshot of Bohemian Rhapsody waveform, showing the cross duration calculated in the first run: 16.5 seconds before end – way too much](https://github.com/Moonbase59/autocue/assets/3706922/20a9396b-a31a-4a11-87b4-641d6868cc49)

`gocue` has determined a "normal" overlay start point (`liq_cross_start_next`) of `336.5` seconds (5:36.5).

We can see this would destroy an important part of the song’s end.

#### <a name="a-long-tail"></a>A long tail! <a href="#toc" class="goToc">⇧</a>

Finding that the calculated cross duration of `16.5` seconds is longer than 15 seconds (the `-l`/`--longtail` parameter), `gocue` now _recalculates the overlay start position_ automatically, using an extra -15 LU loudness offset (`-x`/`--extra` parameter, defaults to `-12` in v4.0.3+), and arrives at this:

![Screenshot of Bohemian Rhapsody waveform, showing the newly calculated cross duration: 4.5 seconds before end – just right, not cutting off important parts of the song ending](https://github.com/Moonbase59/autocue/assets/3706922/9f9ec3af-89d4-4edc-9316-d53ed1fcf000)

`gocue` has now set `liq_cross_start_next` to `348.5` seconds and `liq_longtail` to `true` so we know this song has a "long tail" and been calculated differently.

Much better!

#### <a name="avoiding-too-much-overlap"></a>Avoiding too much overlap <a href="#toc" class="goToc">⇧</a>

We possibly don’t want the previous song to play "too much" into the next song, so we can set a _default fade-out_ (`settings.autocue.gocue.fade_out`). This will ensure a pleasing limit. We use `2.5` seconds as a default:

```ruby
settings.autocue.gocue.fade_out := 2.5  # seconds
```

![Screenshot of Bohemian Rhapsody waveform, showing the user-defined fade-out of 2.5 seconds, starting 4.5 seconds before song ends](https://github.com/Moonbase59/autocue/assets/3706922/f1e96db6-2f23-4cdd-9693-24711fe91895)

Fading area, using above settings. The rest of the ending won’t be heard.

### <a name="blank-silence-detection"></a>Blank (silence) detection <a href="#toc" class="goToc">⇧</a>

Blank (silence) detection within a song is a great feature if you have songs with silence in the middle and a "hidden track" at the end. Autocue can then perform an early cue-out at the point where the silence begins. No one wants to broadcast 10 minutes of dead air, right?

This feature should be used _wisely_, because it could truncate tracks you wouldn’t like to end early, like jingles, ads, prerecorded shows, DJ sets or podcast episodes!

The default minimum silence length before it triggers is set to `5.0` seconds.

You can avoid issues in several ways:
- Don’t use the `-b`/`--blankskip` option (default).
- Set it to `0.00`, which disables the feature.
- Increase the minimum silence length: `-b 10.0` for 10 seconds.
- Manually assign later cue-in/cue-out points in the AzuraCast UI (user settings here overrule the automatic values).


## <a name="liquidsoap-protocol"></a>Liquidsoap protocol <a href="#toc" class="goToc">⇧</a>

**Note: `autocue.gocue` is meant to be used with [Liquidsoap 2.2.5](https://github.com/savonet/liquidsoap/releases) or newer.**

The protocol is invoked by prefixing a playlist or request with `autocue:` like so:

```ruby
radio = playlist(prefix="autocue:", "/home/matthias/Musik/Playlists/Radio/Classic Rock.m3u")
```

Alternatively, you can set `enable_autocue_metadata()` and it will process _all files_ Liquidsoap handles. Use _either_—_or_, but not _both_ variants together. If running video streams, you might also want to _exclude_ the video files from processing, by annotating `liq_gocue=false` for them, for instance as a playlist prefix. `autocue` _can_ handle multi-gigabyte video files, but that will eat up _lots_ of CPU (and bandwidth) and might bring your station down.

`autocue` offers the following settings (defaults shown):

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
settings.autocue.gocue.sustained_loudness_drop := 40.0  # max. percent drop to be considered sustained
settings.autocue.gocue.noclip := false  # clipping prevention like loudgain's `-k`
settings.autocue.gocue.blankskip := 0.0  # skip silence in tracks
settings.autocue.gocue.unify_loudness_correction := true  # unify `replaygain_track_gain` & `liq_amplify`
settings.autocue.gocue.write_tags := false  # write liq_* tags back to file
settings.autocue.gocue.write_replaygain := false  # write ReplayGain tags back to file
settings.autocue.gocue.force_analysis := false  # force re-analysis even if tags found
settings.autocue.gocue.nice := false  # Linux/MacOS only: Use NI=18 for analysis
settings.autocue.gocue.use_json_metadata := true  # pass metadata to `gocue` as JSON
```

## 🔧 Advanced Configuration

### Long Tail Detection

For songs with extended endings, gocue automatically detects "long tail" scenarios and applies additional analysis:

```bash
# Customize long tail detection
./gocue -l 20 -x -15 audio_file.wav
```

### Sustained Ending Detection

Tracks with sustained endings are analyzed using the `--extra` parameter to preserve musical integrity:

```bash
# Adjust sustained ending sensitivity
./gocue -d 30 -x -10 audio_file.wav
```

### Clipping Prevention

Enable automatic gain adjustment to prevent clipping:

```bash
# Prevent clipping above -1 dBFS
./gocue -k audio_file.wav
```

### Blank Skip

Remove hidden tracks or long silence periods:

```bash
# Skip silence longer than 5 seconds
./gocue -b 5 audio_file.wav
```

## 🎵 Use Cases

### Radio Automation

Perfect for radio stations using Liquidsoap for automated playout:

```liquidsoap
# Liquidsoap configuration example
radio = playlist("~/music/")
radio = autocue(radio, "gocue")
```

### Audio Post-Production

Analyze audio files for consistent loudness and transition points:

```bash
# Batch analysis with custom parameters
for file in *.wav; do
  ./gocue -t -16 -s -45 -o -6 "$file" > "${file%.wav}.json"
done
```

### Content Creation

Ensure consistent audio levels across multiple content pieces:

```bash
# Analyze with strict EBU R128 compliance
./gocue -t -23 -s -60 -o -10 content.wav
```

## 🏗️ Architecture

### Core Components

- **Calculator**: Main analysis engine using FFmpeg/FFprobe
- **Result**: Data structure for analysis results
- **Frame**: Audio frame processing utilities
- **Error**: Custom error handling

### Dependencies

- **FFmpeg**: Audio processing and analysis
- **FFprobe**: Audio metadata extraction
- **Cobra**: Command-line interface framework
- **YAML**: Configuration file support

### Performance Features

- **Tag Caching**: Stores analysis results in audio file metadata
- **Smart Re-analysis**: Skips analysis if cached data is sufficient
- **Timeout Protection**: Configurable execution timeouts
- **Memory Efficient**: Streams audio data without full file loading

## 🧪 Testing

Run the test suite to verify functionality:

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific test suite
go test ./pkg/cue -v
```

## 📁 Project Structure

```
gocue/
├── cmd/cue/           # Command-line interface
├── pkg/cue/           # Core library package
│   ├── calculator.go  # Main analysis logic
│   ├── result.go      # Data structures
│   ├── frame.go       # Frame processing
│   └── error.go       # Error handling
├── test_data/         # Test audio files
├── main.go            # Application entry point
├── Makefile           # Build configuration
└── go.mod             # Go module definition
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request. For major changes, please open an issue first to discuss what you would like to change.

### Development Setup

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

The project is inspired by the Python project [autocue](https://github.com/Moonbase59/autocue).
It's a great tool for audio analysis and automation, but unfortunately it is not maintained anymore and incompatible with the latest versions of Liquidsoap.
I've ported it to Go with some performance improvements and code cleanup.

- **FFmpeg**: For audio processing capabilities
- **Liquidsoap**: For the autocue protocol inspiration
- **EBU**: For the R128 loudness measurement standard
- **Go Community**: For the excellent ecosystem and tools

## 📞 Support

If you encounter any issues or have questions:

- Open an issue on GitHub
- Check the existing issues for solutions
- Review the command-line help: `./gocue --help`

---

**gocue** - Professional audio analysis for modern workflows
