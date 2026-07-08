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
  getBootstrap: () => call("GetBootstrap"),
  setBootstrap: (addrs) => call("SetBootstrap", addrs),
  setBootstrapLive: (addrs) => call("SetBootstrapLive", addrs),
  session: () => call("Session"),
  logout: () => call("Logout"),
  hasIdentity: () => call("HasIdentity"),
  resetIdentity: () => call("ResetIdentity"),
  revealMnemonic: () => call("RevealMnemonic"),
  restoreFromMnemonic: (phrase, passphrase) => call("RestoreFromMnemonic", phrase, passphrase),
  login: (passphrase) => call("Login", passphrase),
  identity: () => call("Identity"),
  guilds: () => call("Guilds"),
  createGuild: (name) => call("CreateGuild", name),
  notesDM: () => call("NotesDM"),
  startDM: (fingerprint) => call("StartDM", fingerprint),
  createChannel: (guildID, name, type = "", category = "") =>
    call("CreateChannel", guildID, name, type, category),
  createCategory: (guildID, name) => call("CreateCategory", guildID, name),
  deleteChannel: (guildID, channelID) => call("DeleteChannel", guildID, channelID),
  deleteCategory: (guildID, categoryID) => call("DeleteCategory", guildID, categoryID),
  setGuildProfile: (guildID, name, icon, banner, description) =>
    call("SetGuildProfile", guildID, name, icon, banner, description),
  newDMInvite: () => call("NewDMInvite"),
  createGroupDM: (fingerprints) => call("CreateGroupDM", fingerprints),
  addCustomEmoji: (guildID, name, dataURI) => call("AddCustomEmoji", guildID, name, dataURI),
  removeCustomEmoji: (guildID, name) => call("RemoveCustomEmoji", guildID, name),
  setChannelMeta: (guildID, channelID, type, category, position, topic = "") =>
    call("SetChannelMeta", guildID, channelID, type, category, position, topic),
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
  sendAttachment: (channelID, dataURL, w, h, replyTo = "") =>
    call("SendAttachment", channelID, dataURL, w, h, replyTo),
  fetchAttachment: (channelID, blobID, keys, subtype) =>
    call("FetchAttachment", channelID, blobID, keys, subtype),
  sendFile: (channelID, dataURL, filename, replyTo = "") =>
    call("SendFile", channelID, dataURL, filename, replyTo),
  fetchFile: (channelID, blobID, keys, mime) =>
    call("FetchFile", channelID, blobID, keys, mime),
  linkPreview: (url) => call("LinkPreview", url),
  checkForUpdate: () => call("CheckForUpdate"),
  members: (guildID) => call("Members", guildID),
  removeMember: (guildID, fingerprint) => call("RemoveMember", guildID, fingerprint),
  setNickname: (guildID, nick) => call("SetNickname", guildID, nick),
  roles: (guildID) => call("Roles", guildID),
  upsertRole: (guildID, roleID, name, color, perms, position) =>
    call("UpsertRole", guildID, roleID, name, color, perms, position),
  deleteRole: (guildID, roleID) => call("DeleteRole", guildID, roleID),
  assignRole: (guildID, fingerprint, roleID, add) =>
    call("AssignRole", guildID, fingerprint, roleID, add),
  banMember: (guildID, fingerprint) => call("BanMember", guildID, fingerprint),
  unbanMember: (guildID, fingerprint) => call("UnbanMember", guildID, fingerprint),
  muteMember: (guildID, fingerprint, minutes) => call("MuteMember", guildID, fingerprint, minutes),
  unmuteMember: (guildID, fingerprint) => call("UnmuteMember", guildID, fingerprint),
  bans: (guildID) => call("Bans", guildID),
  contacts: () => call("Contacts"),
  joinVoice: (channelID) => call("JoinVoice", channelID),
  leaveVoice: (channelID) => call("LeaveVoice", channelID),
  relaySignal: (toPeerID, data) => call("RelaySignal", toPeerID, data),
  sendTyping: (channelID) => call("SendTyping", channelID),
  setProfile: (name, status, emoji, color, avatar, presence = "", bio = "") =>
    call("SetProfile", name, status, emoji, color, avatar, presence, bio),
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
// The desktop app streams events over the Wails runtime (window.runtime, which
// is injected even when the Go method bindings aren't); the browser build uses
// SSE. We probe window.runtime directly rather than via isWails() because the
// desktop build ships without the window.go.* bindings (RPC goes over HTTP).
export function on(event, handler) {
  const rt = typeof window !== "undefined" ? window.runtime : null;
  if (rt && typeof rt.EventsOn === "function") return rt.EventsOn(event, handler);
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
