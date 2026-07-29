// voice.js: a browser-side WebRTC audio mesh for one voice channel.
//
// Media never touches the Go backend. Each participant opens a direct
// RTCPeerConnection to every other participant; audio flows P2P over
// DTLS-SRTP. Go only relays the SDP/ICE signaling (api.relaySignal) and tells
// us who is present (voice-presence events, fed in via handlePresence).
//
// Glare (both sides offering at once) is resolved with the standard "perfect
// negotiation" pattern, with the politeness role decided deterministically by
// comparing peer IDs so the two sides always disagree.

import { micStream, cameraStream, applySink } from "./devices.js";
import { tuneOpus } from "./sdp.js";
import { loadDenoiser, makeDenoiseNode, nrValue } from "./denoise.js";

const DEFAULT_ICE = [{ urls: "stun:stun.l.google.com:19302" }];

// How long the mic stays open after a push-to-talk key comes up.
const TALK_TAIL_MS = 150;

export class VoiceMesh {
  // selfPeerId: our libp2p peer id; channelId: the voice room; relay(to, json)
  // sends a signaling blob; onRoster(peerIds[]) reports participant changes.
  constructor({
    selfPeerId,
    channelId,
    relay,
    onRoster,
    onSpeaking,
    onVideo,
    onVideoState,
    onWatcher,
    iceServers,
    forceRelay = false,
    devices = {},
    audio = {},
  }) {
    this.selfPeerId = selfPeerId;
    this.channelId = channelId;
    this.relay = relay;
    this.iceServers = iceServers?.length ? iceServers : DEFAULT_ICE;
    // forceRelay = never emit our real address: only relayed candidates, so a
    // call peer (e.g. a stranger on a meeting link) can't learn our IP. Requires
    // a TURN relay to be reachable, or the call can't connect.
    this.iceTransportPolicy = forceRelay ? "relay" : "all";
    this.onRoster = onRoster || (() => {});
    this.onSpeaking = onSpeaking || (() => {});
    // onVideo(key, stream|null, meta): a remote video source appeared/vanished.
    //   key = `${peerId}:${streamId}`; meta = { peerId, kind }.
    // onVideoState(kind, on): our own camera/screen toggled.
    this.onVideo = onVideo || (() => {});
    this.onVideoState = onVideoState || (() => {});
    // onWatcher(peerId): someone began watching the screen we're sharing.
    this.onWatcher = onWatcher || (() => {});
    this.peers = new Map(); // peerId -> { pc, makingOffer, ignoreOffer, audioEl, videoKeys }
    this.localStream = null;
    this.muted = false;
    // Deafen: silence ALL incoming audio (and imply self-mute). volumes holds a
    // per-peer 0..1 gain so you can turn any one participant down/off locally,
    // independent of deafen. Both are client-side only — nothing leaves this node.
    this.deafened = false;
    this.volumes = new Map(); // peerId -> 0..1 (absent = 1)
    this.audioCtx = null;
    this.analysers = new Map(); // key ("self"|peerId) -> { analyser, data }
    this._monitor = null;
    this._lastSpeaking = "";
    // Local video sources: independent "screen" and "camera", each a stream +
    // per-peer senders so either can be added/removed without touching the
    // other. A screen share can carry system audio, so it's senders per peer
    // (plural): the video track and, when the OS/browser allows it, its sound.
    this.videoSources = { screen: null, camera: null }; // kind -> { stream, senders: Map<peerId, sender[]> }
    // Remote stream-id -> kind ("screen"|"camera"), learned from a signaling note.
    this.remoteKinds = new Map();
    this._pendingVideo = new Map(); // streamId -> re-emit fn (kind arrived late)
    // Chosen hardware ({ mic, speaker, camera } deviceIds; "" = OS default).
    // Held here rather than read from app state so this file stays a plain
    // WebRTC module; the app hands them in and calls the setters below when
    // the user picks something else mid-call.
    this.devices = {
      mic: devices.mic || "",
      speaker: devices.speaker || "",
      camera: devices.camera || "",
      // Optional audio input used as a screen share's sound when the platform
      // won't give us one (see startVideo).
      shareAudio: devices.shareAudio || "",
    };
    // Audio knobs. The three processing flags are capture-time getUserMedia
    // constraints (changing one reopens the mic); gain/gate are ours, done in a
    // small WebAudio chain; output is master playback level.
    this.audio = {
      echoCancel: audio.echoCancel !== false,
      noiseSuppress: audio.noiseSuppress !== false,
      autoGain: audio.autoGain !== false,
      gain: audio.gain ?? 1, // mic boost/trim; 1 = the capture as it comes
      gate: audio.gate ?? 0, // 0 = off, else the RMS level that opens the mic
      nr: audio.nr || "", // spectral noise reduction level id ("" = off)
      output: audio.output ?? 1,
      bitrate: audio.bitrate ?? 64000, // what we ask peers for AND send, bits/s
    };
    // A nonce for THIS mesh instance. It rides every signaling message so the
    // far side can tell "the same peer, still talking" from "the same peer,
    // restarted" — a page refresh or relaunch keeps the libp2p peer id, so
    // without it a reconnection is indistinguishable from a heartbeat.
    this.sessionId = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
    this.sendStream = null; // processed mic, when a chain is in play
    this._chain = null; // { src, gain, gate }
    this._gateOpenUntil = 0;
    // Push-to-talk: the mic is shut unless `talking` is held. It supersedes the
    // noise gate rather than stacking with it — see setPushToTalk.
    this.pushToTalk = !!audio.pushToTalk;
    this.talking = false;
    this._talkTail = null;
  }

