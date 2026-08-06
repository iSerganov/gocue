# Initialize settings for gocue implementation
let settings.gocue = ()

# Internal only! Not a user setting.
let settings.gocue.version =
  settings.make(
    description=
      "Software version of this gocue script.",
    "1.1.2"
  )

let settings.gocue.path =
  settings.make(
    description=
      "Path of the gocue binary.",
    "gocue"
  )

let settings.gocue.fade_in =
  settings.make(
    description=
      "Default fade-in duration if not specified by the user.",
    0.1
  )

let settings.gocue.fade_out =
  settings.make(
    description=
      "Default fade-out duration if not specified by the user.",
    2.5
  )

let settings.gocue.timeout =
  settings.make(
    description=
      "Timeout (in seconds) for gocue executions.",
    60.0
  )

let settings.gocue.target =
  settings.make(
    description=
      "Loudness target in LUFS.",
    -18.0
  )

let settings.gocue.silence =
  settings.make(
    description=
      "Silence level (for cue points) in LU below track loudness.",
    -42.0
  )

let settings.gocue.overlay =
  settings.make(
    description=
      "Start overlay level in LU below track loudness.",
    -8.0
  )

let settings.gocue.longtail =
  settings.make(
    description=
      "More than so many seconds of calculated overlay are considered a long \
       tail.",
    15.0
  )

let settings.gocue.overlay_longtail =
  settings.make(
    description=
      "Extra LU level below overlay loudness, to recalculate songs with long \
       tails.",
    -12.0
  )

let settings.gocue.sustained_loudness_drop =
  settings.make(
    description=
      "Consider track to have a sustained ending if its loudness at the end \
      does NOT drop more than so many percent. Otherwise, it has a hard ending.",
    40.0
  )

let settings.gocue.noclip =
  settings.make(
    description=
      "Clipping prevention: Lowers track gain if needed, to avoid peaks \
       going above -1 dBFS. Uses true peak values of all audio channels.",
    false
  )

let settings.gocue.blankskip =
  settings.make(
    description=
      "Skip blank (silence) within track if longer than `blankskip` seconds \
       (get rid of \"hidden tracks\"). \
       Sets the cue-out point to where the silence begins. Don't use this \
       with spoken or TTS-generated text, as it will often cut the message \
       short. Zero (0.0) to switch off.",
    0.0
  )

let settings.gocue.unify_loudness_correction =
  settings.make(
    description=
      'Unify `replaygain_track_gain` and `liq_amplify`. If enabled, this will \
       ensure both have the same value, with `replaygain_track_gain` taking \
       precedence if seen, and we have a `replaygain_reference_loudness`. \
       Allows scripts to amplify on either value, without loudness jumps.',
    true
  )

let settings.gocue.write_tags =
  settings.make(
    description=
      "Write back `liq_*` tags to original audio file. Ensure you have enough \
       free space to hold a copy of the original file. \
       gocue never mutates files, so this script performs the write via ffmpeg.",
    false
  )

let settings.gocue.write_replaygain =
  settings.make(
    description=
      "Write ReplayGain tags to file (track only, no album). Useful if your \
       files have no previous RG tags. Only valid if `write_tags` is also true.",
    false
  )

let settings.gocue.force_analysis =
  settings.make(
    description=
      'Force re-analysis even when all needed data could be read from file tags. \
       Not supported by gocue; kept for configuration compatibility.',
    false
  )

let settings.gocue.nice =
  settings.make(
    description=
      'Linux/MacOS only: Use nice for `gocue` operations?',
    false
  )

let settings.gocue.use_json_metadata =
  settings.make(
    description=
      'Send metadata to `gocue` as JSON, allowing to override/add to \
       autocue-relevant metadata stored in file tags. This can help to avoid \
       unnecessary re-analysis runs. \
       Not supported by gocue (it reads file tags directly); kept for \
       configuration compatibility.',
    false
  )

