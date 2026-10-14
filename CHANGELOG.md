# Changelog

All notable changes to gocue are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and gocue uses
[Semantic Versioning](https://semver.org/).

## [1.3.0] - 2026-10-13

gocue no longer starts `ffmpeg` or `ffprobe`. It reads tags and measures
loudness in-process through FFmpeg's libraries (libav) via cgo. For the same
file and flags it prints the same JSON as 1.2.0, byte for byte.

### Changed

- The tag probe reads durations and tags through libavformat instead of an
  `ffprobe` process. Durations are formatted exactly as ffprobe prints them,
  and tags merge in the same order, so cached files hit the cache as before.
- The EBU R128 scan runs FFmpeg's `ebur128` filter in-process instead of
  parsing the text that `ffmpeg … ametadata=print` wrote to a pipe. Frame
  times, momentary and integrated loudness, true peak and loudness range are
  identical to the old path on every test file and at more than one target
  loudness. Cue points match Python autocue as before.
- Failures name libav's reason, such as `analysis failed for "a.mp3": No such
  file or directory`, instead of a process exit status. A timeout reads
  `timed out after 20s` and is checked inside libav's reads.
- Building gocue needs Go 1.27, a C compiler, FFmpeg's headers and
  `pkg-config`. FFmpeg 9.0.1 is the tested version. On Linux, link with
  `-Wl,-rpath,<dir>/lib -Wl,--disable-new-dtags` when the libraries live
  outside the system path; the README explains why.
- The example Docker station builds gocue against FFmpeg 9.0.1's shared
  libraries for amd64 and arm64 and ships them under `/opt/ffmpeg/lib`.
  Liquidsoap keeps its own FFmpeg 7.1. The image no longer installs the
  `ffmpeg` binary.
- CI installs a pinned FFmpeg 9.0.1 build for building, linting and testing,
  and fails if any other version is found. golangci-lint is pinned to v2.14.0,
  the first release that type-checks Go 1.27 code here.

### Added

- `BenchmarkCalc` and `BenchmarkProbe` on the three longest test files, and a
  `make bench` target that runs them with fixed iterations so separate runs
  can be compared with benchstat.
- The `.github/actions/setup-ffmpeg` action used by every CI job.
- This changelog.

### Removed

- The runtime dependency on the `ffmpeg` and `ffprobe` binaries.
  `gocue.liq` still calls `ffmpeg` when `write_tags` is enabled.

### Performance

Measured against 1.2.0 on the three longest test files (748–948 s of audio)
with FFmpeg 9.0.1 on an Apple M4 Max. The README has the full table.

- The tag probe, which is all the cached path does, is 70× to 109× faster:
  0.21–0.33 ms instead of about 23 ms.
- A full analysis is 1.9% to 4.3% faster, and 10% faster on a 113 s file.
- The `gocue` command uses 16% less CPU time and 91% less kernel time. Its
  wall time is 0.6% to 1.2% shorter, because decoding and the loudness
  measurement were already libav code and still dominate a long file.
- Go allocates 86% less memory per analysis and 67% fewer objects. Peak RSS
  is 28–29 MiB for the single process. 1.2.0 reported about 25 MiB, most
  likely the ffmpeg child's peak, with gocue's own process running alongside.

## [1.2.0] - 2026-09-12

### Added

- An end-to-end integration suite (`make test-integration`, build tag
  `integration`) that runs real Liquidsoap against the real gocue binary. It
  checks cue points, fades, parameter forwarding, `annotate:` overrides, tag
  write-back and playlist order, with fixtures under `integration/testdata/`.
- `integration/scripts/gocue.liq`, the Liquidsoap autocue provider for gocue,
  adapted from Moonbase59's autocue script.

### Changed

- **Breaking for library callers:** `Result.Annotations()` returns only the
  map; it no longer returns an error.
- A failing ffmpeg scan quotes the start of ffmpeg's error output instead of
  only its exit status.
- `gocue.liq` runs gocue under `nice -n 18`, since gocue's `--nice` means
  pretty-printed JSON rather than a lower priority.
- The version string is set from the Makefile at build time; a build without
  it reports `dev`.
- Pretty-printed JSON is indented by two spaces with no line prefix.

### Fixed

- Cached tags must now parse as numbers. A damaged `liq_cue_out` used to reach
  Liquidsoap as `0` and play as a zero-length track; now the file is
  re-analysed.
- A frame whose momentary loudness is `nan`, or missing, counts as silence
  (negative infinity), as upstream autocue treats it. Before, it compared as
  NaN or as full scale and could hide blanks and sustained endings.
- If reading ffmpeg's output fails, ffmpeg is stopped right away instead of
  blocking on a full pipe until the timeout.
- A failure to write the JSON result is reported instead of ignored.

## [1.1.2] - 2026-08-05

### Fixed

- Tags stored at the container level, as FLAC and MP3 files do, are merged
  with the audio stream's tags, so those files can use the cached path
  instead of always being re-analysed. Stream tags win on conflict.
- A non-zero `--blankskip` forces re-analysis when the file's `liq_blankskip`
  tag is missing or different, so cached cue-outs cannot come from another
  blankskip setting.
- After a full scan, `duration` is the container's precise duration rather
  than the coarse value derived from the last 100 ms frame.
- Only `ErrRequireAnalysis` triggers a scan; any other probe error is
  returned. The error now unwraps and is safe to print when empty.
- `--print_flags` writes to stderr, so stdout carries only JSON for
  Liquidsoap.

### Changed

- The README documents the read-only tag cache, the real CLI flags, the
  unit-suffixed JSON fields and the blankskip default.

## [1.1.1] - 2026-07-01

### Changed

- `CalculatorOptions.Diagnostics` takes an `io.Writer` for scan progress
  messages; it defaults to stderr.
- `Result` stores every measurement as a number. Unit suffixes such as
  `"-18.000 LUFS"` are added only when JSON, YAML or `Annotations()` output
  is produced.
- The CLI builds its flags per invocation instead of in package-level
  variables, and returns errors instead of exiting from inside the command.
- Tag lookups and `Annotations()` do less work per call: tag sets are maps,
  and annotations are built from the struct directly instead of through a
  JSON round trip.

### Fixed

- A `CalculatorOptions` with no `ExecutionTimeout` no longer expires the
  analysis immediately; it gets the default timeout.
- A failed read of ffmpeg's output is reported instead of analysing a partial
  frame list.
- The help text no longer claims that gocue writes tags; it only reads them.

## [1.1.0] - 2026-06-01

### Added

- A CI workflow with lint, race-enabled tests and coverage.
- The `tch_big.ogg` fixture, a long track with a long tail and a sustained
  ending.

### Changed

- Building gocue needs Go 1.26.
- The scan and its cue/overlay math moved to `scan.go`, separate from tag
  probing in `calculator.go`.
- ffprobe output is decoded into typed structures, and analysis frames are
  stored by value, which uses less memory per scan.
- Errors and tag-read warnings go to stderr instead of stdout.
- `Annotations()` reports a value of an unexpected type as `N/A`.

## [1.0.1] - 2025-11-12

### Added

- `Result.Annotations()`, which returns the result as Liquidsoap annotation
  key/value strings.
- A longer README with flags, output fields and examples.

## [1.0.0] - 2025-08-25

First release.

### Added

- A command-line analyser for one audio file that prints cue-in, cue-out,
  next-track overlay point and EBU R128 loudness as one line of JSON for
  Liquidsoap's `autocue:` protocol, following Moonbase59's autocue.
- Loudness, loudness range and true peak from ffmpeg's `ebur128` filter, and
  tags read with ffprobe.
- Reuse of complete `liq_*` and ReplayGain tags already in the file, which
  skips the scan.
- Long-tail and sustained-ending detection, blank skipping, clipping
  prevention, and flags for every threshold.
- `--print_flags` and benchmark tests.

[1.3.0]: https://github.com/iSerganov/gocue/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/iSerganov/gocue/compare/v1.1.2...v1.2.0
[1.1.2]: https://github.com/iSerganov/gocue/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/iSerganov/gocue/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/iSerganov/gocue/compare/v1.0.1...v1.1.0
[1.0.1]: https://github.com/iSerganov/gocue/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/iSerganov/gocue/releases/tag/v1.0.0