  async start() {
    this.localStream = await micStream(this.devices.mic, this.audio);
    if (this.audio.nr) await loadDenoiser(this._ac());
    this._buildChain();
    // Join shut, not open, when the mic is on a key — and let the same call
    // park the gate, so there's one place that knows how those two interact.
    if (this.pushToTalk) this.setPushToTalk(true);
    this._hintTracks();
    // Metered on the RAW capture, not the processed send: the gate below reads
    // this level to decide when to open, and a meter behind a closed gate could
    // never reopen it.
    this._addAnalyser("self", this.localStream);
    this._startMonitor();
  }

  // ---- microphone: device, processing, boost, gate ----

  // _buildChain routes the raw capture through boost + gate before it goes out.
  // It's only built when a knob is off-default: at 100% with the gate off,
  // peers get the capture track itself, exactly as they did before any of this
  // existed. Best effort — a webview without MediaStreamAudioDestinationNode
  // simply keeps sending the raw track.
  _needsChain() {
    return this.audio.gain !== 1 || !!this.audio.gate || !!this.audio.nr;
  }

  _buildChain() {
    this._teardownChain();
    if (!this.localStream || !this._needsChain()) return;
    try {
      const ctx = this._ac();
      const src = ctx.createMediaStreamSource(this.localStream);
      // Order matters. The high-pass goes first so rumble never reaches the
      // noise estimator (it would otherwise dominate the low bins), denoise
      // next so the gate is deciding about a signal that's already clean, and
      // boost last so it lifts your voice rather than the noise under it.
      const hp = ctx.createBiquadFilter();
      hp.type = "highpass";
      hp.frequency.value = 85; // below the lowest speech fundamental
      hp.Q.value = 0.7;
      const nr = this.audio.nr ? makeDenoiseNode(ctx, nrValue(this.audio.nr)) : null;
      const gain = ctx.createGain();
      gain.gain.value = this.audio.gain;
      const gate = ctx.createGain();
      gate.gain.value = this.audio.gate ? 0 : 1; // a gate starts closed
      const dest = ctx.createMediaStreamDestination();
      let node = src.connect(hp);
      if (nr) node = node.connect(nr);
      node.connect(gain).connect(gate).connect(dest);
      this._chain = { src, hp, nr, gain, gate };
      this.sendStream = dest.stream;
    } catch (err) {
      console.warn("audio chain unavailable", err);
      this._chain = null;
      this.sendStream = null;
    }
  }

  // setNoiseReduction changes the spectral denoiser's strength. Going from off
  // to on (or back) changes the graph, so it rebuilds; anything else is a live
  // parameter tweak with no track churn.
  async setNoiseReduction(id) {
    const had = !!this.audio.nr;
    this.audio.nr = id || "";
    if (had && this.audio.nr && this._chain?.nr) {
      this._chain.nr.parameters.get("strength").value = nrValue(this.audio.nr);
      return;
    }
    if (this.audio.nr) await loadDenoiser(this._ac());
    await this._rechain();
  }

  _teardownChain() {
    if (this._chain) {
      try {
        this._chain.src.disconnect();
        this._chain.hp?.disconnect();
        this._chain.nr?.disconnect();
      } catch {}
      this._chain = null;
    }
    this.sendStream?.getTracks().forEach((t) => t.stop());
    this.sendStream = null;
  }

  // _micTrack: what peers actually receive — the processed track when a chain
  // is up, otherwise the raw capture.
  _micTrack() {
    return this.sendStream?.getAudioTracks()[0] || this.localStream?.getAudioTracks()[0] || null;
  }

