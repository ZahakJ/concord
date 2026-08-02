<script>
  // The guild calendar: agenda list + month grid (toggle), day drill-down,
  // and the create/edit form. The agenda leads on phones — a month grid is a
  // toggle there, never the landing view, because 393px turns grids into
  // squint tests. Desktop lands on the grid. Editorial throughout: typography
  // and hairlines carry the hierarchy, color is reserved for state.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EventCard from "../EventCard.svelte";
  import { fly } from "svelte/transition";
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

  let { onClose, onJoinVoice } = $props();

  // Frozen at open: the panel is about the guild it was opened from, even if
  // a background refresh changes the active guild id.
  const gid = S.modal?.guildId || S.activeGuildId;
  const g = $derived(S.guilds.find((x) => x.id === gid) || null);
  const events = $derived(EV.byGuild[gid] || []);

  let view = $state(S.isMobile ? "agenda" : "grid");
  let selectedDay = $state(""); // a dayKey, or "" = everything
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
    editing ? (editing.id ? "Edit event" : "New event") : `${g?.name || "Guild"} · Events`,
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
    for (const ev of events) {
      const k = dayKey(ev.startUnix);
      counts[k] = (counts[k] || 0) + 1;
      if (happeningNow(ev)) liveDays[k] = true;
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
    return { id: "", title: "", details: "", location: "", locationChannelId: "", start: toLocalInput(at), durMin: 60, guests: false, autoAdmit: false };
  }
  // The OS picker is the fallback, never the greeting: chips first, the
  // datetime-local appears once a time has been touched (or on request).
  let showPicker = $state(false);
  let durMore = $state(false);
  function startCreate() {
    editing = blankDraft();
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
      // else…" and the words survive.
      locationChannelId: locChannels.some((c) => c.id === ev.locationChannelId)
        ? ev.locationChannelId
        : "",
      start: toLocalInput(ev.startUnix * 1000),
      durMin: ev.endUnix ? Math.round((ev.endUnix - ev.startUnix) / 60) : 0,
      guests: false,
      autoAdmit: false,
    };
    // Editing an existing time: show the precise picker straight away, and
    // unfold the full duration list when the value isn't one of the quick three.
    showPicker = true;
    durMore = ![30, 60, 120].includes(editing.durMin);
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
  const draftValid = $derived(!!editing?.title.trim() && !isNaN(draftAt));

  // The echo line: the form reads back what it heard, kicker-voiced, so a
  // disabled Create button always explains itself.
  const echo = $derived.by(() => {
    if (!editing) return "";
    if (isNaN(draftAt)) return "Pick a time";
    const d = new Date(draftAt);
    const day = d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" });
    const t = (x) => x.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    if (editing.durMin) return `${day} · ${t(d)} – ${t(new Date(draftAt + editing.durMin * 60000))}`;
    return `${day} · ${t(d)}`;
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
    // (what ICS export and stale channel lists still show).
    const locCh = locChannels.find((c) => c.id === editing.locationChannelId) || null;
    const locStr = locCh ? locLabel(locCh) : editing.location.trim();
    const locChId = locCh ? locCh.id : "";
    let saved;
    try {
      if (isEdit)
        saved = await api.updateEvent(gid, editing.id, editing.title.trim(), editing.details.trim(), startUnix, endUnix, locStr, locChId);
      else
        saved = await api.createEvent(gid, editing.title.trim(), editing.details.trim(), startUnix, endUnix, locStr, locChId);
    } catch (err) {
      flash(err); // e.g. not allowed to edit someone else's — never fail silently
      return;
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
    flash(isEdit ? "Event updated" : "Event created — everyone can RSVP now", "success");
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
      <div class="kicker echo" class:unset={isNaN(draftAt)}>{echo}</div>
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
          <label class="fld">
            <span class="muted tiny">Starts</span>
            <input type="datetime-local" bind:value={editing.start} />
          </label>
        {/if}
        <div class="fld">
          <span class="muted tiny">Lasts</span>
          {#if durMore}
            <select bind:value={editing.durMin}>
              {#each DURATIONS as d (d.min)}
                <option value={d.min}>{d.label}</option>
              {/each}
            </select>
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
      {#if locChannels.length}
        <!-- The location is a real place in this guild first, words second:
             pick a channel and Join walks people there (voice = its call),
             with the guild posting the start reminder in that chat. The
             native select is the phone-honest picker here. -->
        <label class="fld">
          <span class="muted tiny">Where</span>
          <select class="locsel" bind:value={editing.locationChannelId}>
            <option value="">Somewhere else — type it in…</option>
            {#if locVoice.length}
              <optgroup label="Voice channels">
                {#each locVoice as c (c.id)}
                  <option value={c.id}>🔊 {c.name}</option>
                {/each}
              </optgroup>
            {/if}
            {#if locText.length}
              <optgroup label="Text channels">
                {#each locText as c (c.id)}
                  <option value={c.id}># {c.name}</option>
                {/each}
              </optgroup>
            {/if}
          </select>
        </label>
        {#if editing.locationChannelId}
          <div class="gnote muted">
            <Icon name="bell" size={12} />
            Join takes members straight there{locVoice.some((c) => c.id === editing.locationChannelId)
              ? " and into the call"
              : ""} — and {g?.name || "the guild"} will post a reminder in that channel when it starts.
          </div>
        {:else}
          <input placeholder="Where? An address, a link-up spot, someone's couch…" maxlength="160" bind:value={editing.location} />
        {/if}
      {:else}
        <input placeholder="Where? A channel, an address, someone's couch…" maxlength="160" bind:value={editing.location} />
      {/if}
      <textarea rows="3" placeholder="Details (optional)" maxlength="2000" bind:value={editing.details}></textarea>
      {#if editingHasGuests}
        <div class="gnote muted">
          <Icon name="link" size={12} /> This event already has a room — Join, copy or revoke on the event card.
        </div>
      {:else}
        <label class="chk">
          <input type="checkbox" bind:checked={editing.guests} />
          {#if editing.locationChannelId}
            <!-- With a channel picked, members need no room — the checkbox is
                 ONLY the outsiders' door, and says so. -->
            <span>Also invite outside guests — they get a browser link into a separate, disposable room (this guild stays sealed)</span>
          {:else}
            <span>Open a meeting room — members join in one tap; guests get a shareable browser link</span>
          {/if}
        </label>
        {#if editing.guests}
          <label class="chk sub">
            <input type="checkbox" bind:checked={editing.autoAdmit} />
            <span>Let guests straight in (otherwise they knock and you admit{editing.locationChannelId ? "" : "; members always walk in"})</span>
          </label>
        {/if}
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
                <span class="dots" aria-hidden="true">
                  {#if c.count}
                    {#each Array(Math.min(c.count, 3)) as _, i (i)}<span class="dot" class:livedot={c.live && i === 0}></span>{/each}
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
        {#each grp.events as ev, i (ev.id)}
          <div class="riser" style="animation-delay:{Math.min(grp.offset + i, 8) * 24}ms">
            <EventCard {ev} {g} onEdit={startEdit} {onJoinVoice} bubble="time" />
          </div>
        {/each}
      {:else}
        {#if !pastEvents.length}
          <div class="empty">
            <span class="badge"><Icon name="calendar" size={20} /></span>
            {#if selectedDay}
              <strong>Nothing on {fmtDayHeading(selectedDay)}</strong>
              <p class="muted">A free day. Suspiciously free. Fix that?</p>
            {:else}
              <strong>Nothing on the calendar yet</strong>
              <p class="muted">
                Game night, standup, the big launch — whatever this crew does together, give it a
                time and everyone can RSVP right here.
              </p>
            {/if}
            <button class="primary" onclick={startCreate}>
              <Icon name="plus" size={13} /> Plan something
            </button>
          </div>
        {/if}
      {/each}
      {#if pastEvents.length}
        <button class="pastbtn muted" onclick={() => (showPast = !showPast)}>
          <span class="pchev" class:open={showPast}><Icon name="chevron" size={11} /></span>
          {showPast ? "Hide" : "Show"} {pastEvents.length} past event{pastEvents.length === 1 ? "" : "s"}
        </button>
        {#if showPast}
          {#each [...pastEvents].reverse() as ev (ev.id)}
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
    gap: 8px;
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
    gap: 8px;
    margin-top: 4px;
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
    justify-content: center;
    gap: 1px;
    min-height: 46px;
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
    transition: background 0.12s ease;
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
  .dots {
    display: flex;
    gap: 2px;
    height: 4px;
  }
  .dot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
  }
  .dot.livedot {
    background: var(--ok);
  }
  .cell.outm .dot {
    opacity: 0.5;
  }
  .dayline {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
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
    padding: 8px 4px;
    font-size: var(--fs-compact);
  }
  .pastbtn:hover {
    color: var(--text);
  }
  .pchev {
    display: flex;
    transition: transform 0.15s ease;
  }
  .pchev.open {
    transform: rotate(90deg);
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    text-align: center;
    padding: 26px 16px;
  }
  .empty .badge {
    display: grid;
    place-items: center;
    width: 52px;
    height: 52px;
    border-radius: 16px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    margin-bottom: 4px;
  }
  .empty p {
    margin: 0 0 8px;
    max-width: 340px;
    font-size: var(--fs-compact);
    line-height: 1.5;
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
    transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
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
    gap: 4px;
    min-width: 0;
  }
  /* A datetime-local field has a real intrinsic width (segments + spinner);
     squeezing it clips the year — wrapping is the better failure. */
  .fld input[type="datetime-local"] {
    min-width: 170px;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  textarea {
    resize: vertical;
  }
  .chk {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    /* Real tap target on phones without visually inflating the row. */
    min-height: var(--tap-min, 24px);
  }
  .chk input {
    flex-shrink: 0;
    /* app.css gives every input width:100% for the stacked-field layouts; a
       checkbox in a flex row must NOT take the row (it shoved its own label
       into a one-character column). */
    width: auto;
  }
  .chk.sub {
    margin-left: 24px;
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
    gap: 8px;
  }
  .actions .ghost {
    padding: 7px 14px;
  }
  @media (prefers-reduced-motion: reduce) {
    .riser {
      animation: none;
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
    /* The bar wraps on a phone; the create CTA takes its row whole. */
    .primary.new {
      flex: 1 1 100%;
      justify-content: center;
    }
  }
</style>
