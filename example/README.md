# Example station

A complete, listenable radio station in two containers. It plays audio from a
folder on your machine, streams it as Ogg Vorbis to Icecast, and uses **gocue**
built from this repository for cue points, crossfade timing and EBU R128
loudness.

It exists for two reasons. It is the worked example the top-level README only
sketches, and it is a manual test rig: change something in `pkg/cue`, run
`make example-up`, and hear the result.

## What it demonstrates

- gocue compiled from this tree and driving a real Liquidsoap station.
- The production autocue provider, [`integration/scripts/gocue.liq`](../integration/scripts/gocue.liq),
  used as-is. The image copies that exact file, so the example cannot drift from
  what the integration suite tests.
- Cue-in and cue-out trimming silence, and radio-style crossfades: four seconds
  out under a two-second fade in, or longer where the track genuinely fades.
- Loudness levelling across tracks recorded decades apart.
- Three layers of metadata merging without overwriting each other.
- Ogg Vorbis encoding and Icecast delivery, including stream titles.

## Prerequisites

- Docker with Compose v2. Nothing else. Go, Liquidsoap and ffmpeg all live
  inside the images.
- A folder of audio files. Anything ffmpeg can decode works: MP3, FLAC, Ogg,
  Opus, M4A, WAV.

Both images have native `linux/arm64` and `linux/amd64` builds, so Apple silicon
runs without emulation.

## Setup

**1. Create your local config.**

```bash
cp example/.env.example example/.env
```

**2. Point `MUSIC_DIR` at your audio.** Edit `example/.env`:

```sh
MUSIC_DIR=/absolute/path/to/your/audio/folder
```

It must be an absolute path. The folder is mounted read-only at `/music` inside
the container, so the station can never modify your files.

While you are in there, change the Icecast passwords from `change-me`.

**3. Start it.**

```bash
make example-up
```

That generates the playlist, builds the gocue binary from the current working
tree, and starts both containers. The first build takes a couple of minutes;
later runs are cached.

## Listening

| What | Where |
|------|-------|
| The stream | `http://localhost:8000/radio.ogg` |
| Icecast status page | `http://localhost:8000/` |
| Current track metadata | `http://localhost:8080/metadata` |
| Health | `http://localhost:8080/health` |
| Skip to next track | `http://localhost:8080/skip` |

Open the stream in VLC, or from a terminal:

```bash
ffplay -nodisp http://localhost:8000/radio.ogg
```

Watch what the station is doing:

```bash
make example-logs
```

Stop everything:

```bash
make example-down
```

## The playlist

`make example-playlist` scans `MUSIC_DIR` and writes `example/media/playlist.pls`.
It runs automatically as part of `make example-up`, so you only need it directly
after adding or removing files.

Two details matter:

- The paths inside the playlist are **container** paths (`/music/...`), never
  paths from your machine. Nothing about your host layout ends up in the file.
- The generator writes no `TitleN` or `LengthN` entries. Those would override
  what the decoder reads from the file, and the point here is to show the real
  tags surviving.

The playlist is regenerated, never edited by hand, and never committed.

## How gocue is wired in

`example/scripts/station.liq` includes `gocue.liq`, which registers gocue as a
Liquidsoap autocue provider. `enable_autocue_metadata()` then routes every
request through it.

For each track, before it plays:

1. Liquidsoap calls the `gocue` binary with the flags built from
   `settings.gocue.*`.
2. gocue probes the file's existing tags. If they are complete it returns
   immediately. Otherwise it runs a full ffmpeg ebur128 analysis, roughly a
   second for a three-minute track.
3. gocue prints one line of JSON on stdout. `gocue.liq` parses it, recomputes
   gain against your target, validates the crossfade window, and hands
   Liquidsoap the cue points.

Every `settings.gocue.*` value in the script comes from an environment variable,
so you can change any of them in `.env` without touching Liquidsoap code.

### Reading the logs

Each track start prints one summary line:

```
STATION_TRACK n=2 artist=Benny Goodman Sextet title=Soft Winds cue_in=0.0
  cue_out=147.9 fade_out=0.8 cross_end=0.8 amplify=1.32 dB autocue=gocue
  show=Early 40s mood=smoky
```

- `cue_in` and `cue_out` are where playback starts and ends, with silence
  trimmed.
- `amplify` is the gain applied to reach your loudness target.
- `autocue=gocue-radio` confirms the provider handled the track. If it is
  empty, something in the chain failed and Liquidsoap fell back to the raw file.
