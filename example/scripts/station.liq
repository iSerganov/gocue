# gocue example station.
#
# Plays a local playlist to Icecast as Ogg Vorbis. Cue points, crossfades and
# loudness come from gocue through the same gocue.liq provider the integration
# suite exercises.
#
# Environment (see example/.env.example):
#   GOCUE_BIN             gocue binary            (default /usr/local/bin/gocue)
#   STATION_PLAYLIST      .pls to play            (default /media/playlist.pls)
#   STATION_HARBOR_PORT   control endpoints       (default 8080)
#   STATION_NAME | _DESCRIPTION | _GENRE | _URL
#   ICECAST_HOST | ICECAST_PORT | ICECAST_MOUNT | ICECAST_SOURCE_PASSWORD
#   GOCUE_*               analysis knobs, same names as the integration harness
#
# HTTP on the harbor port:
#   GET       /metadata   current track metadata as JSON
#   GET       /health     {"ok":true} while the playlist source is up
#   GET|POST  /skip       skip to the next track

settings.log.stdout := true
settings.log.file := false
settings.log.level := 3
settings.request.prefetch := 2
settings.decoder.priorities.ffmpeg := 10

%include "gocue.liq"

def env_str(name, default) =
  v = environment.get(default="", name)
  if v == "" then default else v end
end

def env_float(name, default) =
  float_of_string(default=default, environment.get(default="", name))
end

def env_bool(name, default) =
  v = string.case(lower=true, environment.get(default="", name))
  if v == "true" or v == "1" or v == "yes" then
    true
  elsif v == "false" or v == "0" or v == "no" then
    false
  else
    default
  end
end

playlist_path = env_str("STATION_PLAYLIST", "/media/playlist.pls")
harbor_port = int_of_string(env_str("STATION_HARBOR_PORT", "8080"))

station_name = env_str("STATION_NAME", "gocue example radio")
station_description =
  env_str("STATION_DESCRIPTION", "Autocue points and EBU R128 loudness by gocue")
station_genre = env_str("STATION_GENRE", "Oldies")
station_url = env_str("STATION_URL", "https://github.com/iSerganov/gocue")

icecast_host = env_str("ICECAST_HOST", "icecast")
icecast_port = int_of_string(env_str("ICECAST_PORT", "8000"))
icecast_mount = env_str("ICECAST_MOUNT", "/radio.ogg")
icecast_password = env_str("ICECAST_SOURCE_PASSWORD", "hackme")

# ---------------------------------------------------------------------------
# gocue wiring. Every knob here maps to a gocue command-line flag; gocue.liq
# builds the invocation and parses the JSON it prints on stdout.
# ---------------------------------------------------------------------------
settings.gocue.path := env_str("GOCUE_BIN", "/usr/local/bin/gocue")
settings.gocue.target := env_float("GOCUE_TARGET", -18.0)
settings.gocue.silence := env_float("GOCUE_SILENCE", -42.0)
settings.gocue.overlay := env_float("GOCUE_OVERLAY", -8.0)
settings.gocue.longtail := env_float("GOCUE_LONGTAIL", 15.0)
settings.gocue.overlay_longtail := env_float("GOCUE_EXTRA", -12.0)
settings.gocue.sustained_loudness_drop := env_float("GOCUE_DROP", 40.0)
settings.gocue.blankskip := env_float("GOCUE_BLANKSKIP", 0.0)
settings.gocue.fade_in := env_float("GOCUE_FADE_IN", 0.1)
settings.gocue.fade_out := env_float("GOCUE_FADE_OUT", 2.5)
settings.gocue.noclip := env_bool("GOCUE_NOCLIP", true)
settings.gocue.timeout := env_float("GOCUE_TIMEOUT", 120.0)
settings.gocue.unify_loudness_correction := true

# Never on in this example: it would rewrite the files you mounted. See the
# README section "Why tag write-back is off".
settings.gocue.write_tags := false
settings.gocue.write_replaygain := false

ignore(check_autocue_setup(do_print=true))

# ---------------------------------------------------------------------------
# Radio-style transitions.
#
# gocue finds the overlay point in the audio itself, which is exactly right
# when a track tapers off. Plenty of material does not taper: these 1940s
# transfers sit at full level and then drop some 60 LU in half a second, so the
# natural overlap works out under a second and every track ends on an audible
# clip. No threshold can find a fade that was never recorded.
#
# So impose a floor. Start the next track at gocue's point, or STATION_MAX_CROSS
# seconds before cue-out, whichever comes first. Tracks that really do fade keep
# their longer, natural overlap; cold endings get a crossfade regardless.
# ---------------------------------------------------------------------------
max_cross = env_float("STATION_MAX_CROSS", 4.0)

def radio_autocue(~request_metadata, ~file_metadata, filename) =
  analysed =
    gocue(
      request_metadata=request_metadata,
      file_metadata=file_metadata,
      filename
    )
  if null.defined(analysed) then
    cue = null.get(analysed)
    fade_in = settings.gocue.fade_in()
    # Never start the overlap before the track's own fade-in has finished.
    earliest = cue.cue_in + fade_in
    start_next = max(earliest, min(cue.start_next, cue.cue_out - max_cross))
    overlap = cue.cue_out - start_next
    cue.{
      start_next = start_next,
      fade_out = min(max_cross, overlap),
      fade_in = min(fade_in, overlap)
    }
  else
    analysed
  end