  // _sendMicTrack hands the current outgoing track to every peer. replaceTrack
  // keeps the existing m-line, so nothing renegotiates and nobody hears a gap.
  async _sendMicTrack() {
    const track = this._micTrack();
    if (!track) return;
    track.enabled = this._micLive(); // a fresh track is live; honor mute / push-to-talk
    this._hintTracks();
    for (const [, peer] of this.peers) {
      const sender = peer.pc.getSenders().find((s) => s.track?.kind === "audio");
      try {
        await sender?.replaceTrack(track);
      } catch (err) {
        console.warn("mic swap", err);
      }
      // replaceTrack keeps the m-line but resets encoder parameters.
      await this._applySenderParams(peer.pc);
    }
  }

  // setInputDevice swaps the microphone without dropping the call.
  async setInputDevice(deviceId) {
    this.devices.mic = deviceId || "";
    await this._reopenMic();
  }

  // setProcessing toggles one of the browser's capture-time filters (echo
  // cancellation, noise suppression, automatic gain). They can only be chosen
  // when the mic is opened, so this recaptures.
  async setProcessing(name, on) {
    this.audio[name] = !!on;
    await this._reopenMic();
  }

  // _reopenMic recaptures with the current device + processing. The new stream
  // is opened FIRST so a failure leaves the current mic running rather than
  // knocking us silent mid-sentence.
  async _reopenMic() {
    if (!this.localStream) return; // not in a call: applies at the next join
    const next = await micStream(this.devices.mic, this.audio);
    if (!next.getAudioTracks().length) {
      next.getTracks().forEach((t) => t.stop());
      return;
    }
    const old = this.localStream;
    this._teardownChain();
    this.localStream = next;
    this._buildChain();
    await this._sendMicTrack();
    old.getTracks().forEach((t) => t.stop());
    this.analysers.delete("self"); // re-meter from the new source
    this._addAnalyser("self", next);
  }

  // setMicGain boosts (or trims) the mic before it's sent: 1 = untouched. When
  // this is the knob that creates or retires the chain, the outgoing track
  // changes with it, so peers get the new one.
  async setMicGain(gain) {
    this.audio.gain = Math.max(0.25, Math.min(4, gain || 1));
    if (this._chain && this._needsChain()) {
      // Ramp rather than jump — a step in gain is an audible click.
      this._chain.gain.gain.setTargetAtTime(this.audio.gain, this._ac().currentTime, 0.02);
      return;
    }
    await this._rechain();
  }

  // setGate sets the level your voice has to clear for the mic to open, so a
  // noisy room isn't in everyone's ears between sentences. 0 turns it off.
  async setGate(threshold) {
    this.audio.gate = Math.max(0, Math.min(0.25, threshold || 0));
    if (this._chain && this._needsChain()) {
      // The chain stays up and the monitor picks up the new threshold on its
      // next tick — no track churn while a slider is being dragged. Turning the
      // gate off, though, has to leave it open for good.
      if (!this.audio.gate) this._chain.gate.gain.setTargetAtTime(1, this._ac().currentTime, 0.02);
      return;
    }
    await this._rechain();
  }

  async _rechain() {
    const had = !!this._chain;
    this._buildChain();
    if (had || this._chain) await this._sendMicTrack();
  }

  // _applyGate opens the outgoing gate while you're actually talking and closes
  // it in between. It reads the PRE-gate level (scaled by the boost, so the
  // threshold means what peers would hear) — fast to open, slow to close, with
  // a hold so word tails and short pauses don't get chopped.
  _applyGate(level) {
    if (!this._chain || !this.audio.gate || this.pushToTalk) return;
    if (level * this.audio.gain >= this.audio.gate) this._gateOpenUntil = Date.now() + 500;
    const open = Date.now() < this._gateOpenUntil;
    this._chain.gate.gain.setTargetAtTime(open ? 1 : 0, this._ac().currentTime, open ? 0.01 : 0.15);
  }

  // ---- push-to-talk ----
  //
  // The alternative to voice activity, not a layer on top of it: while it's on,
  // the noise gate is parked open and stops deciding anything. It has to be —
  // the gate opens on measured level, but the level it measures comes from a
  // mic we've just switched off, so it would read silence and clip the first
  // syllable of every push.

  setPushToTalk(on) {
    this.pushToTalk = !!on;
    this.talking = false;
    clearTimeout(this._talkTail);
    if (this.pushToTalk && this._chain) {
      this._chain.gate.gain.setTargetAtTime(1, this._ac().currentTime, 0.02);
    }
    this._applyMicLive();
  }

