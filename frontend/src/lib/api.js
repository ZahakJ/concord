// api.js is the frontend's single door to the Go backend. It works with two
// transports, chosen automatically:
//
//   - Wails desktop: calls the bindings Wails injects at window.go.main.App.*
//     and subscribes to events via window.runtime.EventsOn.
//   - Browser-served: POSTs to /rpc and streams events from /events (SSE).
//
// The rest of the UI is transport-agnostic — it only ever calls api.* and on().

function wailsBindings() {
  return typeof window !== "undefined" && window.go && window.go.main
    ? window.go.main.App
    : null;
}

const isWails = () => wailsBindings() !== null;

export function hasBackend() {
  return true; // one of the two transports is always available at runtime
}

async function call(name, ...args) {
  const b = wailsBindings();
  if (b && typeof b[name] === "function") {
    return b[name](...args);
  }
  // Browser transport.
  const res = await fetch("/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method: name, args }),
  });
  if (!res.ok) throw new Error(`rpc ${name}: HTTP ${res.status}`);
  const body = await res.json();
  if (body.error) throw new Error(body.error);
  return body.result;
}

export const api = {
  hasBackend,
  getBootstrap: () => call("GetBootstrap"),
  setBootstrap: (addrs) => call("SetBootstrap", addrs),
  setBootstrapLive: (addrs) => call("SetBootstrapLive", addrs),
  session: () => call("Session"),
  login: (passphrase) => call("Login", passphrase),
  identity: () => call("Identity"),
  guilds: () => call("Guilds"),
  createGuild: (name) => call("CreateGuild", name),
  createChannel: (guildID, name) => call("CreateChannel", guildID, name),
  renameGuild: (guildID, name) => call("RenameGuild", guildID, name),
  leaveGuild: (guildID) => call("LeaveGuild", guildID),
  deleteMessage: (channelID, messageID) => call("DeleteMessage", channelID, messageID),
  editMessage: (channelID, messageID, content) =>
    call("EditMessage", channelID, messageID, content),
  toggleReaction: (channelID, messageID, emoji) =>
    call("ToggleReaction", channelID, messageID, emoji),
  inviteCode: (guildID) => call("InviteCode", guildID),
  joinViaInvite: (code) => call("JoinViaInvite", code),
  messages: (channelID) => call("Messages", channelID),
  sendMessage: (channelID, content, replyTo = "") =>
    call("SendMessage", channelID, content, replyTo),
  members: (guildID) => call("Members", guildID),
  removeMember: (guildID, fingerprint) => call("RemoveMember", guildID, fingerprint),
  contacts: () => call("Contacts"),
  verify: (peerID) => call("Verify", peerID),
  joinVoice: (channelID) => call("JoinVoice", channelID),
  leaveVoice: (channelID) => call("LeaveVoice", channelID),
  relaySignal: (toPeerID, data) => call("RelaySignal", toPeerID, data),
  sendTyping: (channelID) => call("SendTyping", channelID),
  setDisplayName: (name) => call("SetDisplayName", name),
  setProfile: (name, status, emoji, color, avatar) =>
    call("SetProfile", name, status, emoji, color, avatar),
  verifyFingerprint: (fingerprint) => call("VerifyFingerprint", fingerprint),
  pinMessage: (channelID, messageID) => call("PinMessage", channelID, messageID),
  searchMessages: (query) => call("SearchMessages", query),
};

// Shared SSE connection for the browser transport.
let eventSource = null;
function sse() {
  if (!eventSource) eventSource = new EventSource("/events");
  return eventSource;
}

// on subscribes to a backend event and returns an unsubscribe function.
export function on(event, handler) {
  if (isWails()) {
    const rt = window.runtime;
    if (rt && rt.EventsOn) return rt.EventsOn(event, handler);
    return () => {};
  }
  const listener = (e) => {
    let data = null;
    try {
      data = e.data ? JSON.parse(e.data) : null;
    } catch {
      data = null;
    }
    handler(data);
  };
  sse().addEventListener(event, listener);
  return () => sse().removeEventListener(event, listener);
}
