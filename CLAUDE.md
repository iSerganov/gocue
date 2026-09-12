# CLAUDE.md

Guidance for Claude Code when working in this repository. The first half is how
to work, the second half is what the project is and how it must behave.

**Tradeoff:** the working guidelines below bias toward caution over speed. For
trivial tasks, use judgment.

## How to work here

### 1. Think before coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them, don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

In this repo that means one extra step before touching analysis code: read the
corresponding Python function in `~/Development/autocue/cue_file` first. Code
here that looks wrong is usually upstream behaviour reproduced on purpose.

### 2. Simplicity first

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes,
simplify.

### 3. Surgical changes

**Touch only what you must. Clean up only your own mess.**

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it, don't delete it.
- Remove imports, variables and functions that YOUR changes orphaned. Leave
  pre-existing dead code alone unless asked.

The test: every changed line should trace directly to the user's request. The
mirror formulas in `scan.go` are the standing example of code that looks like an
easy cleanup and must not be touched.

### 4. Goal-driven execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:

- "Add validation" becomes "write tests for invalid inputs, then make them pass"
- "Fix the bug" becomes "write a test that reproduces it, then make it pass"
- "Refactor X" becomes "ensure tests pass before and after"

For multi-step tasks, state a brief plan:

```
1. [Step] -> verify: [check]
2. [Step] -> verify: [check]
3. [Step] -> verify: [check]
```

Here the strongest available criterion is `TestScanRegression` plus a direct
comparison against the Python reference run with `-f`. Any analysis change that
cannot be checked that way needs a new fixture or a new pinned value.

### 5. Commits