  // setTalking is the key going down and coming back up. Down opens instantly;
  // up waits out a short tail, because people release on the last word and
  // cutting at the keystroke eats its final consonant.
  setTalking(on) {
    clearTimeout(this._talkTail);
    if (on) {
      this.talking = true;
      this._applyMicLive();
      return;
    }
    this._talkTail = setTimeout(() => {
      this.talking = false;
      this._applyMicLive();
    }, TALK_TAIL_MS);
  }

  // _micLive is the single answer to "should peers be hearing us right now".
  // Mute outranks push-to-talk: a muted mic stays muted however hard you hold
  // the key, and deafen reaches this through mute.
  _micLive() {
    return !this.muted && (!this.pushToTalk || this.talking);
  }

  // Both ends of the chain: killing the capture silences what's downstream, and
  // disabling the sent track is what peers actually see stop.
  _applyMicLive() {
    const live = this._micLive();
    this.localStream?.getAudioTracks().forEach((t) => (t.enabled = live));
    this.sendStream?.getAudioTracks().forEach((t) => (t.enabled = live));
  }

  // ---- audio quality ----
  //
  // Two halves that have to agree: the SDP says what we'll ACCEPT from a peer,
  // and the sender parameters say what our own encoder will SPEND. Setting only
  // one leaves the call at the browser's timid default in that direction.

  // _shareAudio: the audio track riding our screen share, if the platform gave
  // us one. It's music/game audio, not a voice, and is treated accordingly.
  _shareAudio() {
    return this.videoSources.screen?.stream.getAudioTracks()[0] || null;
  }

  // _tune rewrites an offer/answer's Opus settings. Shared audio gets marked in
  // BOTH directions, and that matters: an fmtp line says what the sender of the
  // SDP is willing to receive, so the side listening to a screen share is the
  // one that has to ask for stereo.
  _tune(peer, sdp) {
    try {
      const share = this._shareAudio();
      const hifiIndexes = [];
      peer.pc.getTransceivers().forEach((t, i) => {
        const sending = share && t.sender?.track === share;
        const receiving = t.receiver?.track && peer.hifiTracks.has(t.receiver.track);
        if (sending || receiving) hifiIndexes.push(i);
      });
      return tuneOpus(sdp, { bitrate: this.audio.bitrate, hifiIndexes });
    } catch (err) {
      console.warn("sdp tune skipped", err);
      return sdp; // a call at default quality beats a call that won't connect
    }
  }

  // _applySenderParams raises our own encoder ceiling and marks voice as
  // high-priority traffic, so it's the last thing starved when a screen share
  // and a call compete for the same uplink.
  async _applySenderParams(pc) {
    const share = this._shareAudio();
    for (const s of pc.getSenders()) {
      if (s.track?.kind !== "audio") continue;
      try {
        const p = s.getParameters();
        if (!p.encodings?.length) p.encodings = [{}];
        p.encodings[0].maxBitrate = s.track === share ? Math.max(this.audio.bitrate, 160000) : this.audio.bitrate;
        p.encodings[0].networkPriority = "high";
        p.encodings[0].priority = "high";
        await s.setParameters(p);
      } catch {
        /* older webviews reject encodings edits; the SDP half still applies */
      }
    }
  }

  // setBitrate changes the quality target for a call in progress. The encoder
  // side takes effect immediately; the "send us this much" half needs a fresh
  // offer, so nudge one.
  async setBitrate(bps) {
    this.audio.bitrate = Math.max(8000, Math.min(320000, bps || 64000));
    for (const [peerId, peer] of this.peers) {
      await this._applySenderParams(peer.pc);
      try {
        const offer = await peer.pc.createOffer();
        offer.sdp = this._tune(peer, offer.sdp);
        await peer.pc.setLocalDescription(offer);
        this.send(peerId, { description: peer.pc.localDescription });
      } catch (err) {
        console.warn("bitrate renegotiation", err);
      }
    }
  }

  // _hintTracks tells the encoder what it's carrying. "speech" lets it lean on
  // speech coding; "music" keeps it faithful — which is what you want when the
  // filters are off, or when the audio is a screen share's soundtrack.
  _hintTracks() {
    const voice = this.audio.noiseSuppress || this.audio.autoGain ? "speech" : "music";
    for (const t of this.localStream?.getAudioTracks() || []) t.contentHint = voice;
    for (const t of this.sendStream?.getAudioTracks() || []) t.contentHint = voice;
    const share = this._shareAudio();
    if (share) share.contentHint = "music";
  }

  // setOutputVolume is the master playback level for the whole call; the
  // per-peer volumes multiply into it.
  setOutputVolume(v) {
    this.audio.output = Math.max(0, Math.min(1, v ?? 1));
    for (const [peerId, peer] of this.peers) this._applyAudio(peerId, peer);
  }

