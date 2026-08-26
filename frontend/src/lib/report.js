// The evidence file behind Report → Export.
//
// Concord has no server, so a report cannot be filed anywhere; what it can do
// is write down what happened in a form that still means something to whoever
// reads it next — a guild's admins, an employer, a lawyer, the police. That
// makes the file the whole feature, and it has to be exact: the wrong
// timestamp or a display name in place of a key is the difference between a
// record and an anecdote.
//
// Kept out of the Svelte component so it can be tested, and so its shape is
// pinned by something other than a screenshot.

export const REPORT_VERSION = 1;

// A display name is self-asserted — anyone can call themselves anything — so
// it is recorded as a claim alongside the fingerprint, never instead of it.
// The fingerprint is the safety number: derived from the sender's keys, the
// only identifier in Concord that a person cannot simply choose.
export function buildReport({ message, reporter = "", guildId = "", guildName = "", now = new Date() }) {
  if (!message?.id) throw new Error("report: no message");
  return {
    concordReport: REPORT_VERSION,
    exportedAt: now.toISOString(),
    note:
      "Written locally by Concord at the reporting user's request. Concord has no server and no moderation service; " +
      "nothing in this file was sent anywhere, and no copy of it exists outside this device.",
    reportedBy: reporter || "",
    guildId: guildId || "",
    guildName: guildName || "",
    channelId: message.channelId || "",
    message: {
      id: message.id,
      sentAt: message.sent || "",
      edited: !!message.edited,
      senderFingerprint: message.sender || "",
      senderDisplayName: message.senderName || "",
      content: message.content ?? "",
    },
  };
}

// Colons are legal in a filename on Linux and not on Windows or Android, and
// this file is meant to be moved between machines — so the ISO stamp is
// flattened rather than trusted.
export function reportFilename(now = new Date()) {
  return `concord-report-${now.toISOString().replace(/[:.]/g, "-")}.json`;
}