end

autocue.register(name="gocue-radio", radio_autocue)
settings.autocue.preferred := "gocue-radio"

enable_autocue_metadata()

track_count = ref(0)
current_meta = ref([])

# ---------------------------------------------------------------------------
# Source. The .pls carries one annotate: URI per entry, which is metadata
# layer 2 (see the README). Layer 1 is whatever ID3 tags the file already has.
# ---------------------------------------------------------------------------
radio =
  playlist(
    playlist_path,
    mime_type="audio/x-scpls",
    reload_mode="watch",
    mode="randomize",
    loop=true,
    max_fail=5
  )

# ---------------------------------------------------------------------------
# Metadata layer 3: fields computed while the track is starting.
#
# metadata.map with update=true merges what this function returns into the
# metadata already attached, so the file's own tags and the annotate: fields
# both survive. Only the keys returned here are added or overwritten.
# ---------------------------------------------------------------------------
moods = ["mellow", "swinging", "wistful", "upbeat", "smoky"]

def add_station_metadata(m) =
  recorded = m["date"] ?? ""
  decade =
    if string.length(recorded) >= 4 then
      string.sub(recorded, start=0, length=3) ^ "0s"
    else
      "unknown"
    end
  mood =
    list.nth(
      default="mellow",
      moods,
      random.int(min=0, max=list.length(moods))
    )
  [
    ("station", station_name),
    ("played_at", string.float(decimal_places=0, time())),
    ("decade", decade),
    ("mood", mood)
  ]
end

radio = metadata.map(update=true, strip=false, add_station_metadata, radio)

# Encoders export an allowlist of metadata fields, and custom keys are not on
# it by default, so they reach /metadata but never the Ogg comments a listener
# receives. Append ours to the defaults rather than replacing them.
settings.encoder.metadata.export :=
  [
    ...settings.encoder.metadata.export(),
    "station",
    "station_show",
    "rotation_slot",
    "playlist_position",
    "played_at",
    "decade",
    "mood"
  ]

radio = amplify(1., override="liq_amplify", radio)
radio = crossfade(radio)
radio = fallback(track_sensitive=false, [radio, blank(duration=1.)])

def on_track_change(m) =
  track_count := track_count() + 1
  current_meta := metadata.cover.remove(m)
  artist = m["artist"] ?? ""
  title = m["title"] ?? ""
  cue_in = m["liq_cue_in"] ?? ""
  cue_out = m["liq_cue_out"] ?? ""
  fade_out = m["liq_fade_out"] ?? ""
  cross_end = m["liq_cross_end_duration"] ?? ""
  amp = m["liq_amplify"] ?? ""
  provider = m["liq_autocue"] ?? ""
  show = m["station_show"] ?? ""
  mood = m["mood"] ?? ""
  print(
    "STATION_TRACK n=#{track_count()} artist=#{artist} title=#{title} \
     cue_in=#{cue_in} cue_out=#{cue_out} fade_out=#{fade_out} \
     cross_end=#{cross_end} amplify=#{amp} autocue=#{provider} \
     show=#{show} mood=#{mood}"
  )
end

radio.on_metadata(on_track_change, synchronous=true)

# ---------------------------------------------------------------------------
# Control endpoints.
# ---------------------------------------------------------------------------
def get_meta(_, response) =
  obj = json.object()
  obj.add("track", track_count())
  list.iter(fun (kv) -> obj.add(fst(kv), snd(kv)), current_meta())
  response.http_version("1.1")
  response.content_type("application/json; charset=utf-8")
  response.data(obj.stringify())
end

def health(_) =
  ok = source.is_ready(radio)
  http.response(
    status_code=if ok then 200 else 503 end,
    headers=[("Content-Type", "application/json; charset=utf-8")],
    data=json.stringify({ok = ok, track = track_count()})
  )
end

def do_skip(_, response) =
  source.skip(radio)
  response.json({ok = true, action = "skip"})
end

harbor.http.middleware.register(harbor.http.middleware.cors(origin="*"))
harbor.http.register(port=harbor_port, method="GET", "/metadata", get_meta)
harbor.http.register.simple(port=harbor_port, "/health", health)
harbor.http.register(port=harbor_port, method="GET", "/skip", do_skip)
harbor.http.register(port=harbor_port, method="POST", "/skip", do_skip)

# ---------------------------------------------------------------------------
# Output. Ogg Vorbis to Icecast. fallible + on_error keeps the station alive
# when Icecast is not accepting sources yet, which happens on a cold start.
# ---------------------------------------------------------------------------
def on_icecast_error(~restart_in, err) =
  log.important(label="station", "Icecast error: #{err}, retrying in 5s")
  restart_in(5.)
end

output.icecast(
  %vorbis(quality=1, channels=2, samplerate=48000),
  host=icecast_host,
  port=icecast_port,
  password=icecast_password,
  mount=icecast_mount,
  name=station_name,
  description=station_description,
  genre=station_genre,
  url=station_url,
  fallible=true,
  on_error=on_icecast_error,
  radio
)

print(
  "STATION_READY playlist=#{playlist_path} \
   icecast=http://#{icecast_host}:#{icecast_port}#{icecast_mount} \
   harbor=#{harbor_port}"
)