  // setOutputDevice re-points every participant's playback at another speaker.
  // No-op on webviews without setSinkId (see devices.canPickOutput).
  async setOutputDevice(deviceId) {
    this.devices.speaker = deviceId || "";
    for (const [, peer] of this.peers) {
      for (const el of [peer.audioEl, ...peer.auxEls.values()]) {
        await applySink(el, this.devices.speaker);
      }
    }
  }

  // setCameraDevice switches which camera is sent. Unlike the mic this restarts
  // the source (the local preview tile is keyed on the stream, so the app needs
  // the new one back) — returns the new preview stream, or null if the camera
  // is currently off, in which case the choice just applies next time.
  // setShareAudioDevice picks where a screen share's sound comes from when the
  // platform won't supply it. Applies to the next share.
  setShareAudioDevice(deviceId) {
    this.devices.shareAudio = deviceId || "";
  }

  async setCameraDevice(deviceId) {
    this.devices.camera = deviceId || "";
    if (!this.videoSources.camera) return null;
    this.stopVideo("camera");
    return this.startVideo("camera");
  }

  // _ac: the one AudioContext this call uses, for both metering and the input
  // chain. Created on first need; closed in stop().
  _ac() {
    if (!this.audioCtx) {
      this.audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    }
    // A suspended context doesn't just stop metering — when the input chain is
    // in play it IS the outgoing audio, so leaving it suspended would send
    // silence. Autoplay policy suspends contexts created without a gesture.
    if (this.audioCtx.state === "suspended") this.audioCtx.resume().catch(() => {});
    return this.audioCtx;
  }

  stop() {
    this.stopVideo("screen");
    this.stopVideo("camera");
    this._teardownChain();
    for (const id of [...this.peers.keys()]) this.removePeer(id);
    if (this._monitor) clearInterval(this._monitor);
    this._monitor = null;
    clearTimeout(this._talkTail);
    this.analysers.clear();
    if (this.audioCtx) {
      this.audioCtx.close().catch(() => {});
      this.audioCtx = null;
    }
    if (this.localStream) {
      this.localStream.getTracks().forEach((t) => t.stop());
      this.localStream = null;
    }
  }

  // _addAnalyser wires an audio-level meter for a stream, keyed by "self" or a
  // peer ID, so we can detect who is currently speaking.
  _addAnalyser(key, stream) {
    try {
      const src = this._ac().createMediaStreamSource(stream);
      const analyser = this.audioCtx.createAnalyser();
      analyser.fftSize = 512;
      src.connect(analyser);
      this.analysers.set(key, { analyser, data: new Uint8Array(analyser.frequencyBinCount) });
    } catch {
      /* audio metering is best-effort */
    }
  }

  _startMonitor() {
    if (this._monitor) return;
    this._monitor = setInterval(() => {
      const speaking = new Set();
      let selfLevel = 0;
      for (const [key, { analyser, data }] of this.analysers) {
        analyser.getByteTimeDomainData(data);
        let sum = 0;
        for (let i = 0; i < data.length; i++) {
          const v = (data[i] - 128) / 128;
          sum += v * v;
        }
        const rms = Math.sqrt(sum / data.length);
        if (key === "self") selfLevel = rms;
        if (rms > 0.04) speaking.add(key);
      }
      this._applyGate(selfLevel);
      // With a gate on, "speaking" should mean what peers can actually hear —
      // otherwise the ring lights up on room noise the gate is holding back.
      if (this.audio.gate && selfLevel * this.audio.gain < this.audio.gate) speaking.delete("self");
      const sig = [...speaking].sort().join(",");
      if (sig !== this._lastSpeaking) {
        this._lastSpeaking = sig;
        this.onSpeaking([...speaking]);
      }
    }, 150);
  }

  setMuted(muted) {
    this.muted = muted;
    this._applyMicLive();
  }

  // setDeafened silences every remote participant and, Discord-style, also mutes
  // your own mic (you can't sensibly talk to a room you can't hear). Undeafening
  // does NOT auto-unmute — that stays the user's explicit choice.
  setDeafened(deafened) {
    this.deafened = deafened;
    if (deafened) this.setMuted(true);
    for (const [peerId, peer] of this.peers) this._applyAudio(peerId, peer);
  }

  // setPeerVolume sets a single participant's local playback gain (0 = silence
  // just them, 1 = full). Persisted on the peer and re-applied if they reconnect.
  setPeerVolume(peerId, vol) {
    const v = Math.max(0, Math.min(1, vol));
    this.volumes.set(peerId, v);
    const peer = this.peers.get(peerId);
    if (peer) this._applyAudio(peerId, peer);
  }

