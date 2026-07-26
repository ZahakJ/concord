// sdp.js: tune the Opus parameters in an offer/answer before it goes out.
//
// Left alone, a browser negotiates Opus conservatively — roughly 32 kbit/s
// mono, tuned for "a phone call over a bad network". That's the single biggest
// reason a P2P call sounds thinner than it needs to: the codec is fine, the
// settings are timid. These knobs live only in SDP, so this is the one place
// that has to know the wire format.
//
// Which side sets what is the confusing part: per RFC 7587 the fmtp attributes
// you send describe what YOU want to RECEIVE. So raising maxaveragebitrate here
// asks the peer to send us better audio; they run this same code, so both
// directions improve. Our own encoder ceiling is set separately, on the sender
// (see voice.js).

// tuneOpus rewrites every audio section's opus fmtp line.
//   bitrate     — what we ask peers to send us, bits/s
//   hifiIndexes — positions of the m-lines that carry shared media (a screen
//                 share's sound) rather than a voice: those get stereo and a
//                 higher ceiling, because music squeezed through a mono speech
//                 codec is the one thing everybody notices.
//
// Indexes, not mids: a brand-new transceiver has no mid until the description
// is applied, which is exactly when we need to tune the offer that introduces
// it. m-lines are in transceiver order, so position is the reliable key.
export function tuneOpus(sdp, { bitrate = 64000, hifiIndexes = [] } = {}) {
  if (!sdp) return sdp;
  const hifi = new Set(hifiIndexes);
  // Split into the session part plus one chunk per m-line, edit, rejoin.
  const parts = sdp.split(/(?=^m=)/m);
  let mIndex = -1;
  return parts
    .map((sec) => {
      if (!sec.startsWith("m=")) return sec;
      mIndex++;
      if (!sec.startsWith("m=audio")) return sec;
      const pt = sec.match(/^a=rtpmap:(\d+) opus\/48000/im)?.[1];
      if (!pt) return sec;
      const isHifi = hifi.has(mIndex);
      const want = {
        minptime: "10",
        useinbandfec: "1", // rebuild lost packets from the next one
        usedtx: "0", // no "silence saves bandwidth" gaps mid-word
        maxplaybackrate: "48000", // full band, not 16 kHz speech
        "sprop-maxcapturerate": "48000",
        maxaveragebitrate: String(isHifi ? Math.max(bitrate, 128000) : bitrate),
        ...(isHifi ? { stereo: "1", "sprop-stereo": "1" } : { stereo: "0" }),
      };
      const line = new RegExp(`^a=fmtp:${pt} (.*)$`, "im");
      const existing = sec.match(line);
      // Keep any parameter the browser set that we don't have an opinion on.
      const merged = {};
      if (existing) {
        for (const kv of existing[1].split(";")) {
          const [k, v] = kv.split("=");
          if (k?.trim()) merged[k.trim()] = v;
        }
      }
      Object.assign(merged, want);
      const fmtp = `a=fmtp:${pt} ${Object.entries(merged)
        .map(([k, v]) => (v === undefined ? k : `${k}=${v}`))
        .join(";")}`;
      return existing
        ? sec.replace(line, fmtp)
        : sec.replace(new RegExp(`^(a=rtpmap:${pt} opus/48000.*)$`, "im"), `$1\r\n${fmtp}`);
    })
    .join("");
}
