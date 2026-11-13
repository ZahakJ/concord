// Which ICE servers a call uses.
//
// The servers come from the user's own rendezvous: its TURN relay and, when the
// operator runs one, its own STUN responder. A call needs a STUN answer to get
// two people behind home routers connected directly. A public STUN server could
// give it, but that is a third party that learns the caller's IP at every call
// start, so by default it is never asked: "No third-party call servers"
// (Settings > Privacy & safety) is on unless the user turns it off. On, a call
// between two home networks needs the rendezvous's own responder, or goes
// through its relay, or does not connect. Off, the public server is added, and
// only when the rendezvous offers no STUN server of its own.
//
// callIce() itself takes the switch as an argument and treats a missing one as
// off; the caller passes the preference (App.svelte), whose default is on.

export const PUBLIC_STUN = "stun:stun.l.google.com:19302";

const urlsOf = (s) => [].concat(s?.urls ?? []).map(String);

export function hasOwnStun(servers) {
  return (servers || []).some((s) => urlsOf(s).some((u) => /^stuns?:/i.test(u)));
}

// The public server under any of its names (stun.l.google.com, stun1…stun4, any
// port). The rendezvous never lists it, but the switch must not depend on that.
const isPublic = (u) => /^stuns?:stun\d*\.l\.google\.com([:?]|$)/i.test(u.trim());

export function callIce(servers, { noThirdParty = false } = {}) {
  const own = Array.isArray(servers) ? servers.filter((s) => urlsOf(s).length) : [];
  if (noThirdParty) {
    // Self-enforcing: even a rendezvous answer that lists the public server
    // does not get it past the switch.
    return own
      .map((s) => (urlsOf(s).some(isPublic) ? { ...s, urls: urlsOf(s).filter((u) => !isPublic(u)) } : s))
      .filter((s) => urlsOf(s).length);
  }
  if (hasOwnStun(own)) return own;
  return [{ urls: [PUBLIC_STUN] }, ...own];
}

// Whether a call started now would contact the public STUN server.
export function usesThirdParty(servers, opts) {
  return callIce(servers, opts).some((s) => urlsOf(s).some(isPublic));
}
