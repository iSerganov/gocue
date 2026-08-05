# Uses gocue.liq
#
# Environment:
#   GOCUE_BIN           path to gocue (required)
#   STATION_PLAYLIST    path to .m3u/.pls (required)
#   STATION_HARBOR_PORT HTTP harbor port (default 19000)
#   STATION_OUTPUT      output wav path (default /tmp/gocue-station-out.wav)
#   GOCUE_*             same analysis knobs as harness.liq
#
# HTTP:
#   GET  /metadata  → current track metadata JSON
#   GET  /health    → {"ok":true} when playlist source is up
#   POST /skip      → skip to next track
#   POST /shutdown  → graceful shutdown

settings.log.stdout := false
settings.log.file := false
settings.log.level := 3
settings.request.prefetch := 2
settings.decoder.priorities.ffmpeg := 10

%include "gocue.liq"

def env_float(name, default) =
  float_of_string(default=default, environment.get(default="", name))
end

def env_bool(name, default) =
  v = string.case(lower=true, environment.get(default="", name))
  if v == "true" or v == "1" or v == "yes" then true
  elsif v == "false" or v == "0" or v == "no" then false
  else default end
end

bin = environment.get(default="", "GOCUE_BIN")
playlist_path = environment.get(default="", "STATION_PLAYLIST")
harbor_port = int_of_string(environment.get(default="19000", "STATION_HARBOR_PORT"))
output_path = environment.get(default="/tmp/gocue-station-out.wav", "STATION_OUTPUT")

if bin == "" then
  print("ERROR: GOCUE_BIN is required")
  shutdown(code=2)
end
if playlist_path == "" then
  print("ERROR: STATION_PLAYLIST is required")
  shutdown(code=2)
end

settings.gocue.path := bin
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
settings.gocue.write_tags := false
settings.gocue.unify_loudness_correction := true
settings.autocue.preferred := "gocue"
settings.autocue.metadata.priority := 10
settings.autocue.amplify_behavior := "keep"
settings.fade.out.duration := settings.gocue.fade_out()
settings.autocue.internal.metadata_override := []

enable_autocue_metadata()

track_count = ref(0)
current_meta = ref([])

def on_done() =
  # Do not auto-shutdown: the Go harness inspects /metadata then POSTs /shutdown.
  print("STATION_PLAYLIST_DONE")
end

def on_fail() =
  print("STATION_PLAYLIST_FAIL")
  []
end

myplaylist =
  playlist(
    playlist_path,
    mime_type="audio/x-mpegurl",
    reload_mode="never",
    mode="normal",
    loop=false,
    on_done=on_done,
    max_fail=3,
    on_fail=on_fail
  )

radio = amplify(1., override="liq_amplify", myplaylist)
radio = crossfade(radio)
radio = fallback(track_sensitive=false, [radio, blank(duration=1.)])

def on_track_change(m) =
  track_count := track_count() + 1
  full = metadata.cover.remove(m)
  current_meta := list.add(("on_air_timestamp", "#{time()}"), full)
  title = m["title"] ?? ""
  artist = m["artist"] ?? ""
  uri = m["initial_uri"] ?? (m["filename"] ?? "")
  cue_in = m["liq_cue_in"] ?? ""
  cue_out = m["liq_cue_out"] ?? ""
  fade_in = m["liq_fade_in"] ?? ""
  fade_out = m["liq_fade_out"] ?? ""
  cross_next = m["liq_cross_start_next"] ?? ""
  print(
    "STATION_TRACK n=#{track_count()} artist=#{artist} title=#{title} \
     cue_in=#{cue_in} cue_out=#{cue_out} fade_in=#{fade_in} \
     fade_out=#{fade_out} cross=#{cross_next} uri=#{uri}"
  )
end

radio.on_metadata(on_track_change, synchronous=true)

def get_meta(_, response) =
  let m = current_meta()
  response.json({
    track = track_count(),
    artist = m["artist"] ?? "",
    title = m["title"] ?? "",
    album = m["album"] ?? "",
    duration = m["duration"] ?? "",
    uri = m["initial_uri"] ?? (m["filename"] ?? ""),
    liq_amplify = m["liq_amplify"] ?? "",
    liq_loudness = m["liq_loudness"] ?? "",
    liq_cue_in = m["liq_cue_in"] ?? "",
    liq_cue_out = m["liq_cue_out"] ?? "",
    liq_fade_in = m["liq_fade_in"] ?? "",
    liq_fade_out = m["liq_fade_out"] ?? "",
    liq_cross_start_next = m["liq_cross_start_next"] ?? "",
    liq_cross_max_start_duration = m["liq_cross_max_start_duration"] ?? "",
    liq_autocue = m["liq_autocue"] ?? "",
    on_air_timestamp = m["on_air_timestamp"] ?? ""
  })
end

def health(_) =
  if myplaylist.is_up() then
    http.response(
      status_code=200,
      headers=[("Content-Type", "application/json; charset=utf-8")],
      data=json.stringify({ok = true, track = track_count()})
    )
  else
    http.response(
      status_code=500,
      headers=[("Content-Type", "application/json; charset=utf-8")],
      data=json.stringify({ok = false, track = track_count()})
    )
  end
end

def do_skip(_, response) =
  source.skip(radio)
  response.json({ok = true, action = "skip"})
end

def do_shutdown(_, response) =
  response.json({ok = true, action = "shutdown"})
  # Delay shutdown so the HTTP response can flush.
  thread.run(delay=0.2, shutdown)
end

harbor.http.middleware.register(harbor.http.middleware.cors(origin="*"))
harbor.http.register(port=harbor_port, method="GET", "/metadata", get_meta)
harbor.http.register.simple(port=harbor_port, "/health", health)
harbor.http.register(port=harbor_port, method="GET", "/skip", do_skip)
harbor.http.register(port=harbor_port, method="POST", "/skip", do_skip)
harbor.http.register(port=harbor_port, method="GET", "/shutdown", do_shutdown)
harbor.http.register(port=harbor_port, method="POST", "/shutdown", do_shutdown)

print("STATION_READY playlist=#{playlist_path} harbor=#{harbor_port} output=#{output_path}")

# File output (no Icecast dependency in tests)
output.file(%wav, output_path, radio, fallible=true)
