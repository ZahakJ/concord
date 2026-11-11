# Audio modem link (`audio`)

Concord's sealed frames as sound: one device's speaker to another's microphone
across a room. Code: `internal/transport/audio/`, registered by
`internal/app/offgrid_audio.go` (desktop Linux only).

## What crosses the air

Exactly what crosses gossipsub: transport frames, i.e. a 12-byte bundle header
plus MLS ciphertext (or a fragment of one). The modem never sees a key or
plaintext. Anyone recording the room gets ciphertext.

## Design

| | |
|---|---|
| Modulation | multi-tone FSK, ggwave-style: 4 sub-bands × 16 tones, one tone per sub-band per symbol = 2 bytes/symbol |
| Band | 1875–4828 Hz (FFT bins 40–103 of a 1024-sample window, 46.875 Hz spacing) |
| Symbol | 2048 samples (42.7 ms) at 48 kHz; the middle 1024 are analysed, the rest is guard for echo and clock drift |
| Oscillators | continuous phase per sub-band (no clicks, no splatter), 10 ms fades at frame edges |
| Sync | 8-symbol pseudo-random tone preamble; the receiver scores every 128-sample offset (share of each sub-band's energy in the expected tone) and locks to the middle of the high-score plateau |
| Header | 3 symbols: version, length + 4 Reed–Solomon parity bytes (fixes 2 bad bytes) |
| Body | data ‖ CRC-32 ‖ 24 Reed–Solomon parity bytes (fixes 12 bad bytes), GF(2⁸) |
| MTU | 128 data bytes per audio frame; the transport Manager fragments larger bundles |
| Receiver cost | one 1024-point FFT per 128 samples (375/s) |
| Memory | bounded: ≤ ~11 s of samples buffered, 512-entry spectrum memo |

A frame whose body fails RS correction or whose CRC does not match is dropped.
Nothing garbled is handed to the Manager.

Caps: `Mode: manual`, `Local: true`, `Encryption: true`, `MTU 128`,
`Bitrate 250`, range "same room (~1–5 m)". Manual means frames wait in the
outbox until the user presses **Send N queued** in Settings → Off-grid links,
and only then does the computer play them.

## Audio I/O and the microphone

No cgo. The link drives the system's own tools on a pipe, raw s16le mono
48 kHz, first one found:

- capture: `pw-record` → `parec` → `arecord`
- playback: `pw-play` → `pacat` → `aplay`

If a recorder exits before producing audio (an old PipeWire whose `pw-record`
rejects `--raw`/`--latency`), the next one is tried. Each played frame has a
deadline of its own length plus 5 s; a wedged player is killed so it cannot
hang Flush and the RPC behind it. Decoded frames are handed up through a
64-frame queue off the capture loop, so slow processing above never stalls
the microphone pipe.

`Probe()` only reads `/proc/asound/pcm` and looks the tools up on `PATH`; it
never opens a device. That misses devices the kernel's ALSA list does not show
— Bluetooth headsets (which exist only inside PipeWire/PulseAudio) and a
Flatpak sandbox without `/proc/asound` — which then read as "no hardware"
even though the sound server could reach them. The recorder is started by `Start()`, which the Manager
calls only after the user enables the link, and killed (and reaped) by `Stop()`.
`Stop()` also kills a frame that is mid-play, and `Send()` refuses while the
link is off, so switching it off silences a long flush at once.
While the link is playing, and for 300 ms after, the captured audio is replaced
by silence so a device does not decode itself. If the recorder exits on its
own (mic unplugged, sound server restarted), the settings row says
"microphone stopped: …" and the link refuses to play (half duplex: a link that
cannot hear should not talk). Switching it off clears that, and on again
starts a fresh recorder.

## Throughput (measured)

- On the air: a full 128-byte frame is 89 symbols = 3.80 s → **33.7 B/s
  (270 bit/s)** of payload before gaps.
- Wall clock through the real playback path (`pw-play` per frame, 100 ms
  silence lead-in and tail, player start-up): **28.6 B/s** (296 bytes in 3
  frames in 10.36 s).
- A short chat message (~200–400 bytes of MLS ciphertext + 12-byte header,
  fragmented into 121-byte pieces) is 2–4 audio frames: roughly 8–16 s of sound.

## Test results

Simulated channel (`go test ./internal/transport/audio`), every case decodes
all 5 frames (1, 17, 64, 127, 128 bytes) byte-for-byte:

- clean; attenuation to 1 % amplitude; 0 dB wideband SNR (white noise across
  the whole 24 kHz, i.e. about +9 dB inside the 3 kHz the modem uses)
- receiver clock ±1000 ppm (sweeps passed to ±3000 ppm)
- echo at 0.6 gain/7 ms; everything at once (5 % gain, +500 ppm, 0.5 echo
  at 11 ms, 3 dB SNR, odd start offset); arbitrary push chunk sizes
- margin sweep (10 × 128-byte frames, white noise): 10/10 down to −9 dB
  wideband SNR, 0/10 at −12 dB; echoes of 0.7 at 3–60 ms all 10/10
- 30 s of pure noise: zero frames; heavy noise: never a frame that was not sent
- a frame with 8 symbols (16 bytes) destroyed is rejected; with 4 destroyed
  (8 bytes) it is repaired
- Reed–Solomon: random 0–12 errors always corrected; 30 errors never returned
  as the original message
- `TestAudioLinkCarriesAMessageThroughTheManager`: two links joined by a file
  (`cat >>` / `tail -f` subprocesses), Manager outbox → Flush → modem →
  reassembly → delivery; asserts the recorder is not started before the link is
  enabled and is dead after Stop

### On real hardware (this dev box, 2026-10-26)

Hardware: Intel HDA ALC897 (outputs: S/PDIF only — headphone, line-out and
HDMI jacks all report unplugged), USB mic JOUNIVO JV603 (ALSA card 1, the
PipeWire default source).

- **Not tested: speaker → air → microphone.** There is no audible output on
  this machine (the only sink is `iec958-stereo`, S/PDIF), and the JV603
  returns exact digital silence — peak 0 over 8 s read straight from ALSA
  `hw:1,0`, bypassing PipeWire, with ALSA capture switch on and gain at
  2699/3996 — which looks like its hardware touch-mute is engaged. So no real
  room noise could be recorded either.
- **Tested: the real playback and capture subprocesses through PipeWire.**
  `TestHardwareLoopbackThroughSoundServer`: `pw-play` into the default sink,
  `parec` on that sink's monitor, decoded by a second link: 3/3 frames
  byte-for-byte, 28.6 B/s wall clock. This covers the PCM format, the
  sound server's mono→stereo/s16→s32 conversion and back, and player timing —
  everything except the air.
- **Tested: the real microphone path.** `TestHardwareMicrophoneOpensAndCloses`:
  `pw-record` on the JV603 delivers ~47 k samples/s, nothing is decoded out of
  it, and no recorder process survives `Stop()`.

```
CONCORD_AUDIO_HW=1 CONCORD_AUDIO_MONITOR=$(pactl get-default-sink).monitor \
  go test -run 'HardwareLoopback|HardwareMicrophone' -v ./internal/transport/audio
```

## Two-laptop / laptop-and-phone test

Acoustic test of the modem alone (no Concord accounts needed), two Linux
machines with this repo:

1. Put them 0.5–2 m apart, speakers up, room reasonably quiet. Set the sender's
   output volume to ~70 %; check the receiver's mic is not muted
   (`pactl get-source-mute @DEFAULT_SOURCE@`).
2. Receiver:
   `CONCORD_AUDIO_HW=1 CONCORD_AUDIO_ROLE=rx go test -run HardwareListen -v -timeout 5m ./internal/transport/audio`
3. Sender, within 3 minutes:
   `CONCORD_AUDIO_HW=1 CONCORD_AUDIO_ROLE=tx go test -run HardwareTransmit -v ./internal/transport/audio`
4. The receiver logs each of the 10 × 128-byte frames as it decodes and the
   first→last time. Pass = 10/10. Repeat at 1 m, 3 m, 5 m and with a voice or
   music in the room; note what fails.

Through the app (two desktops, same guild):

1. Settings → Off-grid links: turn **Offline mode** on and enable **Sound
   (audio modem)** on both. Disconnect both from the network.
2. Send a message on A. The Sound row shows **Send N queued**; press it — A
   plays N × ~4 s of chirps.
3. The message appears on B. Received count on B's row goes up.

Phone: the mobile app has no audio link yet (this one drives desktop sound
tools). A phone can still carry the sound: record the sender's chirps with the
phone's voice recorder, then play the recording next to the receiving laptop
with its link enabled. The demodulator does not care when the sound was made.
(Untested: a lossy voice-memo codec at a low bitrate may smear the tones.)

## Limits and next steps

- Half duplex and one-way per press; no acknowledgements. A missed frame is
  missing until the user sends again (the Manager's dedupe makes a resend
  harmless). The internet path still syncs everything later.
- Errors-only RS. Soft decisions (marking a sub-band whose two strongest tones
  are close as an erasure) would roughly double what RS can repair.
- No timing tracking within a frame; ±512 samples of guard covers ±2800 ppm
  over a 3.8 s frame, far more than sound cards drift.
- A native Android/iOS implementation (AudioRecord/AudioTrack) can reuse
  `Modulate` and `Demodulator` unchanged through the Go core.
