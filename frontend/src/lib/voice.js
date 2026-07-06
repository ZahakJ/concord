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
  constructor({ selfPeerId, channelId, relay, onRoster, onSpeaking }) {
    this.selfPeerId = selfPeerId;
    this.channelId = channelId;
    this.relay = relay;
    this.onRoster = onRoster || (() => {});
    this.onSpeaking = onSpeaking || (() => {});
    this.peers = new Map(); // peerId -> { pc, makingOffer, ignoreOffer, audioEl }
    this.localStream = null;
    this.muted = false;
    this.audioCtx = null;
    this.analysers = new Map(); // key ("self"|peerId) -> { analyser, data }
    this._monitor = null;
    this._lastSpeaking = "";
  }

  async start() {
    this.localStream = await navigator.mediaDevices.getUserMedia({ audio: true });
    this._addAnalyser("self", this.localStream);
    this._startMonitor();
  }

  stop() {
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
    };
    this.peers.set(peerId, peer);

    if (this.localStream) {
      for (const track of this.localStream.getTracks()) {
        pc.addTrack(track, this.localStream);
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
    pc.ontrack = ({ streams }) => {
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
