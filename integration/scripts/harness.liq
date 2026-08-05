# Integration harness: run file.autocue.metadata for one or more URIs and emit
# KEY\tVALUE lines between markers so Go tests can parse them reliably.
# Each file produces its own BEGIN_META … END_META block (includes a "file" key).
#
# Environment:
#   GOCUE_BIN              path to gocue (required)
#   GOCUE_TARGET           LUFS target (default -18)
#   GOCUE_SILENCE          silence LU (default -42)
#   GOCUE_OVERLAY          overlay LU (default -8)
#   GOCUE_LONGTAIL         longtail seconds (default 15)
#   GOCUE_EXTRA            overlay_longtail LU (default -12)
#   GOCUE_DROP             sustained drop % (default 40)
#   GOCUE_BLANKSKIP        blankskip seconds (default 0)
#   GOCUE_FADE_IN          fade-in seconds (default 0.1)
#   GOCUE_FADE_OUT         fade-out seconds (default 2.5)
#   GOCUE_NOCLIP           "true"/"false" (default false)
#   GOCUE_WRITE_TAGS       "true"/"false" (default false)
#   GOCUE_WRITE_REPLAYGAIN "true"/"false" (default false)
#   GOCUE_ANNOTATE         optional annotate: prefix metadata, e.g.
#                          liq_fade_out=1.0,liq_cue_out=4.0
#
# Usage:
#   liquidsoap harness.liq -- /path/to/audio.flac
#   liquidsoap harness.liq -- /path/to/dir          # all audio files in dir
#   liquidsoap harness.liq -- a.ogg b.mp3 …

# Keep stdout reserved for BEGIN_META/END_META; diagnostics go to stderr via log.
settings.log.stdout := false
settings.log.file := false
settings.log.level := 2

%include "gocue.liq"

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

bin = environment.get(default="", "GOCUE_BIN")
if bin == "" then
  print("ERROR: GOCUE_BIN is required")
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
settings.gocue.noclip := env_bool("GOCUE_NOCLIP", false)
settings.gocue.write_tags := env_bool("GOCUE_WRITE_TAGS", false)
settings.gocue.write_replaygain := env_bool("GOCUE_WRITE_REPLAYGAIN", false)
settings.gocue.timeout := env_float("GOCUE_TIMEOUT", 60.0)
settings.gocue.unify_loudness_correction := true

settings.autocue.preferred := "gocue"
settings.autocue.metadata.priority := 10
settings.autocue.amplify_behavior := "keep"
settings.fade.out.duration := settings.gocue.fade_out()

annotate = environment.get(default="", "GOCUE_ANNOTATE")

audio_exts = [".flac", ".ogg", ".mp3", ".wav", ".m4a", ".opus"]

def is_audio_file(p) =
  lower = string.case(lower=true, p)
  list.exists(fun (ext) -> string.contains(suffix=ext, lower), audio_exts)
end

def expand_arg(arg) =
  if not file.exists(arg) then
    print("ERROR: path not found: #{arg}")
    shutdown(code=2)
  end
  if file.is_directory(arg) then
    list.sort.natural(
      list.filter(
        is_audio_file,
        list.map(fun (name) -> "#{arg}/#{name}", file.ls(arg))
      )
    )
  else
    [arg]
  end
end

# Collect argv(1..) into a single file list (files and/or directories).
files = ref([])
arg_i = ref(1)
current = ref(argv(default="", arg_i()))
while current() != "" do
  files := list.append(files(), expand_arg(current()))
  arg_i := arg_i() + 1
  current := argv(default="", arg_i())
end

if list.length(files()) == 0 then
  print("ERROR: audio file or directory required as argument(s) after --")
  shutdown(code=2)
end

def dump_meta(uri) =
  request_uri =
    if annotate == "" then
      uri
    else
      'annotate:#{annotate}:#{uri}'
    end
  m = file.autocue.metadata(request_metadata=[], request_uri)
  print("BEGIN_META")
  print("file\t#{uri}")
  list.iter(
    fun (kv) -> begin
      let (k, v) = kv
      print("#{k}\t#{v}")
    end,
    list.sort.natural(m)
  )
  print("END_META")
end

list.iter(dump_meta, files())
shutdown()
