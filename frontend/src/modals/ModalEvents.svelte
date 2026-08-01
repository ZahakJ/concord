<script>
  // The guild calendar: agenda list + month grid (toggle), day drill-down,
  // and the create/edit form. The agenda leads on phones — a month grid is a
  // toggle there, never the landing view, because 393px turns grids into
  // squint tests. Desktop lands on the grid.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EventCard from "../EventCard.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api, on } from "../lib/api.js";
  import {
    EV,
    loadEvents,
    dayKey,
    fmtDayHeading,
    downloadICS,
    icsName,
  } from "../lib/events.svelte.js";

  let { onClose } = $props();

  // Frozen at open: the panel is about the guild it was opened from, even if
  // a background refresh changes the active guild id.
  const gid = S.modal?.guildId || S.activeGuildId;
  const g = $derived(S.guilds.find((x) => x.id === gid) || null);
  const events = $derived(EV.byGuild[gid] || []);

  let view = $state(S.isMobile ? "agenda" : "grid");
  let selectedDay = $state(""); // a dayKey, or "" = everything
  let editing = $state(null); // null | draft {id?, title, details, location, start, durMin}
  let showPast = $state(false);

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
    for (const ev of events) {
      const k = dayKey(ev.startUnix);
      counts[k] = (counts[k] || 0) + 1;
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
        count: counts[key] || 0,
      });
    }
    return out;
  });
  function pageMonth(dir) {
    month = new Date(month.getFullYear(), month.getMonth() + dir, 1);
  }
  function pickDay(key) {
    selectedDay = selectedDay === key ? "" : key; // tap again to widen back out
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
    return { id: "", title: "", details: "", location: "", start: toLocalInput(at), durMin: 60, guests: false, autoAdmit: false };
  }
  function startEdit(ev) {
    editing = {
      id: ev.id,
      title: ev.title,
      details: ev.details || "",
      location: ev.location || "",
      start: toLocalInput(ev.startUnix * 1000),
      durMin: ev.endUnix ? Math.round((ev.endUnix - ev.startUnix) / 60) : 0,
      guests: false,
      autoAdmit: false,
    };
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
  const draftAt = $derived(editing?.start ? new Date(editing.start).getTime() : NaN);
  const draftValid = $derived(!!editing?.title.trim() && !isNaN(draftAt));

  async function save() {
    if (!draftValid) return;
    const startUnix = Math.floor(draftAt / 1000);
    const endUnix = editing.durMin ? startUnix + editing.durMin * 60 : 0;
    const isEdit = !!editing.id;
    let saved;
    try {
      if (isEdit)
        saved = await api.updateEvent(gid, editing.id, editing.title.trim(), editing.details.trim(), startUnix, endUnix, editing.location.trim());
      else
        saved = await api.createEvent(gid, editing.title.trim(), editing.details.trim(), startUnix, endUnix, editing.location.trim());
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
      <!-- svelte-ignore a11y_autofocus -->
      <input
        autofocus={!S.isMobile}
        placeholder="What's happening?"
        maxlength="120"
        bind:value={editing.title}
      />
      <div class="row2">
        <label class="fld">
          <span class="muted tiny">Starts</span>
          <input type="datetime-local" bind:value={editing.start} />
        </label>
        <label class="fld">
          <span class="muted tiny">Lasts</span>
          <select bind:value={editing.durMin}>
            {#each DURATIONS as d (d.min)}
              <option value={d.min}>{d.label}</option>
            {/each}
          </select>
        </label>
      </div>
      <input placeholder="Where? A channel, an address, someone's couch…" maxlength="160" bind:value={editing.location} />
      <textarea rows="3" placeholder="Details (optional)" maxlength="2000" bind:value={editing.details}></textarea>
      {#if editingHasGuests}
        <div class="gnote muted">
          <Icon name="link" size={12} /> Guests can already join — copy or revoke the link on the event card.
        </div>
      {:else}
        <label class="chk">
          <input type="checkbox" bind:checked={editing.guests} />
          <span>Open to guests — get a shareable link anyone can join from a browser</span>
        </label>
        {#if editing.guests}
          <label class="chk sub">
            <input type="checkbox" bind:checked={editing.autoAdmit} />
            <span>Let guests straight in (otherwise they knock and you admit)</span>
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
      <button class="primary new" onclick={() => (editing = blankDraft())}>
        <Icon name="plus" size={13} /> New event
      </button>
    </div>

    {#if view === "grid"}
      <div class="grid-head">
        <button class="pg" aria-label="Previous month" onclick={() => pageMonth(-1)}>
          <span class="chev-l"><Icon name="chevron" size={14} /></span>
        </button>
        <strong class="mlabel">{monthLabel}</strong>
        <button class="pg" aria-label="Next month" onclick={() => pageMonth(1)}>
          <Icon name="chevron" size={14} />
        </button>
      </div>
      <div class="grid" role="grid" aria-label={monthLabel}>
        {#each dayNames as d, i (i)}
          <span class="dow muted">{d}</span>
        {/each}
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
            {#if c.count}
              <span class="dots" aria-hidden="true">
                {#each Array(Math.min(c.count, 3)) as _, i (i)}<span class="dot"></span>{/each}
              </span>
            {/if}
          </button>
        {/each}
      </div>
    {/if}

    {#if selectedDay}
      <div class="dayline">
        <strong>{fmtDayHeading(selectedDay)}</strong>
        <button class="ghost mini-clear" onclick={() => (selectedDay = "")}>Show all</button>
      </div>
    {/if}

    <div class="list">
      {#each groups as grp (grp.key)}
        {#if !selectedDay}
          <div class="dayhead muted">{fmtDayHeading(grp.key)}</div>
        {/if}
        {#each grp.events as ev (ev.id)}
          <EventCard {ev} {g} onEdit={startEdit} />
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
            <button class="primary" onclick={() => (editing = blankDraft())}>
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
            <EventCard {ev} {g} onEdit={startEdit} />
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
  .grid-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 4px;
  }
  .mlabel {
    font-size: var(--fs-ui);
  }
  .pg {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
  }
  .pg:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .chev-l {
    display: flex;
    transform: rotate(180deg);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 3px;
  }
  .dow {
    text-align: center;
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    padding: 2px 0;
  }
  .cell {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    min-height: 44px;
    padding: 2px;
    border-radius: var(--radius-sm);
    background: var(--bg-1);
    border: 1px solid transparent;
    color: var(--text);
    font-size: var(--fs-compact);
  }
  .cell:hover {
    background: var(--bg-3);
  }
  .cell.outm {
    color: var(--text-faint);
    background: transparent;
  }
  /* Today reads as today at a glance: an accent ring, and a bold number. */
  .cell.today {
    border-color: var(--accent);
  }
  .cell.today .dn {
    color: var(--accent-hover);
    font-weight: 700;
  }
  .cell.sel {
    background: var(--accent-soft);
    border-color: var(--accent);
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
  .cell.outm .dot {
    opacity: 0.5;
  }
  .dayline {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .mini-clear {
    padding: 4px 10px;
    font-size: var(--fs-compact);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .dayhead {
    font-size: var(--fs-small);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    margin-top: 6px;
  }
  .pastbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    align-self: flex-start;
    background: transparent;
    padding: 6px 4px;
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
  .form {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .row2 {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
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
</style>
