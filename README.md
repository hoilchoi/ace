# ace

<p align="center">
  <img src="static/logo.png" alt="ace logo" width="200">
</p>

Web admin for driving `voip_patrol` test scenarios:
list scenarios, run them, capture per-call pass/fail + RTP/SIP stats,
play back recorded WAVs.

See [SPEC.md](SPEC.md).

## Quick start

```
go build -o ace .

./ace -addr 0.0.0.0:8086 \
      -voip-patrol-bin /git/voip_patrol/voip_patrol \
      -public-address 24.122.254.8 \
      -scenarios-dir ./scenarios \
      -runs-dir ./runs
```

Drop XML scenarios under `./scenarios/` (any `.xml` file is picked up).

Open `http://localhost:8086/`.

## Flags

```
-addr                  HTTP bind (default 0.0.0.0:8086)
-voip-patrol-bin       path to voip_patrol (default /usr/local/bin/voip_patrol)
-voip-patrol-port      local SIP port (default 5093)
-public-address        public IP for Contact/Via (NAT)
-scenarios-dir         XML scenarios dir (default ./scenarios)
-runs-dir              per-run output dir (default ./runs)
```

## Notes

- One run at a time. The nav shows a "run in progress" badge.
- voip_patrol's `record="true"` attribute on the call action produces
  `record_*.wav` files in each run's directory; the UI lists them and
  serves them via `<audio>` playback.
- `results.json` is append-only inside voip_patrol; we set cwd per-run
  so each run gets its own clean file under `runs/<id>/`.

## Audio checks

A call can pass SIP and still carry no usable audio. Put a `<scenario>.checks.json`
next to the scenario XML (or edit it under **Audio checks** on the scenario page) and
ace analyzes each run's recording; a failed check fails the run and alerts like any
other bot failure. The call action needs `record="true"`.

```json
{
  "thresholds": {
    "rx_rtp_packets":     {"min": 1000},
    "rx_first_speech_ms": {"max": 8000}
  },
  "expect": [
    {"name": "greeting", "wav": "prompts/greeting.wav", "heard_at_ms": {"max": 10000}}
  ]
}
```

- `thresholds` bound call-wide metrics: `rx_rtp_packets` (inbound RTP packets),
  `rx_speech_ms` (total inbound audio) and `rx_first_speech_ms` (first inbound
  audio, ms after answer).
- `expect` lists audio the far end should play: an IVR prompt, an announcement or,
  for an echo line, the scenario's own `play=` file. ace looks for it anywhere in the
  recording and reports `expect.<name>.heard_ms` (how much was recognised),
  `.heard_at_ms` (first heard, ms after answer) and `.offset_ms` (recording position
  minus WAV position; the round trip, for an echo). `heard_ms` / `heard_at_ms` take
  `{"min": …, "max": …}`; with neither, the audio must simply be heard. A WAV played
  on a loop counts its first pass. Onsets are accurate to about a second.
- `call_label` picks the call action when a scenario has several; `params` tunes the
  analysis (see `audio.Params`).

Upload WAVs (16-bit PCM; mono 8 kHz is safest) on the **Prompts** page; it shows the
path to use in `play="…"`. Results appear on the run page and runs list, and as
`ace_run_audio_pass_last` / `ace_run_audio_metric_last{metric=…}`.

Golden tests on real recordings run with `ACE_AUDIO_TESTDATA=<dir>` pointing at
`<name>.wav`, `<name>.expect.wav` and `<name>.want.json` files kept outside the repo;
`go test ./audio -run Golden -update` fills in a new case.