let settings.gocue.ignored_overrides =
  settings.make(
    description=
      'List of gocue results that cannot be overridden by existing \
       metadata or annotations. One such field is `duration`, as it is not \
       a tag, and determined otherwise.',
    ['duration']
  )

let settings.gocue.max_initial_duration_to_skip =
  settings.make(
    description=
      "Max possible duration (in seconds) that may be skipped in the begging of the playlist due to synchronization",
    600.0
  )


time_to_skip = ref(0.0)
stdlib_metadata = metadata

# metadata.json.stringify only exports a limited set, use our own
def meta_json_stringify(
  ~compact=false,
  ~json5=false,
  m
) =
  m = metadata.cover.remove(m)
  data = json()
  list.iter(fun (v) -> data.add(fst(v), snd(v)), m)
  json.stringify(json5=json5, compact=compact, data)
end

# Need to handle pre-version 3.0.0 `liq_blankskip`: was bool, is now float`
# @vitoyucepi, in: https://github.com/savonet/liquidsoap/discussions/3965#discussioncomment-9744430
def something_to_float(~true_value=1., value) =
  value_string = string.case(string(value))
  possible_float =
    try
      float_of_string(value_string)
    catch _ do
      null
    end
  possible_bool =
    try
      bool_of_string(value_string) ? true_value : 0.
    catch _ do
      null
    end
  (possible_float ?? possible_bool) ?? 0.
end

# Activate Autocue, print to terminal if desired.
# There is no version handshake with the external binary (gocue is versioned
# independently of this script), so activation cannot fail and this always
# returns true.
stdlib_print = print

def check_autocue_setup(~do_print=false) =
  # set this so annotations (priority 5) can still override autocue values
  settings.autocue.metadata.priority := 10
  settings.autocue.preferred := "gocue"
  # use our values in any case
  settings.autocue.amplify_behavior := "keep"
  # avoid dead air from reconcile, reset default 3.0s to our fade_out duration
  settings.fade.out.duration := settings.gocue.fade_out()
  # Let user know what version (s)he is running
  log(level=2, label="gocue",
    'You are using gocue version \
     #{settings.gocue.version()}.'
  )
  log(level=2, label="gocue",
    'Using the external "#{settings.gocue.path()}" binary.'
  )
  log(level=2, label="gocue",
    'Setting `settings.fade.out.duration` to \
     #{settings.gocue.fade_out()} s, from \
     `settings.gocue.fade_out`.'
  )
  if do_print then
    stdlib_print(
      'You are using gocue version \
       #{settings.gocue.version()}.'
    )
    stdlib_print(
      'Using the external "#{settings.gocue.path()}" binary.'
    )
    stdlib_print(
      'Setting `settings.fade.out.duration` to \
       #{settings.gocue.fade_out()} s, from \
       `settings.gocue.fade_out`.'
    )
  end
  true
end

# gocue adaptation: cue_file writes tags itself (`-w`/`-r`), gocue never mutates
# files. Copy the audio stream to a temp file with the requested tags applied,
# then replace the original.
def gocue_write_tags(filename, tags, ~write_replaygain) =
  label = "gocue"
  # file.temp(prefix, suffix): keep a real media extension so ffmpeg can mux.
  tmp = file.temp("gocue_tags", file.extension(filename, leading_dot=true))
  # Allow sandbox access to the source file dir and the temp file dir.
  src_dir = path.dirname(filename)
  tmp_dir = path.dirname(tmp)
  args = ref(["-hide_banner", "-y", "-i", filename, "-c", "copy", "-map_metadata", "-1"])
  list.iter(
    fun (kv) -> begin
      let (k, v) = kv
      keep =
        string.contains(prefix="liq_", k) or
        k == "duration" or
        (write_replaygain and string.contains(prefix="replaygain_", k))
      if keep then
        args := [...args(), "-metadata", "#{k}=#{v}"]
      end
    end,
    tags
  )
  args := [...args(), tmp]
  cmd = process.quote.command("ffmpeg", args=args())
  log(level=4, label=label, "ffmpeg tag cmd: #{cmd}")
  res =
    process.run(
      timeout=settings.gocue.timeout(),
      rwdirs=[src_dir, tmp_dir, "default"],
      rodirs=["default"],
      cmd
    )
  if res.status == "exit" and res.status.code == 0 then
    file.move(force=true, tmp, filename)
    log(level=3, label=label, 'Wrote tags to "#{filename}"')
    true
  else
    log(
      level=2,
      label=label,
      'ffmpeg tag write failed for "#{filename}": status=#{res.status} \
       stderr=#{res.stderr}'
    )
    ignore(try file.remove(tmp) catch _ do () end)
    false
  end
