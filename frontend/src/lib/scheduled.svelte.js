// scheduled.svelte.js — scheduled messages + message reminders.
// The two halves live in different places on purpose. A scheduled SEND is held
// by the Go service (scheduled_sends table, swept by a core loop): closing this
// window no longer kills it — it fires as long as this device's Concord is
// running. `scheduled` here is just a mirror of that queue for the UI.
// REMINDERS stay purely local (localStorage + the ticker below): a reminder is
// a notification for the eyes reading this window, so a backend copy firing
// with no window open would alert no one.
import { api } from "./api.js";
import { S, flash, jumpToChannel, clockOpts } from "./state.svelte.js";

const SKEY = "concord.scheduled"; // legacy queue — drained into the backend once
const RKEY = "concord.reminders";

// { id, channelId, text, at } — at = epoch ms. Mirror of the backend queue.
export let scheduled = $state([]);
// { id, channelId, messageId, preview, at }
export let reminders = $state(load(RKEY));

function load(key) {
  try {
    const v = JSON.parse(localStorage.getItem(key) || "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}
function persist() {
  try {
    localStorage.setItem(RKEY, JSON.stringify(reminders));
  } catch {
    /* storage blocked — in-memory only for this session */
  }
}

const uid = () => (crypto?.randomUUID?.() ?? String(Date.now()) + Math.random());

// refreshScheduled re-mirrors the backend queue. Exported states can't be
// reassigned, so the array is replaced in place. Quiet on failure (not logged
// in yet, backend restarting) — the stale mirror is better than an error.
export async function refreshScheduled() {
  try {
    const list = (await api.scheduledSends()) || [];
    scheduled.splice(
      0,
      scheduled.length,
      ...list.map((s) => ({ id: s.id, channelId: s.channelId, text: s.content, at: s.fireAt * 1000 })),
    );
  } catch {
    /* keep the current mirror */
  }
}

export async function scheduleMessage(channelId, text, replyTo, at) {
  try {
    // The backend speaks unix seconds; everything JS-side is epoch ms.
    await api.scheduleSend(channelId, text, replyTo || "", Math.round(at / 1000));
    await refreshScheduled();
  } catch {
    flash("Couldn't schedule the message", "error");
  }
}
export async function cancelScheduled(id) {
  // Optimistic: drop from the mirror now, tell the backend, resync either way.
  const i = scheduled.findIndex((s) => s.id === id);
  if (i >= 0) scheduled.splice(i, 1);
  try {
    await api.cancelScheduledSend(id);
  } catch {
    /* resync below puts it back if the cancel didn't land */
  }
  await refreshScheduled();
}
export function addReminder(channelId, messageId, preview, at) {
  reminders.push({ id: uid(), channelId, messageId, preview: (preview || "").slice(0, 140), at });
  reminders.sort((a, b) => a.at - b.at);
  persist();
}
export function cancelReminder(id) {
  const i = reminders.findIndex((r) => r.id === id);
  if (i >= 0) {
    reminders.splice(i, 1);
    persist();
  }
}

// migrateLocalScheduled drains the pre-backend localStorage queue into the Go
// service, once. Entries the backend refuses (channel gone, not logged in yet)
// stay in localStorage for the next launch to retry, so an upgrade can't drop
// someone's queued message.
async function migrateLocalScheduled() {
  const legacy = load(SKEY);
  if (legacy.length === 0) return;
  const kept = [];
  for (const s of legacy) {
    try {
      await api.scheduleSend(s.channelId, s.text, s.replyTo || "", Math.round(s.at / 1000));
    } catch {
      kept.push(s);
    }
  }
  try {
    if (kept.length === 0) localStorage.removeItem(SKEY);
    else localStorage.setItem(SKEY, JSON.stringify(kept));
  } catch {
    /* storage blocked — nothing to clean up */
  }
}

// localNotify — the one OS-notification door for locally-decided alerts
// (reminders here, the event radar in lib/radar.svelte.js). Only speaks when
// the page is hidden: a visible app already showed its own toast/banner, and
// doubling both is the fastest way to teach people to revoke the permission.
// Degrades silently when Notification is absent or denied.
export function localNotify(title, body, channelId, tag) {
  try {
    if (typeof Notification !== "undefined" && Notification.permission === "granted" && document.hidden) {
      const n = new Notification(title, { body, tag });
      n.onclick = () => {
        window.focus();
        if (channelId) jumpToChannel(channelId);
        n.close();
      };
    }
  } catch {
    /* no notifications available — the in-app surface still fired */
  }
}

function fireDueReminders(now) {
  const due = reminders.filter((r) => r.at <= now);
  for (const r of due) {
    cancelReminder(r.id);
    flash(`⏰ Reminder: ${r.preview || "a message"}`, "info");
    localNotify("⏰ Reminder", r.preview || "You asked to be reminded", r.channelId, r.id);
  }
}

let timer;
export function startScheduler() {
  migrateLocalScheduled().then(refreshScheduled);
  const tick = () => {
    fireDueReminders(Date.now());
    // Re-mirror the backend queue so fired sends fall off the manager list.
    refreshScheduled();
  };
  clearInterval(timer);
  timer = setInterval(tick, 15000);
  return () => clearInterval(timer);
}

// Quick "when" presets, recomputed each open so "this evening" is always right.
export function whenPresets() {
  const now = new Date();
  const at = (d) => d.getTime();
  const plus = (min) => new Date(now.getTime() + min * 60000);
  const evening = new Date(now);
  evening.setHours(18, 0, 0, 0);
  if (evening <= now) evening.setDate(evening.getDate() + 1);
  const tomorrow = new Date(now);
  tomorrow.setDate(tomorrow.getDate() + 1);
  tomorrow.setHours(9, 0, 0, 0);
  return [
    { label: "In 30 minutes", at: at(plus(30)) },
    { label: "In 1 hour", at: at(plus(60)) },
    { label: "In 3 hours", at: at(plus(180)) },
    { label: "This evening", at: at(evening) },
    { label: "Tomorrow morning", at: at(tomorrow) },
  ];
}

// Human label for a future timestamp, e.g. "in 2h", "Tomorrow 9:00 AM".
export function whenLabel(at) {
  const d = new Date(at);
  const mins = Math.round((at - Date.now()) / 60000);
  if (mins < 1) return "now";
  if (mins < 60) return `in ${mins}m`;
  const sameDay = d.toDateString() === new Date().toDateString();
  // 12/24h follows the user's clock preference, like every other timestamp.
  const time = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...clockOpts() });
  if (sameDay) return `today ${time}`;
  const tomorrow = new Date();
  tomorrow.setDate(tomorrow.getDate() + 1);
  if (d.toDateString() === tomorrow.toDateString()) return `tomorrow ${time}`;
  return d.toLocaleDateString([], { month: "short", day: "numeric" }) + ` ${time}`;
}
