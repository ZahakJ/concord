<script>
  // The guild calendar: agenda list + month grid (toggle), day drill-down,
  // and the create/edit form. The agenda leads on phones — a month grid is a
  // toggle there, never the landing view, because 393px turns grids into
  // squint tests. Desktop lands on the grid. Editorial throughout: typography
  // and hairlines carry the hierarchy, color is reserved for state.
  // The same panel serves DMs (shared with the people in the chat) and the
  // Notes self-DM (private to you) — see the isDM/isNotes derivations below.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmptyState from "../EmptyState.svelte";
  import Select from "../Select.svelte";
  import Switch from "../Switch.svelte";
  import EventCard from "../EventCard.svelte";
  import { fly, slide } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { S, flash } from "../lib/state.svelte.js";
  import { api, on } from "../lib/api.js";
  import {
    EV,
    loadEvents,
    dayKey,
    fmtDayHeading,
    happeningNow,
    downloadICS,
    icsName,
  } from "../lib/events.svelte.js";
  import { REPEATS, expand, repeatSentence } from "../lib/recurrence.js";
  import { encodeEventToken } from "../lib/eventtoken.js";

  let { onClose, onJoinVoice } = $props();

  // Frozen at open: the panel is about the guild it was opened from, even if
  // a background refresh changes the active guild id.
  const gid = S.modal?.guildId || S.activeGuildId;
  const g = $derived(S.guilds.find((x) => x.id === gid) || null);
  const records = $derived(EV.byGuild[gid] || []);

  // Every reading surface below works on OCCURRENCES, not records: a series is
  // one record and many days, and the agenda, the grid and the day drill-down
  // all want the days. The window is generous but finite — six months back for
  // the "past events" drawer, eighteen forward — because an endless series has
  // to be bounded by whoever expands it.
  const WINDOW_BACK_MS = 183 * 86400000;
  const WINDOW_FWD_MS = 550 * 86400000;
  const events = $derived(
    expand(records, Date.now() - WINDOW_BACK_MS, Date.now() + WINDOW_FWD_MS),
  );

  // DMs get calendars too — same records, same sync lane, different manners.
  // A DM has exactly ONE channel and it doubles as the call (ChatHeader's
  // "Call" button), so a DM event's natural place is the conversation itself:
  // no channel picker, no throwaway guest guild — Join lands in the DM's own
  // call. Notes is the degenerate case: a single-member DM (just you), so its
  // events are private reminders — no call, no guests, only words.
  const isDM = $derived(g?.kind === "dm");
  const isNotes = $derived(!!g?.dmNotes);
  const dmChannel = $derived(isDM ? g?.channels?.[0] || null : null);
  // "you both" for a 1:1, "everyone" once it's a group DM.
  const dmEveryone = $derived((g?.dmMembers ?? 2) > 2 ? "everyone" : "you both");

  let view = $state(S.isMobile ? "agenda" : "grid");
  let selectedDay = $state(""); // a dayKey, or "" = everything

  // The empty panel's copy, as data. It used to be four branches of markup
  // inside the illustration, which is why this panel could not use the shared
  // EmptyState — the component takes a headline and a sub, and the branching
  // was tangled up with the badge. Lifting it out is what let the illustration
  // become the house one.
  const emptyHead = $derived(
    selectedDay
      ? `Nothing on ${fmtDayHeading(selectedDay)}`
      : isNotes
        ? "Nothing planned — just yours"
        : isDM
          ? `Nothing planned ${(g?.dmMembers ?? 2) > 2 ? "here" : `with ${g?.name || "them"}`} yet`
          : "Nothing on the calendar yet",
  );
  const emptyBody = $derived(
    selectedDay
      ? "A free day. Suspiciously free. Fix that?"
      : isNotes
        ? 'Dentist, deadline, "call mom" — events here are private to you, synced to your own devices, and show up in Your calendar tagged Private.'
        : isDM
          ? `"When are we hopping on?" — put a time on it. Everyone in this chat sees it, and Join drops ${dmEveryone} straight into the call.`
          : "Game night, standup, the big launch — whatever this crew does together, give it a time and everyone can RSVP right here.",
  );
  let editing = $state(null); // null | draft {id?, title, details, location, start, durMin}
  let showPast = $state(false);

  const reduceMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

  // Load on open; recheck on every guild-updated — that's the signal the core
  // emits for event upserts/removals/RSVPs (local and gossiped alike).
  $effect(() => {
    loadEvents(gid);
    return on("guild-updated", () => loadEvents(gid));
  });

  const title = $derived(
    editing
      ? (editing.id ? "Edit event" : "New event")
      : isNotes
        ? "Private events"
        : `${g?.name || "Guild"} · Events`,
  );

  // ---- month grid ----
  let month = $state(new Date(new Date().getFullYear(), new Date().getMonth(), 1));
  let pageDir = $state(1); // which way the fresh month slides in from
  const monthName = $derived(month.toLocaleDateString([], { month: "long" }));
  const monthYear = $derived(month.getFullYear());
  const monthLabel = $derived(month.toLocaleDateString([], { month: "long", year: "numeric" }));
  // Monday-start; 2024-01-01 is a Monday, so it mints locale day initials.
  const dayNames = [...Array(7)].map((_, i) =>
    new Date(2024, 0, 1 + i).toLocaleDateString([], { weekday: "narrow" }),
  );
  const cells = $derived.by(() => {
    const lead = (month.getDay() + 6) % 7;
    const start = new Date(month);
    start.setDate(1 - lead);
    const counts = {};
    const liveDays = {};
    const titles = {};
    for (const ev of events) {
      const k = dayKey(ev.startUnix);
      counts[k] = (counts[k] || 0) + 1;
      if (happeningNow(ev)) liveDays[k] = true;
      // The grid spent half the modal to mark a day with a three-pixel dot,
      // and the event's name lived only in the list underneath. Two chips is
      // what a 40px cell can hold honestly; past that the count says the rest.
      (titles[k] ||= []).push(ev.title);
    }
    const today = new Date().toDateString();
    const out = [];
    // Always 6 rows: the grid keeps one height as months page by, so the ‹ ›
    // buttons don't jump under a paging thumb.
    for (let i = 0; i < 42; i++) {
      const d = new Date(start);
      d.setDate(start.getDate() + i);
      const key = d.toDateString();
      out.push({
        key,
        n: d.getDate(),
        out: d.getMonth() !== month.getMonth(),
        today: key === today,
        live: !!liveDays[key],
        count: counts[key] || 0,
        titles: titles[key] || [],
      });
    }
    return out;
  });
  function pageMonth(dir) {
    pageDir = dir;
    month = new Date(month.getFullYear(), month.getMonth() + dir, 1);
  }
  function goToday() {
    const now = new Date();
    pageDir = now > month ? 1 : -1;
    month = new Date(now.getFullYear(), now.getMonth(), 1);
    selectedDay = now.toDateString();
  }
  function pickDay(key) {
    selectedDay = selectedDay === key ? "" : key; // tap again to widen back out
  }

  // Swipe to page on coarse pointers. Claim the gesture only when it is
  // decisively horizontal (|dx| > |dy|×1.5) so vertical scroll and the
  // sheet's drag-dismiss keep working.
  let swipe = null;
  function swipeStart(e) {
    if (e.pointerType !== "touch") return;
    swipe = { x: e.clientX, y: e.clientY };
  }
  function swipeEnd(e) {
    if (!swipe) return;
    const dx = e.clientX - swipe.x;
    const dy = e.clientY - swipe.y;
    swipe = null;
    if (Math.abs(dx) > 48 && Math.abs(dx) > Math.abs(dy) * 1.5) pageMonth(dx < 0 ? 1 : -1);
  }

  // ---- agenda ----
  const startOfToday = () => new Date(new Date().toDateString()).getTime() / 1000;
  const groupsOf = (list) => {
    const out = [];
    for (const ev of list) {
      const k = dayKey(ev.startUnix);
      if (!out.length || out[out.length - 1].key !== k) out.push({ key: k, events: [] });
      out[out.length - 1].events.push(ev);
    }
    // A flat running offset per group, so the entrance stagger can count
    // across group boundaries (first 8 entries only).
    let off = 0;
    for (const grp of out) {
      grp.offset = off;
      off += grp.events.length;
    }
    return out;
  };
  const visible = $derived(selectedDay ? events.filter((e) => dayKey(e.startUnix) === selectedDay) : events);
  const pastEvents = $derived(visible.filter((e) => (e.endUnix || e.startUnix + 3600) < startOfToday()));
  const groups = $derived(groupsOf(visible.filter((e) => (e.endUnix || e.startUnix + 3600) >= startOfToday())));

  // ---- create / edit ----
  // datetime-local wants a local-time string; same tz dance as ModalWhen.
  function toLocalInput(ms) {
    return new Date(ms - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  }
  const toLocalDate = (ms) => toLocalInput(ms).slice(0, 10);

  // The zone the times on this form are IN. The dialog showed local times with
  // nothing naming them, which is why the seeded guild's messages resort to
  // typing "19:00 UTC" into chat by hand. Every member reads the event in
  // THEIR own zone — the record is UTC seconds — so the honest line names the
  // zone the AUTHOR is entering it in.
  const tzName = (() => {
    try {
      const z = Intl.DateTimeFormat().resolvedOptions().timeZone;
      const abbr = new Intl.DateTimeFormat([], { timeZoneName: "short" })
        .formatToParts(new Date())
        .find((p) => p.type === "timeZoneName")?.value;
      return abbr ? `${z} (${abbr})` : z;
    } catch {
      return "";
    }
  })();

  // Channels a card can be posted INTO: text-shaped only, because a voice room
  // has a chat but nobody reads it, and a forum board takes posts rather than
  // messages.
  const announceChannels = $derived(
    (g?.channels || []).filter(
      (c) => !c.parent && (c.type === "" || c.type === "text" || c.type === "announcement"),
    ),
  );

  // Channels this guild can host an event IN: the location picker's menu.
  // Voice rooms first (they're what a meeting usually means), then the text-
  // shaped kinds; never forum posts (threads have a parent) and never in a DM
  // or meeting room — there the location stays plain words, gracefully.
  const locChannels = $derived.by(() => {
    if (!g || g.kind === "dm" || g.kind === "meeting") return [];
    const ok = (c) =>
      !c.parent && (c.type === "voice" || c.type === "" || c.type === "text" || c.type === "announcement");
    const list = (g.channels || []).filter(ok);
    return [...list.filter((c) => c.type === "voice"), ...list.filter((c) => c.type !== "voice")];
  });
  const locVoice = $derived(locChannels.filter((c) => c.type === "voice"));
  const locText = $derived(locChannels.filter((c) => c.type !== "voice"));
  // The picked channel's display label — doubles as the free-text Location so
  // ICS exports and not-yet-synced peers still see "🔊 lounge" instead of air.
  const locLabel = (c) => (c.type === "voice" ? `🔊 ${c.name}` : `# ${c.name}`);

  function blankDraft() {
    // A tapped day pre-fills its evening; otherwise an hour from now, like
    // ModalWhen's custom default.
    let at;
    if (selectedDay && new Date(selectedDay).getTime() > Date.now()) {
      const d = new Date(selectedDay);
      d.setHours(18, 0, 0, 0);
      at = d.getTime();
    } else {
      at = Date.now() + 3600000;
    }
    // mode's real default depends on locChannels — startCreate/startEdit set
    // it; "remote" here is the safe floor (no channels → no dead toggle).
    return {
      id: "",
      title: "",
      details: "",
      location: "",
      locationChannelId: "",
      mode: "remote",
      start: toLocalInput(at),
      durMin: 60,
      guests: false,
      autoAdmit: false,
      repeat: "",
      repeatUntil: "", // a date input string, "" = no end
      announceIn: "",  // channel to post the card into, "" = don't announce
    };
  }
  // The OS picker is the fallback, never the greeting: chips first, the
  // datetime-local appears once a time has been touched (or on request).
  let showPicker = $state(false);
  let durMore = $state(false);
  // Once a human has touched the room switch, mode flips stop second-guessing it.
  let guestsTouched = $state(false);
  function startCreate() {
    editing = blankDraft();
    if (isNotes) {
      // Notes is a party of one: no call to join, no guests to invite. The
      // draft is pure words-and-a-time — a reminder only you will ever see.
      editing.mode = "remote";
      editing.guests = false;
    } else if (isDM) {
      // A DM event defaults to the DM itself: the single channel IS the call,
      // so Join drops everyone straight in — no throwaway room minted.
      editing.mode = "local";
      editing.locationChannelId = dmChannel?.id || "";
      editing.guests = false;
    } else {
      // Where is an either/or: LOCAL (a channel of this guild) when the guild
      // has channels, REMOTE (free text + the guests' door) otherwise. Local
      // pre-selects the first voice channel — the common meeting case is zero
      // extra taps and the mode is valid out of the gate.
      editing.mode = locChannels.length ? "local" : "remote";
      if (editing.mode === "local" && locVoice.length) editing.locationChannelId = locVoice[0].id;
      else if (editing.mode === "local" && locChannels.length) editing.locationChannelId = locChannels[0].id;
      // Remote IS the guest stuff → room armed by default. Except in a meeting
      // room, which already has its own guest link — the core refuses a second.
      editing.guests = editing.mode === "remote" && g?.kind !== "meeting";
    }
    // Where to announce it, defaulted from where it IS. Scheduling an event
    // used to notify nobody: the record replicated, so it appeared in every
    // member's calendar — but only if they opened the calendar. The dialog
    // already knows a channel, so it can post the card into one.
    editing.announceIn =
      announceChannels.find((c) => c.id === editing.locationChannelId)?.id ||
      announceChannels.find((c) => c.id === S.activeChannelId)?.id ||
      announceChannels[0]?.id ||
      "";
    guestsTouched = false;
    showPicker = false;
    durMore = false;
  }
  function startEdit(ev) {
    editing = {
      id: ev.id,
      title: ev.title,
      details: ev.details || "",
      location: ev.location || "",
      // A channel that has since been deleted falls back to the free-text
      // label the form saved alongside it — the picker shows "somewhere
      // else…" and the words survive. In a DM the only valid channel is the
      // DM's own (locChannels is empty there by design).
      locationChannelId: (isDM
        ? !!ev.locationChannelId && ev.locationChannelId === dmChannel?.id
        : locChannels.some((c) => c.id === ev.locationChannelId))
        ? ev.locationChannelId
        : "",
      start: toLocalInput(ev.startUnix * 1000),
      durMin: ev.endUnix ? Math.round((ev.endUnix - ev.startUnix) / 60) : 0,
      guests: false,
      autoAdmit: false,
      repeat: ev.repeat || "",
      repeatUntil: ev.repeatUntil ? toLocalDate(ev.repeatUntil * 1000) : "",
      // Editing never re-announces: the card in the channel already points at
      // this record and follows it.
      announceIn: "",
    };
    // Mode falls out of the same deleted-channel guard above: a live channel
    // id → Local; anything else (free text, or a channel since deleted) →
    // Remote with the saved words intact. No pre-select on edit.
    editing.mode = editing.locationChannelId ? "local" : "remote";
    guestsTouched = false;
    // Editing an existing time: show the precise picker straight away, and
    // unfold the full duration list when the value isn't one of the quick three.
    showPicker = true;
    durMore = ![30, 60, 120].includes(editing.durMin);
  }
  // Local↔Remote is non-destructive: a round-trip loses nothing. The one
  // opinion: the FIRST flip to remote on a fresh draft arms the room (remote
  // is the guest stuff), unless the human already touched the switch.
  function setMode(m) {
    if (editing.mode === m) return;
    editing.mode = m;
    // In a DM, Local means THIS conversation — restore its channel on the way
    // back so the draft is valid again without any picker.
    if (m === "local" && isDM) editing.locationChannelId = dmChannel?.id || "";
    // …and don't auto-arm the guest room there: "somewhere else" in a DM is
    // usually a real-world spot between people who already share the chat.
    if (m === "remote" && !editingHasGuests && !guestsTouched && !editing.id && g?.kind !== "meeting" && !isDM)
      editing.guests = true;
  }
  // ---- keyboard: native radio idiom (arrows move AND select) ----
  function focusChecked(group) {
    queueMicrotask(() => group?.querySelector('[aria-checked="true"]')?.focus());
  }
  function modeKeys(e) {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) return;
    e.preventDefault();
    setMode(editing.mode === "local" ? "remote" : "local");
    focusChecked(e.currentTarget.closest(".where-seg"));
  }
  function chanTab(c) {
    if (editing.locationChannelId) return editing.locationChannelId === c.id ? 0 : -1;
    return locChannels[0]?.id === c.id ? 0 : -1; // no pick yet: first row is the stop
  }
  function chanKeys(e) {
    // One radiogroup spanning voice+text; the kickers are presentational.
    const list = [...locVoice, ...locText];
    if (!list.length) return;
    const idx = Math.max(0, list.findIndex((c) => c.id === editing.locationChannelId));
    let next = -1;
    if (e.key === "ArrowDown" || e.key === "ArrowRight") next = Math.min(idx + 1, list.length - 1);
    else if (e.key === "ArrowUp" || e.key === "ArrowLeft") next = Math.max(idx - 1, 0);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = list.length - 1;
    else if (e.key.length === 1 && /\S/.test(e.key)) {
      // Type-ahead: jump to the next channel whose name starts with the key.
      const q = e.key.toLowerCase();
      const wrapped = [...list.slice(idx + 1), ...list.slice(0, idx + 1)];
      const hit = wrapped.find((c) => c.name.toLowerCase().startsWith(q));
      if (hit) next = list.indexOf(hit);
    }
    if (next < 0) return;
    e.preventDefault();
    editing.locationChannelId = list[next].id;
    focusChecked(e.currentTarget);
  }
  function doorKeys(e) {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) return;
    e.preventDefault();
    editing.autoAdmit = !editing.autoAdmit;
    focusChecked(e.currentTarget);
  }
  // Whether the event being edited already has a live guest link — the form
  // then points at the card (copy/revoke live there) instead of offering to
  // mint a second one.
  const editingHasGuests = $derived(!!editing?.id && !!events.find((e) => e.id === editing.id)?.guestUrl);
  const DURATIONS = [
    { min: 0, label: "No end time" },
    { min: 30, label: "30 minutes" },
    { min: 60, label: "1 hour" },
    { min: 90, label: "1½ hours" },
    { min: 120, label: "2 hours" },
    { min: 180, label: "3 hours" },
    { min: 240, label: "4 hours" },
    { min: 480, label: "All day-ish (8h)" },
  ];
  const QUICK_DUR = [
    { min: 30, label: "30 min" },
    { min: 60, label: "1 h" },
    { min: 120, label: "2 h" },
  ];
  const draftAt = $derived(editing?.start ? new Date(editing.start).getTime() : NaN);
  // Local REQUIRES a channel — belt-and-suspenders; the pre-select keeps it
  // satisfied in practice.
  const draftValid = $derived(
    !!editing?.title.trim() && !isNaN(draftAt) && (editing.mode !== "local" || !!editing.locationChannelId),
  );

  // The echo line: the form reads back what it heard, kicker-voiced, so a
  // disabled Create button always explains itself.
  const echo = $derived.by(() => {
    if (!editing) return "";
    if (isNaN(draftAt)) return "Pick a time";
    const d = new Date(draftAt);
    const day = d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" });
    const t = (x) => x.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    // A disabled Create button always explains itself — unreachable in
    // practice (local pre-selects a channel), cheap insurance regardless.
    const tail = editing.mode === "local" && !editing.locationChannelId ? " · pick a channel" : "";
    if (editing.durMin) return `${day} · ${t(d)} – ${t(new Date(draftAt + editing.durMin * 60000))}${tail}`;
    return `${day} · ${t(d)}${tail}`;
  });

  // "When" chips — the bookpage slot DNA: obvious near-future times as pills,
  // the OS picker demoted to "Pick a time…".
  const whenChips = $derived.by(() => {
    if (!editing || editing.id) return [];
    const out = [];
    const seen = new Set();
    const push = (label, ms) => {
      if (ms > Date.now() && !seen.has(ms)) {
        seen.add(ms);
        out.push({ label, ms });
      }
    };
    const at = (base, h) => {
      const d = new Date(base);
      d.setHours(h, 0, 0, 0);
      return d.getTime();
    };
    const now = new Date();
    if (selectedDay) {
      const t = at(new Date(selectedDay), 18);
      const lbl = new Date(t).toLocaleDateString([], { weekday: "short" });
      push(`That evening (${lbl} 6 PM)`, t);
    }
    push("Tonight 7 PM", at(now, 19));
    const tom = new Date(now);
    tom.setDate(tom.getDate() + 1);
    push("Tomorrow 9 AM", at(tom, 9));
    push("Tomorrow 7 PM", at(tom, 19));
    const mon = new Date(now);
    mon.setDate(mon.getDate() + (((8 - mon.getDay()) % 7) || 7));
    push("Next Mon 9 AM", at(mon, 9));
    return out.slice(0, 4);
  });
  const pickedMs = $derived(isNaN(draftAt) ? 0 : draftAt);
  // "Every week" is honest but vague; "Every Tuesday" is the thing an
  // organizer is actually setting up, and the weekday falls straight out of
  // the start time.
  const draftWeekday = $derived(
    isNaN(draftAt) ? "" : new Date(draftAt).toLocaleDateString([], { weekday: "long" }),
  );
  function pickChip(ms) {
    editing.start = toLocalInput(ms);
    showPicker = true; // fine-tuning is one glance away once a time exists
  }

  async function save() {
    if (!draftValid) return;
    const startUnix = Math.floor(draftAt / 1000);
    const endUnix = editing.durMin ? startUnix + editing.durMin * 60 : 0;
    const isEdit = !!editing.id;
    // A picked channel writes BOTH fields: the id (what Join and the
    // in-channel reminder run on) and its label as the free-text location
    // (what ICS export and stale channel lists still show). Mode is the
    // truth: a remote save ignores whatever channel the draft still carries.
    const locCh =
      editing.mode === "local"
        ? (isDM ? dmChannel : locChannels.find((c) => c.id === editing.locationChannelId)) || null
        : null;
    // The DM's channel label ("dm") means nothing on a card or in an .ics —
    // the human-true location is the conversation itself.
    const locStr = locCh ? (isDM ? `📞 ${g?.name || "this chat"}` : locLabel(locCh)) : editing.location.trim();
    const locChId = locCh ? locCh.id : "";
    // Recurrence: a rule and an optional end, both on the one record. The
    // occurrences are expanded by whoever reads the calendar (lib/recurrence),
    // never stored, so "edit the meeting" stays one edit.
    const repeat = editing.repeat || "";
    const repeatUntil =
      repeat && editing.repeatUntil
        ? Math.floor(new Date(`${editing.repeatUntil}T23:59:59`).getTime() / 1000)
        : 0;
    let saved;
    try {
      if (isEdit)
        saved = await api.updateEvent(gid, editing.id, editing.title.trim(), editing.details.trim(), startUnix, endUnix, locStr, locChId, repeat, repeatUntil);
      else
        saved = await api.createEvent(gid, editing.title.trim(), editing.details.trim(), startUnix, endUnix, locStr, locChId, repeat, repeatUntil);
    } catch (err) {
      flash(err); // e.g. not allowed to edit someone else's — never fail silently
      return;
    }
    // The announcement. One ordinary message carrying one token, so it syncs,
    // pins, searches and deletes like anything else, and the card it draws
    // reads the LIVE record rather than a snapshot. Best effort: the event
    // exists either way, and a failed post must not read as a failed save.
    let announced = false;
    const announceWhere = announceChannels.find((c) => c.id === editing.announceIn) || null;
    if (!isEdit && editing.announceIn) {
      try {
        await api.sendMessage(editing.announceIn, encodeEventToken({ id: saved.id, title: saved.title }));
        announced = true;
      } catch (err) {
        flash(err);
      }
    }
    // Guest access rides the same form: mint the link right after the event
    // lands. A failure here must not un-save the event — report it apart.
    if (editing.guests && !saved.guestUrl) {
      try {
        const opened = await api.openEventGuests(gid, saved.id, editing.autoAdmit);
        await navigator.clipboard?.writeText(opened.guestUrl);
        flash("Guest link copied — anyone with it can join from a browser", "success");
      } catch (err) {
        flash(err);
      }
    }
    editing = null;
    await loadEvents(gid);
    flash(
      isEdit
        ? "Event updated"
        : isNotes
          ? "Saved — only you can see this"
          : announced && announceWhere
            ? `Event created and posted in #${announceWhere.name}`
            : "Event created — everyone can RSVP now",
      "success",
    );
    // Land the list where the new thing is.
    view = "agenda";
    selectedDay = "";
  }

  async function exportAll() {
    try {
      downloadICS(`${icsName(g?.name || "guild")}-calendar.ics`, await api.eventsICS(gid));
      flash("Calendar exported — open it with your calendar app", "success");
    } catch (err) {
      flash(err);
    }
  }
