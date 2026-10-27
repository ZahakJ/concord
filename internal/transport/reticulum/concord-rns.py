#!/usr/bin/env python3
"""concord-rns: Concord's Reticulum (RNS) sidecar.

Concord (Go) runs this as a child process and speaks a tiny binary protocol
over stdin/stdout. Every frame Concord hands over is already MLS ciphertext
plus a bundle header; this process never sees a key or any plaintext. It just
moves opaque bytes between Concord and the Reticulum mesh.

Framing, both directions: 1 byte type, 2 bytes big-endian length, payload.
  Go -> sidecar   0x01 SEND      payload: one frame to fan out to every peer
                  0x02 ANNOUNCE  payload: empty; announce now
  sidecar -> Go   0x81 RECV      payload: one frame that arrived
                  0x82 READY     payload: our destination hash (hex)
                  0x83 ERROR     payload: UTF-8 reason (fatal ones precede exit)
                  0x84 PEERS     payload: 2-byte BE count of known peers
Logs go to stderr only. The process exits when stdin closes.

LEGAL: amateur radio rules (FCC 97.113 and equivalents elsewhere) forbid
messages encoded to obscure their meaning. Concord only ever sends
ciphertext, so this sidecar refuses to start if any enabled interface is
amateur-only: an AX25KISSInterface (which carries a callsign), any interface
configured with an amateur station callsign (callsign / id_callsign), or an
interface whose frequency lies in an amateur-only band (50-54, 144-148,
222-225, 420-450, 1240-1300 MHz). The 902-928 MHz and 863-870 MHz ISM bands
are licence-free and fine.

Usage: concord-rns.py --config DIR [--announce-interval SECONDS] [--check-only]
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
MAX_PEERS = 256
MAX_IN = 4096  # largest Go->sidecar payload we will even read

# Amateur-only allocations (US Part 97; similar elsewhere), in Hz.
HAM_ONLY_BANDS = [
    (50_000_000, 54_000_000),
    (144_000_000, 148_000_000),
    (222_000_000, 225_000_000),
    (420_000_000, 450_000_000),
    (1_240_000_000, 1_300_000_000),
]

# Real stdout carries only protocol frames. Everything else (RNS prints, stray
# library output) is pushed onto stderr by pointing fd 1 at fd 2.
_out = os.fdopen(os.dup(1), "wb", buffering=0)
os.dup2(2, 1)
sys.stdout = sys.stderr
_out_lock = threading.Lock()


def emit(kind, payload=b""):
    if len(payload) > 0xFFFF:
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
    _out.flush()
    os._exit(code)


def _enabled(c):
    def yes(k):
        return str(c.get(k, "")).strip().lower() in ("yes", "true", "1", "on")
    return yes("interface_enabled") or yes("enabled")


def _walk(section, path):
    yield path, section
    for k in section.sections:
        yield from _walk(section[k], path + "/" + k)


def ham_violations(configdir):
    """Reasons this config would put ciphertext on amateur spectrum."""
    path = os.path.join(configdir, "config")
    if not os.path.isfile(path):
        return []
    from RNS.vendor.configobj import ConfigObj
    cfg = ConfigObj(path)
    out = []
    ifaces = cfg.get("interfaces", {})
    for name in getattr(ifaces, "sections", []):
        c = ifaces[name]
        if not _enabled(c):
            continue
        if str(c.get("type", "")) == "AX25KISSInterface":
            out.append("%s: AX25KISSInterface is amateur radio" % name)
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
    return out


class Sidecar:
    def __init__(self, configdir, interval):
        import RNS
        self.RNS = RNS
        self.interval = interval
        self.lock = threading.Lock()
        self.peers = {}  # destination hash -> (RNS.Destination, last heard)
        RNS.Reticulum(configdir=configdir, loglevel=RNS.LOG_NOTICE, logdest=log)
        idpath = os.path.join(configdir, "concord_identity")
        ident = RNS.Identity.from_file(idpath) if os.path.isfile(idpath) else None
        if ident is None:
            ident = RNS.Identity()
            ident.to_file(idpath)
        # SINGLE, not PLAIN/GROUP: RNS drops PLAIN and GROUP packets that
        # have travelled more than one hop (Transport.inbound), while SINGLE
        # packets follow the path an announce established across the mesh.
        self.dest = RNS.Destination(ident, RNS.Destination.IN, RNS.Destination.SINGLE, APP_NAME, ASPECT)
        self.dest.set_proof_strategy(RNS.Destination.PROVE_NONE)
        self.dest.set_packet_callback(self.on_packet)
        self.aspect_filter = APP_NAME + "." + ASPECT
        RNS.Transport.register_announce_handler(self)
        self.mdu = RNS.Packet.ENCRYPTED_MDU

    def received_announce(self, destination_hash, announced_identity, app_data):
        if destination_hash == self.dest.hash or announced_identity is None:
            return
        RNS = self.RNS
        d = RNS.Destination(announced_identity, RNS.Destination.OUT, RNS.Destination.SINGLE, APP_NAME, ASPECT)
        with self.lock:
            new = destination_hash not in self.peers
            self.peers[destination_hash] = (d, time.monotonic())
            while len(self.peers) > MAX_PEERS:  # forget whoever we heard from longest ago
                del self.peers[min(self.peers, key=lambda h: self.peers[h][1])]
            n = len(self.peers)
        if new:
            log("peer %s (%d known)" % (destination_hash.hex(), n))
            emit(T_PEERS, struct.pack(">H", n))

    def on_packet(self, data, packet):
        if 0 < len(data) <= self.mdu:
            emit(T_RECV, bytes(data))

    def announce(self):
        try:
            self.dest.announce()
        except Exception as e:
            log("announce failed: %s" % e)

    def send(self, frame):
        if not frame or len(frame) > self.mdu:
            log("dropping %d-byte frame: exceeds MTU %d" % (len(frame), self.mdu))
            emit(T_ERROR, b"frame exceeds MTU")
            return
        RNS = self.RNS
        with self.lock:
            peers = list(self.peers.items())
        for h, (d, _) in peers:
            try:
                if not RNS.Transport.has_path(h):
                    RNS.Transport.request_path(h)
                RNS.Packet(d, frame, create_receipt=False).send()
            except Exception as e:
                log("send to %s failed: %s" % (h.hex(), e))

    def announcer(self):
        while True:
            time.sleep(self.interval)
            self.announce()


def read_exact(f, n):
    buf = b""
    while len(buf) < n:
        chunk = f.read(n - len(buf))
        if not chunk:
            return None
        buf += chunk
    return buf


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--config", required=True)
    ap.add_argument("--announce-interval", type=int, default=600)
    ap.add_argument("--check-only", action="store_true")
    a = ap.parse_args()
    try:
        import RNS  # noqa: F401
    except Exception as e:
        fail("python cannot import RNS (pip install rns): %s" % e, 3)
    bad = ham_violations(a.config)
    if bad:
        fail("refusing to start: Concord sends only ciphertext, which amateur "
             "radio rules forbid (FCC 97.113). " + "; ".join(bad), 4)
    if a.check_only:
        emit(T_READY, b"check-ok")
        return 0
    try:
        sc = Sidecar(a.config, max(10, a.announce_interval))
    except Exception as e:
        fail("reticulum failed to start: %s" % e, 5)
    sc.announce()
    threading.Thread(target=sc.announcer, daemon=True).start()
    emit(T_READY, sc.dest.hash.hex().encode())
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
    sc.RNS.exit(0)  # persists path tables, tears down interfaces, never returns


if __name__ == "__main__":
    sys.exit(main())
