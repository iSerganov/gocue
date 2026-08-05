# Integration tests

End-to-end tests that drive the real Liquidsoap `gocue.liq` autocue provider
against the `gocue` binary, with fixtures under `testdata/`.

## Requirements

- `liquidsoap` (≥ 2.2.5; developed against 2.4.x)
- `ffmpeg` / `ffprobe`
- Go toolchain (builds `integration/.bin/gocue` automatically)

## Run

```bash
# from repo root
go test -tags=integration ./integration/ -count=1 -timeout 20m

# verbose
go test -tags=integration ./integration/ -v -count=1 -timeout 20m
```

Unit tests (`go test ./...`) skip this package (build tag `integration`).

## Layout

```
integration/
├── scripts/
│   ├── gocue.liq           # Liquidsoap autocue provider for gocue
│   ├── harness.liq         # metadata dump (file or directory)
│   └── station.liq         # minimal radio station for playlist tests
├── testdata/               # audio fixtures (+ free/ PD long tracks)
├── harness_test.go
├── integration_test.go
├── station_test.go
└── README.md
```

## What is validated

- Cue-in / cue-out / cross-start-next vs direct `gocue` CLI
- Fade-in / fade-out after Liquidsoap post-processing
- Parameter forwarding (`target`, `blankskip`, `noclip`, …)
- `annotate:` metadata overrides (e.g. `liq_fade_out`)
- Tag write-back via the Liquidsoap script (`write_tags` → ffmpeg) and
  ffprobe round-trip
- Skip path when `liq_gocue=false`
- Full metadata for every track in `testdata/free/` via `harness.liq`
- Playlist playback order on `station.liq`

Note: **gocue itself does not write tags**; `gocue.liq` writes them with
ffmpeg when `settings.gocue.write_tags` is enabled.