Follow the [Go commit message conventions](https://go.dev/wiki/CommitMessage):
a short imperative summary line of the form `<package>: <what changed>`,
lowercase, with no trailing period. Then a blank line, then a body that explains
why the change was made. Recent history uses this shape, for example
`cue: fix tag cache, blankskip reuse, and JSON stdout hygiene`.

### 6. Comments

Minimum comments in code. Write a doc comment above **exported** types,
functions, methods, constants and vars, starting with the identifier's name, per
standard Go doc conventions. Do not comment obvious logic or restate what the
code already says.

The exception is the parity code. Where a line reproduces upstream Python
behaviour that reads as a bug, a short comment saying so is load-bearing and
must survive.

### 7. Tests

- Organize tests into testify suites (`suite.Suite`), not bare
  `func TestX(t *testing.T)`.
- Always write table-driven unit tests. Case names must be self-descriptive, so
  that no comment is needed to explain what a case does.
- Assert with `testify/assert` and `testify/require`.
- Integration tests carry the `//go:build integration` tag and are excluded from
  `go test ./...`.

This applies to new and modified tests. `pkg/cue` already uses suites, though
not every case is table-driven yet. `cmd/cue/cue_test.go` and the three files in
`integration/` predate the convention and use bare test functions. Migrate one
of those to a suite only when you are already changing it, not as drive-by
cleanup.

### 8. Code quality

- Avoid duplication at all costs. Extract shared logic instead of copy-pasting.
- Enforce SOLID and general Go practice: small interfaces, dependency injection
  over globals, explicit error handling, no premature abstraction.
- Lint your changes before considering them done.

This repo has no `.golangci.yml`, so `golangci-lint run` uses the default linter
set. CI pins v2.11.4 through `golangci/golangci-lint-action` in
[.github/workflows/ci.yml](.github/workflows/ci.yml). Use a v2.x binary locally
so results match, and re-pin here if that version changes.

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer
rewrites due to overcomplication, and clarifying questions come before
implementation rather than after mistakes.

## What this project is

`gocue` is a Go port of Moonbase59's Python `autocue` analyser (`cue_file`). It
reads one audio file, works out cue-in / cue-out / next-track overlay points and
EBU R128 loudness, and prints a single line of JSON on stdout for Liquidsoap's
`autocue:` protocol.

Two hard rules that shape everything else:

1. **gocue never writes tags to audio files.** It only reads them. Tag
   write-back lives in the Liquidsoap script.
2. **Output must stay compatible with upstream Python autocue.** See the parity
   contract below before touching any analysis math.

## Commands

```bash
make build                 # -> ./dist/gocue, version injected via ldflags
make test                  # go test -race -count=1 ./...
make test-integration      # needs liquidsoap; go test -tags=integration ./integration/
golangci-lint run --timeout=5m
go test ./pkg/cue -run TestCalculatorSuite/TestScanRegression -v
```

`ffmpeg` and `ffprobe` must be on `PATH` for almost every test in `pkg/cue`.
Integration tests additionally need `liquidsoap` 2.3.0+ and build their own
binary into `integration/.bin/`. A Homebrew liquidsoap can break after an ffmpeg
major upgrade (missing `libswresample` dylib); check `liquidsoap --version`
before assuming an integration failure is a code problem.

CI runs lint, race tests with coverage, and the Liquidsoap integration job. Keep
all three green.

## Architecture

| File | Role |
|------|------|
| [cmd/cue/cue.go](cmd/cue/cue.go) | Cobra CLI, flag ranges, JSON printing |
| [pkg/cue/calculator.go](pkg/cue/calculator.go) | ffprobe tag probe, cached fast path, gain math |
| [pkg/cue/scan.go](pkg/cue/scan.go) | Full ffmpeg ebur128 scan and all cue/overlay math |
| [pkg/cue/result.go](pkg/cue/result.go) | Numeric `Result`, unit suffixes applied only at marshal time |
| [pkg/cue/frame.go](pkg/cue/frame.go) | One ebur128 frame (PTS + momentary loudness) |
| [pkg/cue/error.go](pkg/cue/error.go) | `ErrRequireAnalysis`, the "cache is unusable" signal |
| [integration/scripts/gocue.liq](integration/scripts/gocue.liq) | Production Liquidsoap autocue provider, not test scaffolding |

`Calc` has exactly two paths and they must produce the same shape of result:

- **Cached path.** `probe` reads tags, `doPreAnalysis` decides they are
  sufficient, `populate` fills gaps, `adjustLoudness` derives values, `parseTags`
  builds the `Result`. No audio is decoded.
- **Scan path.** `doPreAnalysis` returns `ErrRequireAnalysis`, `scan` runs
  ffmpeg, then `applyProbeDuration` replaces the coarse frame-derived duration
  with the precise container duration.

Only `ErrRequireAnalysis` may trigger a scan. Any other probe error is fatal and
propagates.

## The parity contract

The reference implementation is checked out at `~/Development/autocue/cue_file`
(a fork of upstream v4.1.1). Run it with `-f` to force a fresh analysis and
compare.

Things that look wrong but are correct, because upstream does the same:

- `cueOutTime = math.Max(cueOutTime, duration-cueOutTime)` and the matching
  mirror formulas for the three overlay candidates. These are verbatim upstream
  behaviour and are pinned by `TestScanRegression`. Do not "simplify" them.
- Restoring the wider `end = endBlank` window after a blankskip search, which
  lets the overlay search run over trailing silence.
- Picking the latest of the normal / sustained / longtail overlay candidates.

Differences from Python that the user has reviewed and accepted. Do not change
these to match Python unless explicitly asked:

- 3-decimal precision on loudness and gain fields, where Python uses 2.
- Compact JSON with integral floats printed as `0` rather than `0.0`.

Deliberate improvements over Python already in the tree:

- A missing `liq_blankskip` tag forces re-analysis when a non-zero blankskip was
  requested. Python would reuse stale cues.
- Format-level and stream-level tags are both merged, so FLAC and MP3 containers
  can hit the cache. Note the precedence is the reverse of Python's: here the
  audio stream wins, upstream the format wins.

Verified-identical outputs for `pkg/cue/test_data/{classic.wav,sample.ogg,tch_big.ogg}`
are pinned in `TestScanRegression`. If that test moves, the port has drifted.

## Output contract

- **stdout carries JSON and nothing else.** Liquidsoap parses the first line.
  Diagnostics, progress and flag dumps go to stderr through
  `cmd.ErrOrStderr()` or the injectable `CalculatorOptions.Diagnostics` writer.
- Loudness and gain fields are unit-suffixed strings such as `"-18.000 LUFS"`.
  Times, `liq_true_peak` and `liq_blankskip` are JSON numbers. `liq_longtail`,
  `liq_sustained_ending` and `liq_blank_skipped` are JSON booleans.
- The suffixes are applied in one place only, `Result.dto()`. Add new fields
  there so JSON, YAML and `Annotations()` stay in sync.

## Gotchas

- Tag lookups are exact-match lowercase against `verifyTags`. ffprobe preserves
  the case it finds, so ID3 `TXXX` and Opus `R128_TRACK_GAIN` often arrive
  uppercase and are silently dropped, costing a cache hit. Python lowercases
  every key first.
- `parseTags` swallows every parse error. A corrupt `liq_cue_out` becomes `0`
  with no diagnostic, which reaches Liquidsoap as a zero-length track.
- `strconv.ParseFloat` accepts `"nan"`. Python maps ebur128 `M=nan` to negative
  infinity so those frames count as silence; in Go a NaN frame fails both the
  `>` and the `<=` comparison. This matters for blankskip and sustained-ending
  detection.
- The library default execution timeout is 10s, the CLI default is 20s, and
  `gocue.liq` passes 60s. The same timeout covers both ffprobe and ffmpeg.
- ffmpeg's stderr is discarded, so a scan failure surfaces only as an exit
  status. Attach a capture buffer when debugging a scan.
- `--nice` means pretty-print JSON here. In Python it means run under `nice(1)`.
  `gocue.liq` bridges the difference by invoking `nice` itself.

## Conventions

- Analysis parameters live on the `Calculator` struct, set once by
  `NewCalculator`. Flags live on a per-invocation `options` struct built by
  `newRootCmd`, never as package globals, so commands stay reentrant.
- `Calculator` is safe for concurrent `Calc` calls and `TestScanConcurrent`
  enforces that under `-race`. Keep new state per-call or immutable.
- Fixtures are committed audio. Do not add more large binaries; the repo is
  already heavy. Anything generated at the repo root, such as `nice_out.mp3`,
  is stray and should not be committed.
- The version string is duplicated in `Makefile`, the README badge,
  `settings.gocue.version` in `gocue.liq`, the ldflags in
  `integration/harness_test.go`, and the fallback in `cmd/cue/cue.go`. Update
  them together.
- `gocue.liq` ships to production despite living under `integration/`. Treat
  changes to it as product changes and mirror them in the README section that
  documents it.