  // _applyAudio reconciles everything we play from a peer — their voice and any
  // audio riding along with their screen share — against the deafen flag and
  // their per-peer volume. Deafen wins; otherwise volume 0 mutes just them.
  _applyAudio(peerId, peer) {
    if (!peer) return;
    const own = this.volumes.has(peerId) ? this.volumes.get(peerId) : 1;
    const vol = own * this.audio.output; // per-peer trim under the master level
    for (const el of [peer.audioEl, ...peer.auxEls.values()]) {
      if (!el) continue;
      el.muted = this.deafened || vol === 0;
      el.volume = vol;
    }
  }

  // toggleVideo(kind) starts/stops a local video source ("screen" or "camera").
  // Returns the local preview stream when starting (null on stop/cancel).
  async toggleVideo(kind) {
    if (this.videoSources[kind]) {
      this.stopVideo(kind);
      return null;
    }
    return this.startVideo(kind);
  }

  async startVideo(kind) {
    if (this.videoSources[kind]) return this.videoSources[kind].stream;
    let stream;
    try {
      stream =
        kind === "screen"
          ? // audio:true asks to share the sound of what's on screen too. Whether
            // it's actually offered is the platform's call — Chromium hands over
            // tab/window (and on Windows, system) audio if the user ticks the box
            // in the picker; WebKit gives video only. Either way we just send
            // whatever tracks come back.
            await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: 30 }, audio: true })
          : await cameraStream(this.devices.camera);
    } catch {
      return null; // user dismissed the picker / denied the camera
    }
    // Did the platform actually hand over the sound? Chromium gives tab/window
    // (and on Windows, system) audio when the user ticks the box in its picker;
    // WebKit and most Linux setups give video only, silently. When it didn't,
    // fall back to capturing an audio INPUT the user nominated — on Linux the
    // "Monitor of <your output>" device is exactly "what my speakers are
    // playing", which is the thing people mean by sharing sound.
    if (kind === "screen" && !stream.getAudioTracks().length && this.devices.shareAudio) {
      try {
        const extra = await micStream(this.devices.shareAudio, {
          // It's a program's output, not a voice in a room: every one of these
          // would fight it.
          echoCancel: false,
          noiseSuppress: false,
          autoGain: false,
        });
        const t = extra.getAudioTracks()[0];
        if (t) stream.addTrack(t);
      } catch (err) {
        console.warn("share audio source", err);
      }
    }
    const senders = new Map();
    this.videoSources[kind] = { stream, senders };
    this._hintTracks(); // a share's sound is music, not speech
    const track = stream.getVideoTracks()[0];
    // Fires on the browser's "Stop sharing" chrome or a camera unplug.
    track.addEventListener("ended", () => this.stopVideo(kind));
    for (const [peerId, peer] of this.peers) {
      senders.set(peerId, this._addSourceTracks(peer.pc, stream));
      // Tell the peer which kind this new stream is, so it labels the tile —
      // and, for audio, so it knows this isn't the mic.
      this.send(peerId, { videoMeta: { streamId: stream.id, kind } });
    }
    this.onVideoState(kind, true, { audio: stream.getAudioTracks().length > 0 });
    return stream;
  }

  // _addSourceTracks sends every track of a local video source (the picture,
  // plus its sound when the platform shared any) and returns the senders.
  _addSourceTracks(pc, stream) {
    const senders = [];
    for (const t of stream.getTracks()) {
      try {
        senders.push(pc.addTrack(t, stream));
      } catch (err) {
        console.warn("video addTrack", err);
      }
    }
    return senders;
  }

  stopVideo(kind) {
    const src = this.videoSources[kind];
    if (!src) return;
    for (const [peerId, peer] of this.peers) {
      for (const sender of src.senders.get(peerId) || []) {
        try {
          peer.pc.removeTrack(sender);
        } catch {}
      }
    }
    src.stream.getTracks().forEach((t) => t.stop());
    this.videoSources[kind] = null;
    this.onVideoState(kind, false);
  }

  // handlePresence reacts to a peer joining/leaving the room.
  handlePresence(from, action) {
    if (from === this.selfPeerId) return;
    if (action === "leave") {
      this.removePeer(from);
    } else if (action === "join" && !this.peers.has(from)) {
      // A newly-seen peer: create the connection. onnegotiationneeded drives
      // the offer, so both sides converge regardless of who saw whom first.
      this.addPeer(from);
    }
  }

  // handleSignal applies an inbound description or ICE candidate.
  async handleSignal(from, raw) {
    let msg;
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    if (msg.channelId !== this.channelId) return;

    // Somebody started watching a screen we're sharing.
    if (msg.watching) {
      if (this.videoSources.screen?.stream.id === msg.watching) this.onWatcher(from);
      return;
    }
    // A note about which kind a remote video stream is (camera vs screen).
    if (msg.videoMeta) {
      this.remoteKinds.set(msg.videoMeta.streamId, msg.videoMeta.kind);
      this._pendingVideo.get(msg.videoMeta.streamId)?.(); // re-label if track already arrived
      return;
    }

    // A peer we already hold, but from a different session, means their client
    // restarted — refreshed the page, relaunched, crashed and came back. The
    // connection we're holding is to something that no longer exists and will
    // never speak again, so replace it instead of trying to negotiate on it.
    //
    // Connection state can't be used for this. A direct connection dies fast
    // enough that "failed" cleans it up, but a relayed one (which is what
    // "Hide my IP on calls" makes every connection) stays alive against a TURN
    // allocation that outlives the browser tab — so the corpse reports itself
    // connected for minutes, the roster still lists them, and the rejoining
    // peer's offers land on a socket nobody is home at. That is the bug this
    // exists to close: with IP hiding on, refreshing killed the call both ways.
    let peer = this.peers.get(from);
    if (peer && msg.s && peer.session && peer.session !== msg.s) {
      this.removePeer(from);
      peer = null;
    }
    if (!peer) peer = this.addPeer(from);
    if (msg.s) peer.session = msg.s;
    const { pc } = peer;

    try {
      if (msg.description) {
        const offerCollision =
          msg.description.type === "offer" &&
          (peer.makingOffer || pc.signalingState !== "stable");
        peer.ignoreOffer = !peer.polite && offerCollision;
        if (peer.ignoreOffer) return;

        await pc.setRemoteDescription(msg.description);
        if (msg.description.type === "offer") {
          const answer = await pc.createAnswer();
          answer.sdp = this._tune(peer, answer.sdp);
          await pc.setLocalDescription(answer);
          this.send(from, { description: pc.localDescription });
        }
        this._applySenderParams(pc);
      } else if (msg.candidate) {
        try {
          await pc.addIceCandidate(msg.candidate);
        } catch (err) {
          if (!peer.ignoreOffer) throw err;
        }
      }
    } catch (err) {
      console.warn("voice signal error", err);
    }
  }

  addPeer(peerId) {
    const pc = new RTCPeerConnection({
      iceServers: this.iceServers,
      iceTransportPolicy: this.iceTransportPolicy,
    });
    const peer = {
      pc,
      makingOffer: false,
      ignoreOffer: false,
      // Deterministic, opposite roles on the two ends.
      polite: this.selfPeerId > peerId,
      session: "", // their mesh instance's nonce; a change means they restarted
      watching: new Set(), // their stream ids we've already acknowledged watching
      audioEl: null, // their voice
      micStreamId: "", // which remote stream that voice came in on
      auxEls: new Map(), // streamId -> <audio> for sound riding a screen share
      hifiTracks: new Set(), // remote audio tracks that are shared media, not a voice
      videoKeys: new Set(), // remote video tile keys from this peer
    };
    this.peers.set(peerId, peer);

    const mic = this._micTrack();
    if (mic) pc.addTrack(mic, this.sendStream || this.localStream);
    // A browser guest answers our offer and never gets to add an m-line of its
    // own for a track we didn't ask for. So if we're not sending video (camera
    // off — the common case), the guest's camera would have nowhere to land and
    // we'd never see them. Offer a receive-only video slot up front.
    if (peerId.startsWith("guest:")) {
      try {
        pc.addTransceiver("video", { direction: "recvonly" });
      } catch {}
    }
    // A peer that joins while we're already sending video (camera and/or screen)
    // gets those sources too, with a note about each one's kind.
    for (const kind of ["screen", "camera"]) {
      const src = this.videoSources[kind];
      if (!src?.stream.getVideoTracks().length) continue;
      src.senders.set(peerId, this._addSourceTracks(pc, src.stream));
      this.send(peerId, { videoMeta: { streamId: src.stream.id, kind } });
    }

    pc.onnegotiationneeded = async () => {
      try {
        peer.makingOffer = true;
        // Created explicitly (rather than the implicit setLocalDescription())
        // so the Opus parameters can be raised before the peer ever sees them.
        const offer = await pc.createOffer();
        offer.sdp = this._tune(peer, offer.sdp);
        await pc.setLocalDescription(offer);
        this._applySenderParams(pc);
        this.send(peerId, { description: pc.localDescription });
      } catch (err) {
        console.warn("negotiation error", err);
      } finally {
        peer.makingOffer = false;
      }
    };
    pc.onicecandidate = ({ candidate }) => {
      if (candidate) this.send(peerId, { candidate });
    };
    pc.ontrack = ({ track, streams }) => {
      if (track.kind === "video") {
        // A remote camera/screen. One tile per stream, so a peer sending both
        // shows two tiles. The kind may arrive slightly before or after; re-emit
        // when it does so the label corrects itself.
        const stream = streams[0] || new MediaStream([track]);
        const key = `${peerId}:${stream.id}`;
        peer.videoKeys.add(key);
        // An unlabeled video track defaults to "camera": the kind note may be
        // lost or late (a browser guest sends it before we're even in the call),
        // and the UI only renders "camera"/"screen" tiles. Guessing camera shows
        // the person; dropping the track shows nothing.
        const emit = () => {
          peer.videoKeys.add(key);
          this._pendingVideo.set(stream.id, emit);
          const kind = this.remoteKinds.get(stream.id) || "camera";
          this.onVideo(key, stream, { peerId, kind });
          // Sharing into silence is unnerving — you can't tell whether anyone
          // is looking. Tell the sharer once per stream that we're watching.
          if (kind === "screen" && !peer.watching.has(stream.id)) {
            peer.watching.add(stream.id);
            this.send(peerId, { watching: stream.id });
          }
        };
        emit();
        const clear = () => {
          peer.videoKeys.delete(key);
          this._pendingVideo.delete(stream.id);
          this.onVideo(key, null);
        };
        // A track NEGOTIATED before frames flow (joining an existing share)
        // arrives "muted", which would clear the tile — but then "unmute" fires
        // once frames arrive, so we must re-show it. Only "ended" is a real gone.
        track.addEventListener("ended", clear);
        track.addEventListener("mute", clear);
        track.addEventListener("unmute", emit);
        return;
      }
      // Audio. A peer sends their voice, and may also send the sound of what
      // they're sharing — a second audio track, riding the screen stream. Both
      // need their own element (one element plays one stream), but only the
      // voice should drive the "speaking" ring, and a shared video's soundtrack
      // must not be mistaken for someone talking.
      const stream = streams[0] || new MediaStream([track]);
      const shared = this.remoteKinds.has(stream.id) || (peer.micStreamId && peer.micStreamId !== stream.id);
      if (shared) {
        // Remember it's shared media: the next offer/answer we build asks for
        // this m-line in stereo, and only the receiving side can ask.
        peer.hifiTracks.add(track);
        let aux = peer.auxEls.get(stream.id);
        if (!aux) {
          aux = new Audio();
          aux.autoplay = true;
          peer.auxEls.set(stream.id, aux);
          applySink(aux, this.devices.speaker);
        }
        aux.srcObject = stream;
        // When the share ends, stop holding onto its element.
        track.addEventListener("ended", () => {
          aux.srcObject = null;
          peer.auxEls.delete(stream.id);
        });
        this._applyAudio(peerId, peer);
        return;
      }
      let el = peer.audioEl;
      if (!el) {
        el = new Audio();
        el.autoplay = true;
        peer.audioEl = el;
        applySink(el, this.devices.speaker); // route to the chosen speaker
      }
      peer.micStreamId = stream.id;
      el.srcObject = stream;
      this._applyAudio(peerId, peer); // honor deafen / per-peer volume on (re)connect
      this._addAnalyser(peerId, stream);
    };
    pc.onconnectionstatechange = () => {
      if (["failed", "closed"].includes(pc.connectionState)) this.removePeer(peerId);
    };

    this.emitRoster();
    return peer;
  }

  removePeer(peerId) {
    const peer = this.peers.get(peerId);
    if (!peer) return;
    try {
      peer.pc.close();
    } catch {}
    if (peer.audioEl) peer.audioEl.srcObject = null;
    for (const el of peer.auxEls.values()) el.srcObject = null;
    peer.auxEls.clear();
    this.analysers.delete(peerId);
    // Drop any video tiles this peer was showing.
    for (const key of peer.videoKeys) this.onVideo(key, null);
    // Forget our senders to this peer across all local video sources.
    for (const kind of ["screen", "camera"]) this.videoSources[kind]?.senders.delete(peerId);
    this.peers.delete(peerId);
    this.emitRoster();
  }

  send(toPeerId, payload) {
    this.relay(toPeerId, JSON.stringify({ channelId: this.channelId, s: this.sessionId, ...payload }));
  }

  emitRoster() {
    this.onRoster([...this.peers.keys()]);
  }
}