</script>

<Modal {title} {onClose} wide>
  {#if editing}
    <div class="form">
      <!-- Title first: the event is a sentence, not a database row. -->
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="title-in"
        autofocus={!S.isMobile}
        placeholder="What's happening?"
        maxlength="120"
        bind:value={editing.title}
      />
      <!-- The form reads back what it heard. -->
      <div class="kicker echo" class:unset={isNaN(draftAt) || (editing.mode === "local" && !editing.locationChannelId)}>{echo}</div>
      {#if whenChips.length}
        <div class="chips" role="group" aria-label="When">
          {#each whenChips as c (c.ms)}
            <button class="slotchip" class:on={pickedMs === c.ms} onclick={() => pickChip(c.ms)}>
              {c.label}
            </button>
          {/each}
          {#if !showPicker}
            <button class="slotchip more" onclick={() => (showPicker = true)}>Pick a time…</button>
          {/if}
        </div>
      {/if}
      <div class="row2">
        {#if showPicker || !whenChips.length}
          <label class="fld startfld">
            <span class="muted tiny">Starts</span>
            <input type="datetime-local" bind:value={editing.start} />
          </label>
        {/if}
        <div class="fld">
          <span class="muted tiny">Lasts</span>
          {#if durMore}
            <Select
              label="Lasts"
              value={editing.durMin}
              onPick={(v) => (editing.durMin = v)}
              options={DURATIONS.map((d) => ({ value: d.min, label: d.label }))}
            />
          {:else}
            <div class="chips" role="group" aria-label="Duration">
              {#each QUICK_DUR as d (d.min)}
                <button class="slotchip" class:on={editing.durMin === d.min} onclick={() => (editing.durMin = d.min)}>
                  {d.label}
                </button>
              {/each}
              <button class="slotchip more" onclick={() => (durMore = true)}>More…</button>
            </div>
          {/if}
        </div>
      </div>
      {#if tzName}
        <!-- The dialog showed local times with nothing naming them, which is
             why people end up typing "19:00 UTC" into chat by hand. Every
             member reads the event in their own zone; this names the one the
             times on THIS form are being entered in. -->
        <p class="tzline muted tiny">
          Times are in {tzName}. Everyone else sees this in their own zone.
        </p>
      {/if}

      <div class="fld">
        <span class="muted tiny">Repeats</span>
        <div class="chips" role="radiogroup" aria-label="Repeats">
          {#each REPEATS as r (r.id)}
            <button
              class="slotchip"
              class:on={editing.repeat === r.id}
              role="radio"
              aria-checked={editing.repeat === r.id}
              onclick={() => {
                editing.repeat = r.id;
                if (!r.id) editing.repeatUntil = "";
              }}
            >
              {r.id === "weekly" && draftWeekday ? `Every ${draftWeekday}` : r.label}
            </button>
          {/each}
        </div>
        {#if editing.repeat}
          <label class="untilrow">
            <span class="muted tiny">Ends</span>
            <input type="date" bind:value={editing.repeatUntil} min={editing.start.slice(0, 10)} />
            {#if editing.repeatUntil}
              <button class="slotchip more" onclick={() => (editing.repeatUntil = "")}>No end</button>
            {:else}
              <span class="muted tiny">Never — it keeps going until you change it.</span>
            {/if}
          </label>
        {/if}
      </div>

      <!-- WHERE is a decision, not a dropdown: LOCAL (a channel of this
           guild — Join walks people there, the guild reminds the room) or
           REMOTE (words + the guests' door into a sealed disposable room).
           Either/or; no channels in scope → Remote only, no dead toggle. -->
      <div class="fld where-fld">
        <span class="muted tiny" id="ev-where-lbl">Where</span>

        {#if locChannels.length}
          <div class="where-seg" role="radiogroup" aria-labelledby="ev-where-lbl">
            <button
              type="button"
              class="wseg"
              role="radio"
              aria-checked={editing.mode === "local"}
              tabindex={editing.mode === "local" ? 0 : -1}
              onclick={() => setMode("local")}
              onkeydown={modeKeys}
            >
              <strong class="wseg-t">Local</strong><span class="wseg-h">in a channel here</span>
            </button>
            <button
              type="button"
              class="wseg"
              role="radio"
              aria-checked={editing.mode === "remote"}
              tabindex={editing.mode === "remote" ? 0 : -1}
              onclick={() => setMode("remote")}
              onkeydown={modeKeys}
            >
              <strong class="wseg-t">Remote</strong><span class="wseg-h">guests &amp; elsewhere</span>
            </button>
          </div>
        {/if}

        <div class="wslot">
          {#if editing.mode === "local" && isDM && !isNotes}
            <!-- A DM event's default place: the conversation itself. No
                 segment, no picker — there is exactly one room here and it
                 already doubles as the call. "Somewhere else…" is the quiet
                 exit to free text (+ the outside-guest door). -->
            <div
              class="wpane"
              out:slide={{ duration: reduceMotion ? 0 : 140, easing: cubicOut }}
              in:slide={{ duration: reduceMotion ? 0 : 180, delay: reduceMotion ? 0 : 90, easing: cubicOut }}
            >
              <div class="wpane-in" in:fly={{ y: reduceMotion ? 0 : 6, duration: reduceMotion ? 0 : 160, delay: reduceMotion ? 0 : 140 }}>
                <div class="herecard">
                  <span class="here-badge"><Icon name="phone" size={14} /></span>
                  <span class="here-txt">
                    <strong>Right here — {g?.name || "this chat"}</strong>
                    <span class="here-sub">Join drops {dmEveryone} into this conversation's call when it's time.</span>
                  </span>
                </div>
                <button type="button" class="wlink" onclick={() => setMode("remote")}>Somewhere else…</button>
              </div>
            </div>
          {:else if editing.mode === "local" && locChannels.length}
            <div
              class="wpane"
              out:slide={{ duration: reduceMotion ? 0 : 140, easing: cubicOut }}
              in:slide={{ duration: reduceMotion ? 0 : 180, delay: reduceMotion ? 0 : 90, easing: cubicOut }}
            >
              <div class="wpane-in" in:fly={{ y: reduceMotion ? 0 : 6, duration: reduceMotion ? 0 : 160, delay: reduceMotion ? 0 : 140 }}>
                <div class="chpick" role="radiogroup" aria-label="Channel" onkeydown={chanKeys}>
                  {#if locVoice.length}<div class="chkick" aria-hidden="true">Voice</div>{/if}
                  {#each locVoice as c (c.id)}
                    <button
                      type="button"
                      class="chrow"
                      role="radio"
                      aria-checked={editing.locationChannelId === c.id}
                      tabindex={chanTab(c)}
                      aria-label={`${c.name}, voice channel`}
                      onclick={() => (editing.locationChannelId = c.id)}
                    >
                      <span class="chglyph">{#if editing.locationChannelId === c.id}<Icon name="check" size={14} />{:else}<Icon name="speaker" size={14} />{/if}</span>
                      <span class="chname">{c.name}</span>
                    </button>
                  {/each}
                  {#if locText.length}<div class="chkick" aria-hidden="true">Text</div>{/if}
                  {#each locText as c (c.id)}
                    <button
                      type="button"
                      class="chrow"
                      role="radio"
                      aria-checked={editing.locationChannelId === c.id}
                      tabindex={chanTab(c)}
                      aria-label={`${c.name}, text channel`}
                      onclick={() => (editing.locationChannelId = c.id)}
                    >
                      <span class="chglyph">{#if editing.locationChannelId === c.id}<Icon name="check" size={14} />{:else}<Icon name="hash" size={14} />{/if}</span>
                      <span class="chname">{c.name}</span>
                    </button>
                  {/each}
                </div>
                {#if editing.locationChannelId}
                  <div class="gnote muted" transition:slide={{ duration: reduceMotion ? 0 : 150, easing: cubicOut }}>
                    <Icon name="bell" size={12} />
                    Join takes members straight there{locVoice.some((c) => c.id === editing.locationChannelId)
                      ? " and into the call"
                      : ""} — and {g?.name || "the guild"} will post a reminder in that channel when it starts.
                  </div>
                {/if}
              </div>
            </div>
          {:else}
            <div
              class="wpane"
              out:slide={{ duration: reduceMotion ? 0 : 140, easing: cubicOut }}
              in:slide={{ duration: reduceMotion ? 0 : 180, delay: reduceMotion ? 0 : 90, easing: cubicOut }}
            >
              <div class="wpane-in" in:fly={{ y: reduceMotion ? 0 : 6, duration: reduceMotion ? 0 : 160, delay: reduceMotion ? 0 : 140 }}>
                <input placeholder={isNotes ? "Where? Only you will see it (optional)" : "Where? An address, a link-up spot, someone's couch…"} maxlength="160" bind:value={editing.location} />
                {#if isNotes}
                  <!-- A one-member DM: no call to point at, no guests to
                       admit. The words above are the whole story. -->
                {:else if editingHasGuests}
                  <div class="gnote muted">
                    <Icon name="link" size={12} /> This event already has a room — Join, copy or revoke on the event card.
                  </div>
                {:else}
                  <div class="roomcard" class:armed={editing.guests}>
                    <label class="room-top">
                      <span class="room-badge"><Icon name="link" size={14} /></span>
                      <span class="room-txt">
                        <strong>Meeting room</strong>
                        <span class="room-sub">Guests get a browser link into a separate, sealed, disposable room — this event's guild stays out of it. Members join in one tap.</span>
                      </span>
                      <Switch on={editing.guests}>
                        <input type="checkbox" bind:checked={editing.guests} onchange={() => (guestsTouched = true)} />
                      </Switch>
                    </label>
                    {#if editing.guests}
                      <div class="room-door" transition:slide={{ duration: reduceMotion ? 0 : 150, easing: cubicOut }}>
                        <span class="muted tiny">At the door</span>
                        <div class="seg" role="radiogroup" aria-label="At the door" onkeydown={doorKeys}>
                          <button
                            type="button"
                            class="seg-btn"
                            class:on={!editing.autoAdmit}
                            role="radio"
                            aria-checked={!editing.autoAdmit}
                            tabindex={!editing.autoAdmit ? 0 : -1}
                            onclick={() => (editing.autoAdmit = false)}
                          >They knock</button>
                          <button
                            type="button"
                            class="seg-btn"
                            class:on={editing.autoAdmit}
                            role="radio"
                            aria-checked={editing.autoAdmit}
                            tabindex={editing.autoAdmit ? 0 : -1}
                            onclick={() => (editing.autoAdmit = true)}
                          >Straight in</button>
                        </div>
                        <span class="room-sub">{editing.autoAdmit ? "Guests walk right into the room." : "Knocking means you admit each guest."}</span>
                      </div>
                    {/if}
                  </div>
                {/if}
                {#if isDM && !isNotes}
                  <button type="button" class="wlink" onclick={() => setMode("local")}>Actually — right here, in our call</button>
                {/if}
              </div>
            </div>
          {/if}
        </div>
      </div>
      <textarea rows="3" placeholder="Details (optional)" maxlength="2000" bind:value={editing.details}></textarea>
      {#if !editing.id && announceChannels.length}
        <div class="fld">
          <span class="muted tiny">Announce in</span>
          <Select
            label="Announce in"
            value={editing.announceIn}
            onPick={(v) => (editing.announceIn = v)}
            options={[
              { value: "", label: "Don't announce it" },
              ...announceChannels.map((c) => ({ value: c.id, label: `#${c.name}` })),
            ]}
          />
          <span class="muted tiny">
            {#if editing.announceIn}
              A card lands in that channel now, and it follows the event if you edit it.
            {:else}
              It will only appear in the calendar.
            {/if}
          </span>
        </div>
      {/if}
      <div class="actions">
        <button class="ghost" onclick={() => (editing = null)}>Cancel</button>
        <button class="primary" disabled={!draftValid} onclick={save}>
          {editing.id ? "Save changes" : "Create event"}
        </button>
      </div>
    </div>
  {:else}
    <div class="bar">
      <div class="seg" role="tablist" aria-label="Calendar view">
        <button class="seg-btn" class:on={view === "agenda"} role="tab" aria-selected={view === "agenda"} onclick={() => (view = "agenda")}>
          <Icon name="list" size={13} /> Agenda
        </button>
        <button class="seg-btn" class:on={view === "grid"} role="tab" aria-selected={view === "grid"} onclick={() => (view = "grid")}>
          <Icon name="calendar" size={13} /> Month
        </button>
      </div>
      <span class="spring"></span>
      {#if events.length}
        <button class="ghost export" title="Export the whole calendar as .ics" onclick={exportAll}>
          <Icon name="download" size={13} /> <span class="xl">.ics</span>
        </button>
      {/if}
      <button class="primary new" onclick={startCreate}>
        <Icon name="plus" size={13} /> New event
      </button>
    </div>

    {#if view === "grid"}
      <div class="grid-head">
        <div class="mtitle">
          <strong class="mname">{monthName}</strong>
          <span class="myear">{monthYear}</span>
        </div>
        <div class="mnav">
          <button class="pg" aria-label="Previous month" onclick={() => pageMonth(-1)}>
            <span class="chev-l"><Icon name="chevron" size={14} /></span>
          </button>
          <button class="pg tdy" onclick={goToday}>Today</button>
          <button class="pg" aria-label="Next month" onclick={() => pageMonth(1)}>
            <Icon name="chevron" size={14} />
          </button>
        </div>
      </div>
      <div class="gridwrap" onpointerdown={swipeStart} onpointerup={swipeEnd} onpointercancel={() => (swipe = null)}>
        <div class="dows">
          {#each dayNames as d, i (i)}
            <span class="dow muted">{d}</span>
          {/each}
        </div>
        {#key monthLabel}
          <div
            class="grid"
            role="grid"
            aria-label={monthLabel}
            in:fly={{ x: reduceMotion ? 0 : pageDir * 14, duration: reduceMotion ? 90 : 180 }}
          >
            {#each cells as c (c.key)}
              <button
                class="cell"
                class:outm={c.out}
                class:today={c.today}
                class:sel={selectedDay === c.key}
                class:hasev={c.count > 0}
                role="gridcell"
                aria-label="{c.key}{c.count ? `, ${c.count} event${c.count === 1 ? '' : 's'}` : ''}"
                onclick={() => pickDay(c.key)}
              >
                <span class="dn">{c.n}</span>
                <span class="chipstack" aria-hidden="true">
                  {#each c.titles.slice(0, 2) as t, i (i)}
                    <span class="evchip" class:live={c.live && i === 0}>{t}</span>
                  {/each}
                  {#if c.count > 2}
                    <span class="evmore">+{c.count - 2}</span>
                  {/if}
                </span>
              </button>
            {/each}
          </div>
        {/key}
      </div>
    {/if}

    {#if selectedDay}
      <div class="dayline">
        <strong class="kicker dayk">{fmtDayHeading(selectedDay)}</strong>
        <button class="ghost mini-clear" onclick={() => (selectedDay = "")}>Show all</button>
      </div>
    {/if}

    <div class="list">
      {#each groups as grp (grp.key)}
        {#if !selectedDay}
          <div class="dayhead kicker">{fmtDayHeading(grp.key)}</div>
        {/if}
        {#each grp.events as ev, i (ev.key || ev.id)}
          <div class="riser" style="animation-delay:{Math.min(grp.offset + i, 8) * 24}ms">
            <EventCard {ev} {g} onEdit={startEdit} {onJoinVoice} bubble="time" />
          </div>
        {/each}
      {:else}
        {#if !pastEvents.length}
          <EmptyState icon="calendar" headline={emptyHead} sub={emptyBody}>
            {#snippet actions()}
              <button class="primary" onclick={startCreate}>
                <Icon name="plus" size={13} /> Plan something
              </button>
            {/snippet}
          </EmptyState>
        {/if}
      {/each}
      {#if pastEvents.length}
        <button class="pastbtn muted" onclick={() => (showPast = !showPast)}>
          <span class="pchev" class:open={showPast}><Icon name="chevron" size={11} /></span>
          {showPast ? "Hide" : "Show"} {pastEvents.length} past event{pastEvents.length === 1 ? "" : "s"}
        </button>
        {#if showPast}
          {#each [...pastEvents].reverse() as ev (ev.key || ev.id)}
            <div class="riser">
              <EventCard {ev} {g} onEdit={startEdit} {onJoinVoice} />
            </div>
          {/each}
        {/if}
      {/if}
    </div>
  {/if}
</Modal>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .spring {
    flex: 1;
  }
  .seg {
    display: inline-flex;
    padding: 3px;
    gap: 2px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .seg-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 11px;
    border-radius: calc(var(--radius-md) - 3px);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .seg-btn.on {
    background: var(--bg-1);
    color: var(--text);
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.25);
  }
  .primary {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 7px 14px;
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-md);
    font-weight: 600;
    font-size: var(--fs-ui);
  }
  .primary:disabled {
    opacity: 0.5;
  }
  .ghost.export {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 6px 10px;
    font-size: var(--fs-compact);
  }
  /* ---- month grid, editorial: the month is typography, not 42 boxes ---- */
  .grid-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-2);
    margin-top: var(--sp-1);
  }
  .mtitle {
    display: flex;
    align-items: baseline;
    gap: 7px;
    min-width: 0;
  }
  .mname {
    font-size: var(--fs-display);
    font-weight: 700;
    line-height: 1.1;
  }
  .myear {
    font-size: var(--fs-display);
    font-weight: 300;
    color: var(--text-faint);
    line-height: 1.1;
  }
  .mnav {
    display: inline-flex;
    align-items: center;
    gap: 2px;
  }
  .pg {
    display: grid;
    place-items: center;
    min-width: 34px;
    height: 34px;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
  }
  .pg.tdy {
    padding: 0 10px;
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .pg:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .chev-l {
    display: flex;
    transform: rotate(180deg);
  }
  .gridwrap {
    touch-action: pan-y; /* horizontal is ours (swipe to page months) */
    overflow: hidden;
    /* The dialog is a column flexbox with a max-height: overflow:hidden makes
       this the ONLY shrinkable child, and the whole month silently collapses
       to zero. Never shrink — the dialog scrolls instead. */
    flex: none;
  }
  .dows,
  .grid {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    column-gap: 2px;
  }
  .dow {
    text-align: center;
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 2px 0 6px;
    color: var(--text-faint);
  }
  /* Flat cells — numbers on the page. Hairlines separate week ROWS only. */
  .cell {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    gap: 1px;
    min-height: 62px;
    min-width: 0;
    padding: 3px 2px 2px;
    border-radius: 0;
    background: transparent;
    border: 0;
    border-top: 1px solid transparent;
    color: var(--text);
    font-size: var(--fs-compact);
  }
  .grid .cell:nth-child(n + 8) {
    border-top-color: var(--hairline);
  }
  .cell:hover .dn {
    background: var(--bg-3);
  }
  .cell.outm {
    color: var(--text-faint);
  }
  .dn {
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    font-variant-numeric: tabular-nums;
    transition: background var(--dur-quick) ease;
  }
  /* TODAY is the loudest mark on the grid: a filled accent disc. */
  .cell.today .dn {
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: 700;
  }
  /* SELECTED is a ring — distinct from today even when they coincide. */
  .cell.sel .dn {
    box-shadow: 0 0 0 1.5px var(--accent);
    color: var(--accent-hover);
    font-weight: 700;
  }
  .cell.today.sel .dn {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--accent-fg);
  }
  /* Titles, not dots. A three-pixel mark told you a day was busy and nothing
     about what with; the name is the only reason to look at a month grid at
     all. Two chips fit a cell honestly, and the count carries the rest. */
  .chipstack {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 1px;
    width: 100%;
    min-width: 0;
  }
  .evchip {
    display: block;
    max-width: 100%;
    padding: 0 4px;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    color: var(--accent-hover);
    font-size: var(--fs-micro);
    line-height: 1.5;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .evchip.live {
    background: color-mix(in srgb, var(--ok) 26%, transparent);
    color: var(--ok);
  }
  .evmore {
    padding: 0 4px;
    font-size: var(--fs-micro);
    line-height: 1.4;
    color: var(--text-faint);
    text-align: left;
  }
  .cell.outm .evchip,
  .cell.outm .evmore {
    opacity: 0.55;
  }
  .dayline {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
  }
  .dayk {
    color: var(--text-muted);
  }
  .mini-clear {
    padding: 4px 10px;
    font-size: var(--fs-compact);
  }
  /* ---- agenda ---- */
  .list {
    display: flex;
    flex-direction: column;
  }
  /* Day headings: kicker-voiced, sticky under the sheet's pinned top strip. */
  .dayhead {
    position: sticky;
    top: 33px; /* just below the pinned title strip */
    z-index: 2;
    background: var(--bg-elevated);
    color: var(--text-faint);
    padding: 8px 0 4px;
    margin-top: var(--sp-2);
  }
  .riser {
    animation: ev-rise var(--dur-calm) var(--ease-calm) backwards;
  }
  .riser + .riser {
    border-top: 1px solid var(--hairline);
  }
  @keyframes ev-rise {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  .pastbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    align-self: flex-start;
    background: transparent;
    padding: var(--sp-2) var(--sp-1);
    font-size: var(--fs-compact);
  }
  .pastbtn:hover {
    color: var(--text);
  }
  .pchev {
    display: flex;
    transition: transform var(--dur-standard) ease;
  }
  .pchev.open {
    transform: rotate(90deg);
  }
  /* ---- create / edit: three taps, not a tax form ---- */
  .form {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  /* Title first, borderless: an underline that warms on focus. */
  .form .title-in {
    background: transparent;
    border: 0;
    border-bottom: 1px solid var(--hairline);
    border-radius: 0;
    box-shadow: none;
    padding: 6px 2px 8px;
    font-size: var(--fs-title);
    font-weight: 650;
  }
  .form .title-in:focus {
    border-bottom-color: var(--accent);
    background: transparent;
    box-shadow: none;
  }
  .echo {
    color: var(--accent-hover);
    padding: 0 2px;
  }
  .echo.unset {
    color: var(--text-faint);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  /* The bookpage slot pill, verbatim geometry: 999px, tabular, tint-on-select. */
  .slotchip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 6px 13px;
    border-radius: 999px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    transition: background var(--dur-quick) ease, border-color var(--dur-quick) ease, color var(--dur-quick) ease;
  }
  .slotchip:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .slotchip.on {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent-hover);
  }
  .slotchip.more {
    background: transparent;
    border-style: dashed;
  }
  .row2 {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    align-items: flex-end;
  }
  .fld {
    flex: 1 1 150px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    min-width: 0;
  }
  /* A datetime-local field has a real intrinsic width (segments + spinner);
     squeezing it clips the year — wrapping is the better failure, and .row2
     already wraps.

     `fit-content` rather than a number. 170px was measured against one engine,
     one locale and one face, and it is short in all three directions: the
     desktop build's WebKitGTK spaces the segments wider than Blink does, so at
     170px the whole AM/PM segment fell outside the box — an event start that
     read "08/28/2026, 03:35" with no way to see, or reach, whether it was
     morning or afternoon. A 24-hour locale needs less than 170 and a theme pack
     that swaps in a wide face needs more, so the only number that is right
     everywhere is the one the control computes for itself. */
  .fld input[type="datetime-local"] {
    min-width: fit-content;
  }
  /* The floor has to sit on the FLEX ITEM as well as on the control. `.fld`
     carries `min-width: 0` so that long labels can shrink, which also lets it
     shrink under the input it wraps: with the floor only on the input, the
     field grew to fit AM/PM and then hung 27px past its own label, straight
     under the "Lasts" picker beside it. The two are separate decisions and
     both have to be told. */
  .startfld {
    min-width: fit-content;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  textarea {
    resize: vertical;
  }
  /* ---- WHERE: the one loud decision in the form ---- */
  .where-fld {
    gap: var(--sp-2);
    /* .fld's flex-basis (150px) means WIDTH inside .row2, but this fld sits
       directly in the COLUMN .form where it becomes a height floor — invisible
       under the tall guild panes, an 80px void under Notes' single input. */
    flex: 0 0 auto;
  }
  /* The calendar .seg grown up: two full-width cells, and the CHOSEN one is
     accent-FILLED — deliberately louder than the view tabs' quiet thumb,
     because those are navigation and this is a decision. */
  .where-seg {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 2px;
    padding: 3px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .wseg {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: left;
    gap: 1px;
    padding: var(--sp-2) var(--sp-3);
    border-radius: calc(var(--radius-md) - 3px);
    min-height: var(--tap-min);
    background: transparent;
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease,
      box-shadow 0.18s var(--ease-calm);
  }
  .wseg-t {
    font-size: var(--fs-ui);
    font-weight: 650;
    color: var(--text-muted);
    transition: color var(--dur-quick) ease;
  }
  .wseg-h {
    font-size: var(--fs-small);
    color: var(--text-faint);
    transition: color var(--dur-quick) ease;
  }
  .wseg:hover {
    background: var(--bg-3);
  }
  .wseg:hover .wseg-t {
    color: var(--text);
  }
  .wseg[aria-checked="true"] {
    background: var(--accent);
    box-shadow: var(--accent-glow);
  }
  .wseg[aria-checked="true"] .wseg-t {
    color: var(--accent-fg);
  }
  .wseg[aria-checked="true"] .wseg-h {
    color: color-mix(in srgb, var(--accent-fg) 72%, transparent);
  }
  /* The reveal slot: no fixed height — Details below rides the change; slide
     handles the overflow clipping for free. */
  .wpane-in {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  /* ---- the channel picker: a real list, not a native select ---- */
  .chpick {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-1);
    max-height: min(292px, 38 * var(--dvh)); /* Create stays reachable below */
    overflow-y: auto;
    overscroll-behavior: contain; /* the dialog must not steal the thumb */
  }
  .chkick {
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-faint);
    padding: var(--sp-2) var(--sp-2) var(--sp-1);
  }
  .chrow {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    min-height: var(--tap-min);
    padding: 0 var(--sp-2);
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
    color: var(--text-muted);
    background: transparent;
    border: 1px solid transparent;
    text-align: left;
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .chrow:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  /* The .slotchip.on palette scaled to a 44px row: selection lands with the
     same snap the When chips taught. */
  .chrow[aria-checked="true"] {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent-hover);
    font-weight: 650;
  }
  .chglyph {
    width: 1.5em;
    display: inline-flex;
    justify-content: center;
    flex-shrink: 0;
    color: var(--text-faint); /* the check inherits the row's accent instead */
  }
  .chrow[aria-checked="true"] .chglyph {
    color: inherit;
  }
  .chname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* ---- the remote room card: the guests' door, not naked checkboxes ---- */
  .roomcard {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-3);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease;
  }
  .roomcard.armed {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    background: color-mix(in srgb, var(--accent-soft) 40%, var(--bg-2));
  }
  .room-top {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    cursor: pointer; /* the whole row is the label → flips the switch */
    min-height: var(--tap-min);
  }
  .room-badge {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .room-txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .room-txt strong {
    font-size: var(--fs-ui);
    font-weight: 650;
  }
  .room-sub {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    line-height: 1.4;
  }
  .room-door {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    align-items: flex-start;
  }
  /* ---- the DM "right here" card: the roomcard's armed voice, stating the
     default rather than asking a question — a DM event lives in the DM. ---- */
  .herecard {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-3);
    border-radius: var(--radius-md);
    border: 1px solid color-mix(in srgb, var(--accent) 45%, var(--border));
    background: color-mix(in srgb, var(--accent-soft) 40%, var(--bg-2));
    min-height: var(--tap-min);
  }
  .here-badge {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .here-txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .here-txt strong {
    font-size: var(--fs-ui);
    font-weight: 650;
  }
  .here-sub {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    line-height: 1.4;
  }
  /* The quiet exit under the card — a link, not a control, because 90% of DM
     events never leave the chat. */
  .wlink {
    align-self: flex-start;
    padding: 4px 2px;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 600;
    text-decoration: underline;
    text-decoration-color: color-mix(in srgb, currentColor 40%, transparent);
    text-underline-offset: 3px;
  }
  .wlink:hover {
    color: var(--accent-hover);
  }
  .gnote {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-compact);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
  }
  .actions .ghost {
    padding: 7px 14px;
  }
  @media (prefers-reduced-motion: reduce) {
    .riser {
      animation: none;
    }
    .wseg {
      transition: background var(--dur-quick) ease, color var(--dur-quick) ease; /* drop the glow bloom */
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .dayhead {
      top: 44px; /* the sheet's grip strip is taller on phones */
    }
    .slotchip {
      min-height: 44px;
      padding: 6px 15px;
    }
    /* One-handed at 393px: every Where control is a real target. The cells,
       rows and room label already ride --tap-min (44px here); the door
       segment's buttons are the one pair that needs the floor stated. */
    .room-door .seg-btn {
      min-height: var(--tap-min);
    }
    .room-door .seg {
      align-self: stretch;
    }
    .room-door .seg-btn {
      flex: 1;
      justify-content: center;
    }
    /* The bar wraps on a phone; the create CTA takes its row whole. */
    .primary.new {
      flex: 1 1 100%;
      justify-content: center;
    }
    /* The "somewhere else…" link is a one-thumb target too. */
    .wlink {
      min-height: var(--tap-min);
    }
  }
</style>
