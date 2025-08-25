# gocue

[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-1.0.0-blue.svg)](Makefile)

**gocue** is a high-performance Go-based audio analysis tool designed for professional audio workflows. It analyzes audio files to detect cue-in, cue-out, overlay points, and provides comprehensive EBU R128 loudness measurements. The tool is optimized for integration with Liquidsoap's "autocue:" protocol and supports writing metadata tags to avoid unnecessary re-analysis.

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
