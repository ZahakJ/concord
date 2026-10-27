#!/usr/bin/env python3
"""concord-rns: Concord's Reticulum (RNS) sidecar.

Concord (Go) runs this as a child process and speaks a tiny binary protocol
over stdin/stdout. Every frame Concord hands over is already MLS ciphertext
plus a bundle header; this process never sees a key or any plaintext. It just
moves opaque bytes between Concord and the Reticulum mesh.

Framing, both directions: 1 byte type, 2 bytes big-endian length, payload.
  Go -> sidecar   0x01 SEND      payload: one frame to fan out to peers
                  0x02 ANNOUNCE  payload: empty; announce now
  sidecar -> Go   0x81 RECV      payload: one frame that arrived
                  0x82 READY     payload: our destination hash (hex)
                  0x83 ERROR     payload: UTF-8 reason (fatal ones precede exit)
                  0x84 PEERS     payload: 2-byte BE count of live peers
Logs go to stderr only. The process exits when stdin closes.

IDENTITY: the RNS identity is made fresh in memory at every start, never
written to disk, and replaced every day (--rotate-interval). It exists only so
RNS can route; Concord's security comes from MLS, and a long-lived routing
identity would only let listeners follow a device around.

LEGAL: amateur radio rules (FCC 97.113 and equivalents elsewhere) forbid
messages encoded to obscure their meaning. Concord only ever sends ciphertext,
so this sidecar fails closed and refuses to start when:
  - any enabled interface is an AX25KISSInterface, or sets callsign or
    id_callsign (amateur station identification);
  - any frequency is in an amateur-only band (50-54, 144-148, 222-225,
    420-450, 1240-1300 MHz), or an RNode interface has no frequency;
  - an interface type that can drive an arbitrary radio (KISSInterface,
    SerialInterface, PipeInterface, WeaveInterface, or any type not known to
    be IP-only) lacks `concord_non_amateur = yes` in its section, the user's
    statement that it is not on amateur spectrum;
  - no interface is enabled at all, or no config exists;
  - RNS attached to a shared instance, whose interfaces we cannot inspect.
The 902-928 MHz and 863-870 MHz ISM bands are licence-free and fine.

Usage: concord-rns.py --config DIR [--announce-interval S] [--rotate-interval S]
                      [--check-only]
"""

import argparse
import os
import struct
import sys
import threading
import time

T_SEND, T_ANNOUNCE = 0x01, 0x02
T_RECV, T_READY, T_ERROR, T_PEERS = 0x81, 0x82, 0x83, 0x84

APP_NAME, ASPECT = "concord", "bundle"
MAX_PEERS = 256  # table size; bounds memory
FANOUT = 8  # copies of one frame we put on the air, at most
MAX_IN = 4096  # largest Go->sidecar payload we will even read
PATH_REQ_PER_PEER = 300.0  # seconds between path requests for one peer
PATH_REQ_PER_MIN = 10  # path requests per minute, all peers together

# Amateur-only allocations (US Part 97; similar elsewhere), in Hz.
HAM_ONLY_BANDS = [
    (50_000_000, 54_000_000),
    (144_000_000, 148_000_000),
    (222_000_000, 225_000_000),
    (420_000_000, 450_000_000),
    (1_240_000_000, 1_300_000_000),
]
# Interfaces that only ever speak IP: no radio of their own to worry about.
IP_ONLY = {"AutoInterface", "BackboneInterface", "BackboneClientInterface",
           "TCPServerInterface", "TCPClientInterface", "UDPInterface", "I2PInterface"}
# Radios whose frequency is in the config, so the band can be checked.
TUNED = {"RNodeInterface", "RNodeMultiInterface"}

_out = None
_out_lock = threading.Lock()


def emit(kind, payload=b""):
    if _out is None or len(payload) > 0xFFFF:
        return
    with _out_lock:
        try:
            _out.write(struct.pack(">BH", kind, len(payload)) + payload)
        except OSError:
            os._exit(0)  # Concord is gone


def log(msg):
    sys.stderr.write("concord-rns: %s\n" % msg)
    sys.stderr.flush()


def fail(reason, code=2):
    log(reason)
    emit(T_ERROR, reason.encode("utf-8", "replace")[:1000])
    os._exit(code)


def _yes(c, k):
    return str(c.get(k, "")).strip().lower() in ("yes", "true", "1", "on", "y")


def _walk(section, path):
    yield path, section
    for k in section.sections:
        yield from _walk(section[k], path + "/" + k)