end


# Compute gocue data
# @flag extra
def gocue(~request_metadata, ~file_metadata, filename) =
  timeout = settings.gocue.timeout()
  target = settings.gocue.target()
  silence = settings.gocue.silence()
  overlay = settings.gocue.overlay()
  longtail = settings.gocue.longtail()
  overlay_longtail = settings.gocue.overlay_longtail()
  drop = settings.gocue.sustained_loudness_drop()
  blankskip = settings.gocue.blankskip()
  write_tags = settings.gocue.write_tags()
  write_replaygain = settings.gocue.write_replaygain()
  force_analysis = settings.gocue.force_analysis()
  nice = settings.gocue.nice()
  noclip = settings.gocue.noclip()
  use_json_metadata = settings.gocue.use_json_metadata()
  max_skipped_duration = settings.gocue.max_initial_duration_to_skip()

  label = "gocue"

  # combine request & file metadata into one list, where
  # request_metadata (annotations) takes precedence
  m = ref(list.fold(
    fun(res, entry) ->
      if list.assoc.mem(fst(entry), res) then
        res
      else
        [...res, entry]
      end,
      request_metadata,
      file_metadata
  ))

  # so we can use meta["something"]
  meta = m()

  if
    meta["liq_cue_file"] == "false"
  then
    log(
      level=2,
      label=label,
      'Skipping gocue for "#{filename}" because liq_cue_file=false \
       forbids it.'
    )
    null
  else
    log(
      level=3,
      label=label,
      'Now autocueing: "#{filename}"'
    )

    l = list.sort.natural(stdlib_metadata.cover.remove(meta))
    log(
      level=4,
      label=label,
      'Metadata seen for "#{filename}":'
    )
    list.iter(fun (v) -> log(level=4, label=label, "#{v}"), l)

    log(
      level=4,
      label=label,
      'liq_blankskip=#{meta["liq_blankskip"]}, songtype=#{meta["songtype"]}, \
       jingle_mode=#{meta["jingle_mode"]}'
    )

    # Blank skipping can be set globally using `settings.gocue.blankskip`.
    # For AzuraCast, we override that setting if we detect "jingle_mode",
    # i.e. a track from a playlist that has "Hide Metadata from Listeners" set.
    # For standalone Liquidsoap, the ultimate override is `liq_blankskip`.
    # This can even be used to switch blank skipping ON if is globally off.
    blankskip = ref(blankskip)
    blankskip := list.assoc.mem("jingle_mode", meta) ? 0.0 : blankskip()

    # SAM Broadcaster compat: Switch blankskip off for all songtypes != "S"
    if list.assoc.mem("songtype", meta) then
      if meta["songtype"] != "S" then
        blankskip := 0.0
      end
    end

    # Handle annotated `liq_blankskip`, the ultimate switch
    # Pre-v3.0.0 compatibility: Check for true/false (now float)
    if list.assoc.mem("liq_blankskip", meta) then
      blankskip := null.get(
        default=0.0,
        something_to_float(
          true_value=settings.gocue.blankskip(),
          meta["liq_blankskip"]
        )
      )
      m := list.assoc.remove("liq_blankskip", m())
      m := list.add(
        ("liq_blankskip", string.float(decimal_places=2, blankskip())),
        m()
      )
    end

    log(
      level=3,
      label=label,
      "Blank (silence) skipping active: #{blankskip() > 0.0}, set to #{blankskip()} s"
    )

    log(
      level=3,
      label=label,
      "Clipping prevention active: #{noclip}"
    )

    log(
      level=3,
      label=label,
      "Writing tags: #{write_tags}, including ReplayGain: #{write_replaygain}"
    )

    # set up CLI arguments
    args =
      ref(
        [
          '-t',
          string.float(target, decimal_places=2),
          '-s',
          string.float(silence, decimal_places=2),
          '-o',
          string.float(overlay, decimal_places=2),
          '-l',
          string.float(longtail, decimal_places=2),
          '-x',
          string.float(overlay_longtail, decimal_places=2),
          '-d',
          string.float(drop, decimal_places=2),
          filename
        ]
      )
    if noclip then args := list.add('-k', args()) end
    if blankskip() > 0.0 then
      args := ['-b', string.float(blankskip(), decimal_places=2), ...args()]
    end
    # gocue adaptation: its own execution timeout defaults to 20s, which is too
    # short for a full analysis of long files. Keep it in sync with `timeout`.
    args := ['-e', '#{int(timeout)}s', ...args()]
    # gocue adaptation: `-w`/`-r` do not exist; tags are written below via ffmpeg.
    if force_analysis then
      log(
        level=2,
        label=label,
        "settings.gocue.force_analysis is not supported by gocue; ignoring."
      )
    end

    if use_json_metadata then
      # gocue adaptation: `-j` does not exist. gocue reads autocue-relevant
      # tags from the file itself, so there is nothing to hand over.
      log(
        level=2,
        label=label,
        "settings.gocue.use_json_metadata is not supported by gocue; ignoring."
      )
      log(
        level=4,
        label=label,
        "Metadata that would have been sent: #{meta_json_stringify(compact=true, m())}"
      )
    end

    # gocue adaptation: `-n` means "pretty-print JSON" for gocue, so run the
    # binary through `nice` instead of passing a flag.
    cmd =
      if nice then
        process.quote.command(
          "nice",
          args=[settings.gocue.path(), ...args()]
        )
      else
        process.quote.command(settings.gocue.path(), args=args())
      end

    res =
      try
        list.hd(
          default="",
          process.read.lines(
            timeout=timeout,
            cmd
          )
        )
      catch err do
        log(
          level=2,
          label=label,
          'gocue error: #{err}'
        )
        ""
      end

    if list.assoc.mem("enforce_start_time", meta) then
      enforce_start_time = float_of_string(meta["enforce_start_time"], default=0.0)
      log(level=3, label=label, "Enforce start time found: #{enforce_start_time}")
      if enforce_start_time > 0.0 then
        time_to_skip := time() - enforce_start_time
        if time_to_skip() > max_skipped_duration then
          log(level=3, label=label, "Found enforced_start_time tag suggests to skip #{time_to_skip()} seconds \
                                     which is more than max_skipped_duration (#{max_skipped_duration}) \
                                     - setting time_to_skip back to 0")
          time_to_skip := 0.0
        end
        log(level=3, label=label, "Skipping #{time_to_skip} seconds of the playlist")
      end
    end

    if
      res != ""
    then
      log(
        level=3,
        label=label,
        'gocue result for "#{filename}": #{res}'
      )

      let json.parse (
        {
          duration,
          liq_cue_duration,
          liq_cue_in,
          liq_cue_out,
          liq_cross_start_next,
          liq_longtail,
          liq_sustained_ending,
          liq_loudness,
          liq_loudness_range,
          liq_amplify,
          liq_amplify_adjustment,
          liq_reference_loudness,
          liq_blankskip,
          liq_blank_skipped,
          liq_true_peak,
          liq_true_peak_db
        }
      :
        {
          duration: float,
          liq_cue_duration: float,
          liq_cue_in: float,
          liq_cue_out: float,
          liq_cross_start_next: float,
          liq_longtail: bool,
          liq_sustained_ending: bool,
          liq_loudness: string,
          liq_loudness_range: string,
          liq_amplify: string,
          liq_amplify_adjustment: string,
          liq_reference_loudness: string,
          liq_blankskip: float,
          liq_blank_skipped: bool,
          liq_true_peak: float,
          liq_true_peak_db: string
        }
      ) = res

      # must stringify, because metadata & annotations are strings
      result = ref(
        [
          ("duration", string(duration)),
          ("liq_cue_duration", string(liq_cue_duration)),
          ("liq_cue_in", string(liq_cue_in)),
          ("liq_cue_out", string(liq_cue_out)),
          ("liq_cross_start_next", string(liq_cross_start_next)),
          ("liq_longtail", string(liq_longtail)),
          ("liq_sustained_ending", string(liq_sustained_ending)),
          ("liq_loudness", liq_loudness),
          ("liq_loudness_range", liq_loudness_range),
          ("liq_amplify", liq_amplify),
          ("liq_amplify_adjustment", liq_amplify_adjustment),
          ("liq_reference_loudness", liq_reference_loudness),
          ("liq_blankskip", string(liq_blankskip)),
          ("liq_blank_skipped", string(liq_blank_skipped)),
          ("liq_true_peak", string(liq_true_peak)),
          ("liq_true_peak_db", liq_true_peak_db)
        ]
      )

      # Check if we need to enforce the start time of the track
      if time_to_skip() > 0.0 then
        result := list.assoc.remove("liq_cue_in", result())
        if time_to_skip() >= duration then
          log(level=3, label=label, "Time to skip is greater than duration, skipping...")
          time_to_skip := time_to_skip() - duration
          result := list.add(("liq_cue_in", string.float(decimal_places=2, duration)), result())
        else
          log(level=3, label=label, "Time to skip is less than duration, setting liq_cue_in to: #{time_to_skip()}")
          result := list.add(("liq_cue_in", string.float(decimal_places=2, time_to_skip())), result())
          time_to_skip := 0.0
        end
      end

      # `liq_cue_file` determines what happens now:
      #   tag absent - normal handling, existing metadata preferred
      #   false - we'll never arrive here (don’t process, use existing metadata)
      #   true - gocue metadata preferred (for news, time, etc.)

      if meta["liq_cue_file"] == "" then
        # no `liq_cue_file`, existing metadata preferred
        log(
          level=4,
          label=label,
          'Existing metadata can override gocue results \
           (default; no liq_cue_file seen).'
        )
        result := list.fold(
          fun(res, entry) ->
            if list.assoc.mem(fst(entry), res) then
              if list.mem(fst(entry), settings.gocue.ignored_overrides()) then
                [...list.assoc.remove(fst(entry), res), entry]  # take gocue result
              else
                res  # take existing metadata (meta)
              end
            else
              [...res, entry]  # append new metadata (gocue)
            end,
          m(),
          result()
        )
      elsif meta["liq_cue_file"] == "true" then
        # `liq_cue_file=true`, gocue metadata preferred
        log(
          level=3,
          label=label,
          'gocue results override existing metadata \
           because liq_cue_file=true tells us to.'
        )
        result := list.fold(
          fun(res, entry) ->
            if list.assoc.mem(fst(entry), res) then
              res  # take existing metadata (gocue)
            else
              [...res, entry]  # append new metadata (meta)
            end,
          result(),
          m()
        )
      end

      # make a suffixed string a float
      def make_float(s) =
        # find first number, make float & return
        r = r/[+-]?\d*\.?\d+/g.exec(s)
        float_of_string(default=0.0, r[0])
      end

      # Re-calculate amplify and amplify_correction, using true_peak
      def amplify_correct(target, loudness, true_peak_dB, noclip) =
        # check if we need to reduce the gain for true peaks
        loudness = make_float(loudness)
        true_peak_dB = make_float(true_peak_dB)
        amp = ref(target - loudness)
        amp_correction = ref(0.0)
        if noclip then
          max_amp = -1.0 - true_peak_dB  # difference to EBU recommended -1 dBFS
          if amp() > max_amp then
            amp_correction := max_amp - amp()
            amp := max_amp
          end
        end
        (amp(), amp_correction())
      end

      # Override liq_amplify, liq_amplify_adjustment & liq_reference_loudness,
      # using clipping prevention as requested
      # liq_loudness & liq_true_peak_db are always in the gocue result
      let (amp, amp_correction) =
        amplify_correct(
          target,
          list.assoc("liq_loudness", result()),
          list.assoc("liq_true_peak_db", result()),
          noclip
        )
      result := list.assoc.remove("liq_amplify", result())
      result := list.add(("liq_amplify", string.float(decimal_places=2, amp) ^ " dB"), result())
      result := list.assoc.remove("liq_amplify_adjustment", result())
      result := list.add(("liq_amplify_adjustment", string.float(decimal_places=2, amp_correction) ^ " dB"), result())
      result := list.assoc.remove("liq_reference_loudness", result())
      result := list.add(("liq_reference_loudness", string.float(decimal_places=2, target) ^ " LUFS"), result())

      if settings.gocue.unify_loudness_correction() then
        # We wish to avoid loudness jumps in all possible cases,
        # so bring `replaygain_track_gain` and `liq_amplify` in line.
        # NOTE: This also works for different loudness targets, if
        # files have been tagged with a valid replaygain_reference_loudness.
        if list.assoc.mem("replaygain_track_gain", result()) then
          if list.assoc.mem("replaygain_reference_loudness", result()) then
            la = list.assoc(default="0.00 dB", "liq_amplify", result())
            rg = list.assoc(default="0.00 dB", "replaygain_track_gain", result())
            rgf = make_float(rg)
            rgr = list.assoc(default=string.float(decimal_places=2, target)^" dB", "replaygain_reference_loudness", result())
            rgrf = ref(make_float(rgr))
            # Handle old RG1/mp3gain positive loudness reference
            # "89 dB" (SPL) should actually be -14 LUFS, but as a reference
            # it is usually set equal to the RG2 -18 LUFS reference point
            if rgrf() > 0. then rgrf := rgrf() - 107. end
            # adjust replaygain_track_gain by loudness target difference, set reference
            # we can safely do that since we NEVER write back replaygain_* tags
            # Clipping prevention wins over simple RG adjusting
            if noclip then
              # override replaygain_track_gain with already calculated liq_amplify
              result := list.assoc.remove("replaygain_track_gain", result())
              result := list.add(("replaygain_track_gain", la), result())
              rg = string.float(decimal_places=2, rgf + (target - rgrf())) ^ " dB"
              log(level=3, label=label, 'Clipping prevention: Adjusted calculated replaygain_track_gain from #{rg} to #{la}')
            else
              # simply calculate new RG
              rg = string.float(decimal_places=2, rgf + (target - rgrf())) ^ " dB"
              result := list.assoc.remove("replaygain_track_gain", result())
              result := list.add(("replaygain_track_gain", rg), result())
              # Set liq_amplify to the same value
              result := list.assoc.remove("liq_amplify", result())
              result := list.add(("liq_amplify", rg), result())
              # And reset liq_amplify_adjustment
              result := list.assoc.remove("liq_amplify_adjustment", result())
              result := list.add(("liq_amplify_adjustment", "0.00 dB"), result())
              log(level=3, label=label, 'Replaced liq_amplify=#{la} with #{rg} from adjusted replaygain_track_gain')
            end
            # set replaygain_reference_loudness to new target
            rgr = string.float(decimal_places=2, target) ^ " LUFS"
            result := list.assoc.remove("replaygain_reference_loudness", result())
            result := list.add(("replaygain_reference_loudness", rgr), result())
          else
            log(level=3, label=label, "Can't override liq_amplify from replaygain_track_gain, replaygain_reference_loudness missing.")
          end
        else
          # no `replaygain_track_gain` seen? insert one, using calculated `liq_amplify`
          rg = list.assoc(default="0.00 dB", "liq_amplify", result())
          result := list.add(("replaygain_track_gain", rg), result())
          # also insert a `replaygain_reference_loudness`
          rgr = string.float(decimal_places=2, target) ^ " LUFS"
          result := list.assoc.remove("replaygain_reference_loudness", result())
          result := list.add(("replaygain_reference_loudness", rgr), result())
          log(level=3, label=label, 'Inserted replaygain_track_gain #{rg} and replaygain_reference_loudness #{rgr}')
        end
      end

      # Show any clipping prevention adjustments
      amp_correction_dB = list.assoc(default="0.00 dB", "liq_amplify_adjustment", result())
      if noclip and amp_correction_dB != "0.00 dB" then
        log(
          level=3,
          label=label,
          'Clipping prevention: Adjusted liq_amplify by #{amp_correction_dB} \
           because track’s true peak is #{list.assoc("liq_true_peak_db", result())}.'
        )
      end

      # Adjust fades and cue-out, if necessary
      liq_cue_in = float_of_string(list.assoc("liq_cue_in", result()))
      liq_cue_out = float_of_string(list.assoc("liq_cue_out", result()))
      liq_cross_start_next = float_of_string(list.assoc("liq_cross_start_next", result()))
      liq_fade_in =
        try
          float_of_string(list.assoc("liq_fade_in", result()))
        catch _ do
          log(
            level=3,
            label=label,
            "No fade-in duration given, using default setting \
             (#{settings.gocue.fade_in()} s)."
          )
          settings.gocue.fade_in()
        end

      liq_fade_out =
        try
          float_of_string(list.assoc("liq_fade_out", result()))
        catch _ do
          log(
            level=3,
            label=label,
            "No fade-out duration given, using default setting \
             (#{settings.gocue.fade_out()} s)."
          )
          settings.gocue.fade_out()
        end

      # User might have set cue-out but not start_next, correct
      liq_cross_start_next =
        if liq_cross_start_next <= liq_cue_out
        then
          liq_cross_start_next
        else
          start_next = liq_cue_out - liq_fade_out
          if start_next > liq_cue_in
          then
            # we have enough room for the fade-out
            log(
              level=3,
              label=label,
              "Given liq_cross_start_next (#{liq_cross_start_next} s) > \
               cue-out point (#{liq_cue_out} s), set to #{start_next} s."
            )
            start_next
          else
            # not enough room for fade-out, set to cue-out
            log(
              level=3,
              label=label,
              "Given liq_cross_start_next (#{liq_cross_start_next} s) > \
               cue-out point (#{liq_cue_out} s), set to #{liq_cue_out} s."
            )
            liq_cue_out
          end
        end

      # Adjust cue_out according to user-supplied fade_out
      let (liq_fade_out, liq_cue_out) =
        if
          liq_cross_start_next + liq_fade_out < liq_cue_out
        then
          cue_out = liq_cross_start_next + liq_fade_out
          overlay_duration = liq_cue_out - liq_cross_start_next
          log(
            level=3,
            label=label,
            "Given fade-out (#{liq_fade_out} s) < \
             overlay duration (#{overlay_duration} s), moving cue-out point \
             from #{liq_cue_out} s to #{cue_out} s."
          )
          (liq_fade_out, cue_out)
        else
          fade_out = liq_cue_out - liq_cross_start_next
          log(
            level=2,
            label=label,
            "Given fade-out duration (#{liq_fade_out} s) exceeds \
             available time, using #{fade_out} s."
          )
          (fade_out, liq_cue_out)
        end

      # Check for invalid fade.in
      let liq_fade_in =
        if
          liq_fade_in < liq_cue_out - liq_cue_in
        then
          liq_fade_in
        else
          log(
            level=2,
            label=label,
            "Given fade-in duration (#{liq_fade_in} s) exceeds \
             available time, using 0.1 s."
          )
          0.1
        end

      # correct `liq_cue_duration`
      liq_cue_duration = liq_cue_out - liq_cue_in
      result := list.assoc.remove("liq_cue_duration", result())
      result := list.add(("liq_cue_duration",
        string.float(decimal_places=2, liq_cue_duration)), result())

      # Update result
      result := list.assoc.remove("liq_cue_out", result())
      result := list.add(("liq_cue_out", string(liq_cue_out)), result())
      result := list.assoc.remove("liq_cross_start_next", result())
      result := list.add(("liq_cross_start_next", string(liq_cross_start_next)), result())
      result := list.assoc.remove("liq_fade_in", result())
      result := list.add(("liq_fade_in", string(liq_fade_in)), result())
      result := list.assoc.remove("liq_fade_out", result())
      result := list.add(("liq_fade_out", string(liq_fade_out)), result())

      # now remove everything that’s not autocue-relevant
      # so we don’t blow up decoder and annotation metadata
      def fl(k, _) =
        tags = ["duration", "replaygain_track_gain", "replaygain_reference_loudness"]
        string.contains(prefix="liq_", k) or list.mem(k, tags)
      end
      result := list.assoc.filter((fl), result())

      l = list.sort.natural(stdlib_metadata.cover.remove(result()))
      log.important(label=label, 'Metadata added/corrected for "#{filename}":')
      list.iter(fun(v) -> log.important(label=label, "#{v}"), l)

      # gocue adaptation: cue_file would have written tags itself via `-w`/`-r`.
      if write_tags then
        ignore(gocue_write_tags(filename, result(), write_replaygain=write_replaygain))
      end

      # for optional meta elements that aren’t guaranteed to be in result,
      # like replaygain_track_gain, replaygain_reference_loudness
      def optional_meta(lbl, meta) =
        if list.assoc.mem(lbl, meta) then
          [(lbl, list.assoc(lbl, meta))]
        else
          []
        end
      end

      extra_metadata =
        [
          ("duration", list.assoc("duration", result())),
          ("liq_amplify", list.assoc("liq_amplify", result())),
          ("liq_amplify_adjustment", list.assoc("liq_amplify_adjustment", result())),
          ("liq_cue_duration", list.assoc("liq_cue_duration", result())),
          ("liq_longtail", list.assoc("liq_longtail", result())),
          ("liq_sustained_ending", list.assoc("liq_sustained_ending", result())),
          ("liq_loudness", list.assoc("liq_loudness", result())),
          ("liq_loudness_range", list.assoc("liq_loudness_range", result())),
          ("liq_reference_loudness", list.assoc("liq_reference_loudness", result())),
          ("liq_blankskip", list.assoc("liq_blankskip", result())),
          ("liq_blank_skipped", list.assoc("liq_blank_skipped", result())),
          ("liq_true_peak", list.assoc("liq_true_peak", result())),
          ("liq_true_peak_db", list.assoc("liq_true_peak_db", result())),
          ...optional_meta("replaygain_track_gain", result()),
          ...optional_meta("replaygain_track_peak", result()),
          ...optional_meta("replaygain_track_range", result()),
          ...optional_meta("replaygain_reference_loudness", result())
        ]

      {
        amplify = list.assoc("liq_amplify", result()),
        cue_in = float_of_string(list.assoc("liq_cue_in", result())),
        cue_out = float_of_string(list.assoc("liq_cue_out", result())),
        fade_in = float_of_string(list.assoc("liq_fade_in", result())),
        fade_out = float_of_string(list.assoc("liq_fade_out", result())),
        start_next = float_of_string(list.assoc("liq_cross_start_next", result())),
        extra_metadata=extra_metadata
      }
    else
      log(
        level=2,
        label=label,
        'No autocue data found for "#{filename}"'
      )
      null
    end
  end
end

# set this so annotations (priority 5) can still override autocue values
settings.autocue.metadata.priority := 10
settings.autocue.preferred := "gocue"
# use our values in any case
settings.autocue.amplify_behavior := "keep"
# avoid dead air from reconcile, reset default 3.0s to our fade_out duration
settings.fade.out.duration := settings.gocue.fade_out()
autocue.register(name="gocue", gocue)
