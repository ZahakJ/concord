// scheduled.svelte.js — client-side scheduled messages + message reminders.
// Both are purely local (this device): persisted in localStorage and processed
// by a single ticker. Scheduled messages fire even after a restart (anything
// past-due is sent on next launch); reminders raise a notification + toast.
// Nothing here touches the backend — a scheduled send is just a normal
// SendMessage issued later.
import { api } from "./api.js";
import { S, flash, jumpToChannel, clockOpts } from "./state.svelte.js";

const SKEY = "concord.scheduled";
const RKEY = "concord.reminders";

// { id, channelId, text, replyTo, at }  — at = epoch ms
export let scheduled = $state(load(SKEY));
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
    localStorage.setItem(SKEY, JSON.stringify(scheduled));
    localStorage.setItem(RKEY, JSON.stringify(reminders));
  } catch {
    /* storage blocked — in-memory only for this session */
  }
}

const uid = () => (crypto?.randomUUID?.() ?? String(Date.now()) + Math.random());

export function scheduleMessage(channelId, text, replyTo, at) {
  scheduled.push({ id: uid(), channelId, text, replyTo: replyTo || "", at });
  scheduled.sort((a, b) => a.at - b.at);
  persist();
}
export function cancelScheduled(id) {
  const i = scheduled.findIndex((s) => s.id === id);
  if (i >= 0) {
    scheduled.splice(i, 1);
    persist();
  }
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

function channelExists(channelId) {
  return S.guilds.some((g) => g.channels?.some((c) => c.id === channelId));
}

async function fireDueScheduled(now) {
  const due = scheduled.filter((s) => s.at <= now);
  for (const s of due) {
    cancelScheduled(s.id);
    if (!channelExists(s.channelId)) {
      flash("A scheduled message was dropped — its channel is gone", "error");
      continue;
    }
    try {
      await api.sendMessage(s.channelId, s.text, s.replyTo);
    } catch {
      // Re-queue a minute out so a transient failure doesn't lose the message.
      scheduleMessage(s.channelId, s.text, s.replyTo, Date.now() + 60000);
    }
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
  const tick = () => {
    const now = Date.now();
    fireDueScheduled(now);
    fireDueReminders(now);
  };
  tick(); // catch anything past-due from a previous session
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