- `cross_end` is how many seconds before cue-out the next track begins. See
  "Shaping the transitions" below for why it is at least four.

## Shaping the transitions

gocue finds the overlay point in the audio itself: it scans backwards for where
the momentary loudness falls below your `GOCUE_OVERLAY` threshold, which is the
right answer for a track that tapers off.

Plenty of music does not taper. These 1940s transfers sit at full level and then
drop around 60 LU in half a second:

```
t= 191.4  M=-21.9      <- still at full level
t= 191.8  M=-46.4
t= 192.3  M=-58.5
t= 193.4  M=-110.3     <- gone
```

No threshold can find a fade that was never recorded. Every overlay value from
−8 down to −2 LU puts the overlay point under a second before cue-out on this
material, and the result sounds like a track being cut off rather than a radio
segue.

So the station imposes a floor of its own, in `radio_autocue` in
`station.liq`. It wraps the gocue provider rather than replacing it:

```liquidsoap
start_next = max(cue_in + fade_in, min(gocue_start_next, cue_out - max_cross))
```

The next track begins at gocue's point **or** `STATION_MAX_CROSS` seconds before
cue-out, whichever comes first. A track with a real fade keeps its longer,
natural overlap. A track that ends cold gets a crossfade anyway.

On a genuinely long-tailed recording, for example, gocue puts the overlay 11.7
seconds before cue-out and the floor at 4.0; `min` keeps 11.7 and nothing is
truncated. On these 1940s sides, the natural point is 0.8 seconds and the floor
wins.

The incoming track then fades up over `GOCUE_FADE_IN` seconds, bounded by the
overlap, so the two never fight. With the defaults you get a four-second fade
out under a two-second fade in.

This is also a small worked example of wrapping an autocue provider:
`autocue.register` takes any function with the right signature, so you can
post-process gocue's analysis without forking `gocue.liq`.

## The three metadata layers

The station adds metadata in three ways, and `GET /metadata` shows them
together. Nothing overwrites anything else.

**Layer 1, the file's own tags.** Whatever ffmpeg reads from the file: artist,
title, album, genre, date, comment. Untouched.

**Layer 2, the `annotate:` protocol.** Liquidsoap's metadata protocol attaches
fields to a *request*. The playlist generator wraps every entry:

```
File7=annotate:station_show="Early 40s",rotation_slot="A",playlist_position="7":/music/108.mp3
```

Those fields exist before the file is even opened, which makes them the right
place for scheduling information: which show a track belongs to, which rotation
slot it fills.

Keep `liq_*` keys out of annotations here unless you mean them.
`gocue.liq` reads those as control input, and `liq_cue_file="false"` would switch
analysis off for that track entirely.

**Layer 3, `metadata.map` at play time.** Fields that can only be computed when
the track actually starts:

```liquidsoap
radio = metadata.map(update=true, strip=false, add_station_metadata, radio)
```

`update=true` is what makes this additive: the function returns only the keys it
wants to add, and Liquidsoap merges them into the existing metadata. The example
adds `station`, `played_at` as a timestamp, `decade` derived from the file's date
tag, and a randomly chosen `mood`, to show that values can be dynamic rather
than fixed per file.

One honest wrinkle: `decade` is derived from whatever `date` tag the file
carries, which for a reissue is often the reissue year rather than the recording
year. That is the tag doing what tags do, not a bug in the derivation.

### Making custom fields reach listeners

Getting a field into Liquidsoap's metadata is only half the job. Encoders export
an **allowlist**, and custom keys are not on it. Without this, `station_show` and
friends show up at `/metadata` and in the logs, then vanish from the actual
stream, which is a confusing way to lose an afternoon:

```liquidsoap
settings.encoder.metadata.export :=
  [...settings.encoder.metadata.export(), "station", "station_show", ...]
```

Spreading the current value rather than assigning a fresh list keeps the
defaults, `artist`, `title`, `album` and the rest, instead of silently dropping
them.

To check what a listener really receives, capture from the stream and read the
Ogg comments:

```bash
curl -s --max-time 10 http://localhost:8000/radio.ogg -o /tmp/s.ogg
ffprobe -v error -show_entries format_tags:stream_tags /tmp/s.ogg
```