def check_config(configdir):
    """(violations, enabled interface count) for configdir/config."""
    path = os.path.join(configdir, "config")
    if not os.path.isfile(path):
        return ["no RNS config at %s" % path], 0
    from RNS.vendor.configobj import ConfigObj
    cfg = ConfigObj(path)
    out, enabled = [], 0
    ifaces = cfg.get("interfaces", {})
    for name in getattr(ifaces, "sections", []):
        c = ifaces[name]
        if not (_yes(c, "interface_enabled") or _yes(c, "enabled")):
            continue
        enabled += 1
        kind = str(c.get("type", ""))
        if kind == "AX25KISSInterface":
            out.append("%s: AX25KISSInterface is amateur radio" % name)
        elif kind == "RNodeInterface" and "frequency" not in c:
            out.append("%s: RNodeInterface without a frequency" % name)
        elif kind == "RNodeMultiInterface":
            subs = c.sections
            if not subs or any("frequency" not in c[s] for s in subs):
                out.append("%s: RNodeMultiInterface sub-interface without a frequency" % name)
        elif kind not in IP_ONLY and kind not in TUNED and not _yes(c, "concord_non_amateur"):
            out.append("%s: %s can drive any radio; if it is not on amateur spectrum, "
                       "set concord_non_amateur = yes in its section" % (name, kind or "untyped interface"))
        for sub, s in _walk(c, name):
            for key in ("callsign", "id_callsign"):
                if str(s.get(key, "")).strip():
                    out.append("%s: %s set (amateur station identification)" % (sub, key))
            if "frequency" in s:
                try:
                    f = int(float(str(s["frequency"])))
                except ValueError:
                    out.append("%s: unreadable frequency %r" % (sub, s["frequency"]))
                    continue
                for lo, hi in HAM_ONLY_BANDS:
                    if lo <= f <= hi:
                        out.append("%s: %.3f MHz is an amateur-only band" % (sub, f / 1e6))
    return out, enabled


class PeerTable:
    """Concord nodes heard announcing, forgotten when they go quiet."""

    def __init__(self, ttl, cap=MAX_PEERS, fanout=FANOUT, clock=time.monotonic):
        self.ttl, self.cap, self.fanout, self.clock = ttl, cap, fanout, clock
        self.lock = threading.Lock()
        self.peers = {}  # hash -> (destination, last heard)

    def heard(self, h, dest):
        with self.lock:
            new = h not in self.peers
            self.peers[h] = (dest, self.clock())
            while len(self.peers) > self.cap:  # drop whoever is stalest
                del self.peers[min(self.peers, key=lambda k: self.peers[k][1])]
            return new

    def expire(self):
        with self.lock:
            cut = self.clock() - self.ttl
            old = [h for h, (_, t) in self.peers.items() if t < cut]
            for h in old:
                del self.peers[h]
            return len(old)

    def targets(self):
        """The most recently heard live peers, at most fanout of them."""
        self.expire()
        with self.lock:
            live = sorted(self.peers.items(), key=lambda kv: kv[1][1], reverse=True)
        return [(h, d) for h, (d, _) in live[:self.fanout]]

    def __len__(self):
        with self.lock:
            return len(self.peers)


class PathLimiter:
    """Bounds path requests: each one is a broadcast on every interface."""

    def __init__(self, per_peer=PATH_REQ_PER_PEER, per_min=PATH_REQ_PER_MIN, clock=time.monotonic):
        self.per_peer, self.per_min, self.clock = per_peer, per_min, clock
        self.lock = threading.Lock()
        self.last = {}
        self.recent = []

    def allow(self, h):
        with self.lock:
            now = self.clock()
            self.recent = [t for t in self.recent if now - t < 60]
            if len(self.recent) >= self.per_min:
                return False
            if h in self.last and now - self.last[h] < self.per_peer:
                return False
            if len(self.last) > 4 * MAX_PEERS:
                self.last = {k: t for k, t in self.last.items() if now - t < self.per_peer}
            self.last[h] = now
            self.recent.append(now)
            return True


