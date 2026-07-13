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

const ICE_SERVERS = [{ urls: "stun:stun.l.google.com:19302" }];

export class VoiceMesh {
  // selfPeerId: our libp2p peer id; channelId: the voice room; relay(to, json)
  // sends a signaling blob; onRoster(peerIds[]) reports participant changes.
  constructor({ selfPeerId, channelId, relay, onRoster, onSpeaking, onVideo, onVideoState }) {
    this.selfPeerId = selfPeerId;
    this.channelId = channelId;
    this.relay = relay;
    this.onRoster = onRoster || (() => {});
    this.onSpeaking = onSpeaking || (() => {});
    // onVideo(key, stream|null, meta): a remote video source appeared/vanished.
    //   key = `${peerId}:${streamId}`; meta = { peerId, kind }.
    // onVideoState(kind, on): our own camera/screen toggled.
    this.onVideo = onVideo || (() => {});
    this.onVideoState = onVideoState || (() => {});
    this.peers = new Map(); // peerId -> { pc, makingOffer, ignoreOffer, audioEl, videoKeys }
    this.localStream = null;
    this.muted = false;
    this.audioCtx = null;
    this.analysers = new Map(); // key ("self"|peerId) -> { analyser, data }
    this._monitor = null;
    this._lastSpeaking = "";
    // Local video sources: independent "screen" and "camera", each a stream +
    // per-peer sender so either can be added/removed without touching the other.
    this.videoSources = { screen: null, camera: null }; // kind -> { stream, senders: Map }
    // Remote stream-id -> kind ("screen"|"camera"), learned from a signaling note.
    this.remoteKinds = new Map();
    this._pendingVideo = new Map(); // streamId -> re-emit fn (kind arrived late)
  }

  async start() {
    this.localStream = await navigator.mediaDevices.getUserMedia({ audio: true });
    this._addAnalyser("self", this.localStream);
    this._startMonitor();
  }

  stop() {
    this.stopVideo("screen");
    this.stopVideo("camera");
    for (const id of [...this.peers.keys()]) this.removePeer(id);
    if (this._monitor) clearInterval(this._monitor);
    this._monitor = null;
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
      if (!this.audioCtx) {
        this.audioCtx = new (window.AudioContext || window.webkitAudioContext)();
      }
      const src = this.audioCtx.createMediaStreamSource(stream);
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
      for (const [key, { analyser, data }] of this.analysers) {
        analyser.getByteTimeDomainData(data);
        let sum = 0;
        for (let i = 0; i < data.length; i++) {
          const v = (data[i] - 128) / 128;
          sum += v * v;
        }
        if (Math.sqrt(sum / data.length) > 0.04) speaking.add(key);
      }
      const sig = [...speaking].sort().join(",");
      if (sig !== this._lastSpeaking) {
        this._lastSpeaking = sig;
        this.onSpeaking([...speaking]);
      }
    }, 150);
  }

  setMuted(muted) {
    this.muted = muted;
    if (this.localStream) {
      this.localStream.getAudioTracks().forEach((t) => (t.enabled = !muted));
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
          ? await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: 30 }, audio: false })
          : await navigator.mediaDevices.getUserMedia({ video: { width: 1280, height: 720 } });
    } catch {
      return null; // user dismissed the picker / denied the camera
    }
    const senders = new Map();
    this.videoSources[kind] = { stream, senders };
    const track = stream.getVideoTracks()[0];
    // Fires on the browser's "Stop sharing" chrome or a camera unplug.
    track.addEventListener("ended", () => this.stopVideo(kind));
    for (const [peerId, peer] of this.peers) {
      try {
        senders.set(peerId, peer.pc.addTrack(track, stream));
      } catch (err) {
        console.warn("video addTrack", err);
      }
      // Tell the peer which kind this new stream is, so it labels the tile.
      this.send(peerId, { videoMeta: { streamId: stream.id, kind } });
    }
    this.onVideoState(kind, true);
    return stream;
  }

  stopVideo(kind) {
    const src = this.videoSources[kind];
    if (!src) return;
    for (const [peerId, peer] of this.peers) {
      const sender = src.senders.get(peerId);
      if (sender) {
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

    // A note about which kind a remote video stream is (camera vs screen).
    if (msg.videoMeta) {
      this.remoteKinds.set(msg.videoMeta.streamId, msg.videoMeta.kind);
      this._pendingVideo.get(msg.videoMeta.streamId)?.(); // re-label if track already arrived
      return;
    }

    const peer = this.peers.get(from) || this.addPeer(from);
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
          await pc.setLocalDescription();
          this.send(from, { description: pc.localDescription });
        }
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
    const pc = new RTCPeerConnection({ iceServers: ICE_SERVERS });
    const peer = {
      pc,
      makingOffer: false,
      ignoreOffer: false,
      // Deterministic, opposite roles on the two ends.
      polite: this.selfPeerId > peerId,
      audioEl: null,
      videoKeys: new Set(), // remote video tile keys from this peer
    };
    this.peers.set(peerId, peer);

    if (this.localStream) {
      for (const track of this.localStream.getTracks()) {
        pc.addTrack(track, this.localStream);
      }
    }
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
      const track = src?.stream.getVideoTracks()[0];
      if (track) {
        try {
          src.senders.set(peerId, pc.addTrack(track, src.stream));
        } catch {}
        this.send(peerId, { videoMeta: { streamId: src.stream.id, kind } });
      }
    }

    pc.onnegotiationneeded = async () => {
      try {
        peer.makingOffer = true;
        await pc.setLocalDescription();
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
        const emit = () =>
          this.onVideo(key, stream, { peerId, kind: this.remoteKinds.get(stream.id) || "camera" });
        emit();
        this._pendingVideo.set(stream.id, emit);
        const clear = () => {
          peer.videoKeys.delete(key);
          this._pendingVideo.delete(stream.id);
          this.onVideo(key, null);
        };
        track.addEventListener("ended", clear);
        track.addEventListener("mute", clear);
        return;
      }
      let el = peer.audioEl;
      if (!el) {
        el = new Audio();
        el.autoplay = true;
        peer.audioEl = el;
      }
      el.srcObject = streams[0];
      this._addAnalyser(peerId, streams[0]);
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
    this.analysers.delete(peerId);
    // Drop any video tiles this peer was showing.
    for (const key of peer.videoKeys) this.onVideo(key, null);
    // Forget our senders to this peer across all local video sources.
    for (const kind of ["screen", "camera"]) this.videoSources[kind]?.senders.delete(peerId);
    this.peers.delete(peerId);
    this.emitRoster();
  }

  send(toPeerId, payload) {
    this.relay(toPeerId, JSON.stringify({ channelId: this.channelId, ...payload }));
  }

  emitRoster() {
    this.onRoster([...this.peers.keys()]);
  }
}