If that comes back with nothing but `ENCODER`, your capture started in the
middle of a track. Ogg carries its comment header at the start of each logical
stream, and Liquidsoap opens a new one per track, so capture across a track
change. Hitting `/skip` a second or two into the capture is the quickest way.

## Why tag write-back is off

`gocue.liq` can write its results back into your audio files as tags, so later
runs skip analysis entirely. This example sets `write_tags := false` and mounts
your music read-only, for two reasons.

The obvious one: an example should not modify the library you pointed it at.

The other one: the write-back path in `gocue.liq` currently remuxes with
`ffmpeg -map_metadata -1`, which drops every tag that is not a `liq_*` or
`replaygain_*` key. Artist, title and album do not survive. Until that is fixed,
do not enable `write_tags` on anything you care about.

The cost of leaving it off is that every restart re-analyses every track it
plays. At roughly a second per track, on demand, that is not noticeable.

## What is never committed

| Item | Where it lives | Why |
|------|----------------|-----|
| Your audio files | Wherever `MUSIC_DIR` points | Not ours to distribute |
| The path to them | `example/.env` only | Machine-specific |
| `playlist.pls` | `example/media/`, git-ignored | Generated, contains nothing portable |
| Icecast passwords | `example/.env` only | Secrets |

`example/.env.example` is committed and contains placeholders only. If you ever
need to check, `git check-ignore -v example/.env example/media/playlist.pls`
confirms both are excluded.

## Tuning

Every knob lives in `example/.env` and maps to a gocue flag. Restart with
`make example-down && make example-up` to apply.

| Variable | Default | gocue flag | Effect |
|----------|---------|-----------|--------|
| `GOCUE_TARGET` | `-18.0` | `-t` | Loudness target in LUFS |
| `GOCUE_SILENCE` | `-42.0` | `-s` | LU below track loudness counted as silence, sets cue-in and cue-out |
| `GOCUE_OVERLAY` | `-8.0` | `-o` | LU below track loudness that triggers the next track |
| `GOCUE_LONGTAIL` | `15.0` | `-l` | Overlay longer than this many seconds counts as a long tail |
| `GOCUE_EXTRA` | `-12.0` | `-x` | Extra LU used when recalculating long tails and sustained endings |
| `GOCUE_DROP` | `40.0` | `-d` | Maximum percent loudness drop still counted as a sustained ending |
| `GOCUE_BLANKSKIP` | `0.0` | `-b` | Cue out early on in-track silence this long. Off by default |
| `GOCUE_NOCLIP` | `true` | `-k` | Reduce gain so true peak stays at or below −1 dBFS |
| `GOCUE_TIMEOUT` | `120.0` | `-e` | Analysis timeout per track |
| `GOCUE_FADE_IN` | `2.0` | n/a | Fade-in for the incoming track, bounded by the overlap |
| `GOCUE_FADE_OUT` | `4.0` | n/a | Requested fade-out, bounded by the overlap |
| `STATION_MAX_CROSS` | `4.0` | n/a | Latest the next track may start before cue-out. See "Shaping the transitions" |

Hear the difference: raise `STATION_MAX_CROSS` to `8.0` for long, lazy segues,
or drop it to `1.0` to hear how abrupt the unshaped transitions were.

## Troubleshooting

**`example/.env is missing`.** Run `cp example/.env.example example/.env` and set
`MUSIC_DIR`.

**`no audio files found`.** The generator only looks one level deep. Point
`MUSIC_DIR` at the folder that directly contains the files, not a parent.

**Nothing plays and the log shows Icecast connection errors.** Expected for a few
seconds on a cold start, because the station comes up before Icecast is
accepting sources. It retries every five seconds. If it persists, check that
`ICECAST_SOURCE_PASSWORD` is identical for both services in `.env`.

**`autocue=` is empty in the logs.** gocue failed for that track. The station log
carries gocue's own stderr, which names the file and the reason.

**Port already in use.** Change `ICECAST_PUBLIC_PORT` or `STATION_HARBOR_PORT`
in `.env`.

**The first track takes a few seconds to start.** It is being analysed. Later
tracks are analysed ahead of time, since `settings.request.prefetch` is 2.

## Taking this to production

The shape is already right, but three things would change. Turn on tag
write-back once the metadata-stripping bug above is fixed, so restarts stop
re-analysing. Put Icecast behind TLS and set real passwords. And replace the
`.pls` file with whatever scheduling your station actually uses; the autocue
wiring does not care where requests come from.