class Sidecar:
    def __init__(self, configdir, interval, rotate):
        import RNS
        self.RNS = RNS
        self.interval, self.rotate_every = interval, rotate
        r = RNS.Reticulum(configdir=configdir, loglevel=RNS.LOG_NOTICE, logdest=log)
        if getattr(r, "is_connected_to_shared_instance", False):
            fail("refusing to run on a shared RNS instance: its interfaces cannot be checked "
                 "for amateur spectrum; set share_instance = No in %s/config" % configdir, 7)
        self.peers = PeerTable(ttl=3 * interval)
        self.paths = PathLimiter()
        self.mdu = RNS.Packet.ENCRYPTED_MDU
        self.dest = self.prev = None
        self.dlock = threading.Lock()
        self.aspect_filter = APP_NAME + "." + ASPECT
        RNS.Transport.register_announce_handler(self)
        self.rotate()

    def rotate(self):
        """A fresh in-memory identity and destination, announced. The previous
        one keeps receiving until the next rotation, so peers that have not
        heard the new announce yet still get through."""
        RNS = self.RNS
        # SINGLE, not PLAIN/GROUP: RNS drops PLAIN and GROUP packets that
        # have travelled more than one hop (Transport.inbound), while SINGLE
        # packets follow the path an announce established across the mesh.
        d = RNS.Destination(RNS.Identity(), RNS.Destination.IN, RNS.Destination.SINGLE, APP_NAME, ASPECT)
        d.set_proof_strategy(RNS.Destination.PROVE_NONE)
        d.set_packet_callback(self.on_packet)
        with self.dlock:
            old, self.prev, self.dest = self.prev, self.dest, d
        if old is not None:
            try:
                RNS.Transport.deregister_destination(old)
            except Exception as e:
                log("deregister failed: %s" % e)
        self.rotated = time.monotonic()
        self.announce()

    def ours(self, h):
        with self.dlock:
            return any(d is not None and d.hash == h for d in (self.dest, self.prev))

    def received_announce(self, destination_hash, announced_identity, app_data):
        if announced_identity is None or self.ours(destination_hash):
            return
        RNS = self.RNS
        d = RNS.Destination(announced_identity, RNS.Destination.OUT, RNS.Destination.SINGLE, APP_NAME, ASPECT)
        if self.peers.heard(destination_hash, d):
            self.peers.expire()
            log("peer %s (%d live)" % (destination_hash.hex(), len(self.peers)))
            emit(T_PEERS, struct.pack(">H", len(self.peers)))

    def on_packet(self, data, packet):
        if 0 < len(data) <= self.mdu:
            emit(T_RECV, bytes(data))

    def announce(self):
        try:
            with self.dlock:
                d = self.dest
            d.announce()
        except Exception as e:
            log("announce failed: %s" % e)

    def send(self, frame):
        if not frame or len(frame) > self.mdu:
            log("dropping %d-byte frame: exceeds MTU %d" % (len(frame), self.mdu))
            emit(T_ERROR, b"frame exceeds MTU")
            return
        RNS = self.RNS
        for h, d in self.peers.targets():
            try:
                if not RNS.Transport.has_path(h) and self.paths.allow(h):
                    RNS.Transport.request_path(h)
                RNS.Packet(d, frame, create_receipt=False).send()
            except Exception as e:
                log("send to %s failed: %s" % (h.hex(), e))

    def ticker(self):
        while True:
            time.sleep(self.interval)
            if self.peers.expire():
                emit(T_PEERS, struct.pack(">H", len(self.peers)))
            if time.monotonic() - self.rotated >= self.rotate_every:
                self.rotate()
            else:
                self.announce()

    def hash_hex(self):
        with self.dlock:
            return self.dest.hash.hex()


def read_exact(f, n):
    buf = b""
    while len(buf) < n:
        chunk = f.read(n - len(buf))
        if not chunk:
            return None
        buf += chunk
    return buf


def main():
    global _out
    # Real stdout carries only protocol frames. Everything else (RNS prints,
    # stray library output) goes to stderr by pointing fd 1 at fd 2.
    _out = os.fdopen(os.dup(1), "wb", buffering=0)
    os.dup2(2, 1)
    sys.stdout = sys.stderr

    ap = argparse.ArgumentParser()
    ap.add_argument("--config", required=True)
    ap.add_argument("--announce-interval", type=int, default=600)
    ap.add_argument("--rotate-interval", type=int, default=86400)
    ap.add_argument("--check-only", action="store_true")
    a = ap.parse_args()
    try:
        import RNS  # noqa: F401
    except Exception as e:
        fail("python cannot import RNS (pip install rns): %s" % e, 3)
    bad, enabled = check_config(a.config)
    if bad:
        fail("refusing to start: Concord sends only ciphertext, which amateur "
             "radio rules forbid (FCC 97.113). " + "; ".join(bad), 4)
    if enabled == 0:
        fail("no interface enabled: add one (an RNode, TCP, serial...) to %s/config; "
             "see docs/transports/reticulum.md" % a.config, 8)
    if a.check_only:
        emit(T_READY, b"check-ok")
        return 0
    try:
        sc = Sidecar(a.config, max(10, a.announce_interval), max(60, a.rotate_interval))
    except Exception as e:
        fail("reticulum failed to start: %s" % e, 5)
    threading.Thread(target=sc.ticker, daemon=True).start()
    emit(T_READY, sc.hash_hex().encode())
    stdin = sys.stdin.buffer
    while True:
        hdr = read_exact(stdin, 3)
        if hdr is None:
            break
        kind, n = struct.unpack(">BH", hdr)
        if n > MAX_IN:
            fail("oversized frame from Concord", 6)
        body = read_exact(stdin, n)
        if body is None:
            break
        if kind == T_SEND:
            sc.send(body)
        elif kind == T_ANNOUNCE:
            sc.announce()
    log("stdin closed, exiting")
    sc.RNS.exit(0)  # tears down interfaces; never returns


if __name__ == "__main__":
    sys.exit(main())
