<script>
  // "Your calendar": ONE river across everything you're part of — guild
  // events, DM plans, and your private Notes reminders — the per-user blend.
  // A masthead answers "what's my day?" in one glance (weekday, date, the
  // next thing coming); the river below groups every source's events by day,
  // each entry wearing where it came from in the kicker: a guild its badge, a
  // DM the person/group (tap-through), Notes a Private seal. Guild calendars
  // themselves stay shared-only — the blending happens HERE, on your screen,
  // never in anyone else's view. Read-mostly: RSVPs and reminders work here,
  // creating/editing happens in each room's own calendar.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmptyState from "../EmptyState.svelte";
  import EventCard from "../EventCard.svelte";
  import { fly } from "svelte/transition";
  import { S, clockOpts, flash, refreshGuilds } from "../lib/state.svelte.js";
  import { api, on } from "../lib/api.js";
  import {
    EV,
    loadAllEvents,
    dayKey,
    fmtDayHeading,
    happeningNow,
    isPast,
    guildDotColor,
  } from "../lib/events.svelte.js";

  let { onClose, onJoinVoice } = $props();

  $effect(() => {
    loadAllEvents();
    return on("guild-updated", () => loadAllEvents());
  });

  // One slow tick drives the masthead's "in 3 h" and the now-line's clock.
  let now = $state(Date.now() / 1000);
  $effect(() => {
    const t = setInterval(() => (now = Date.now() / 1000), 30000);
    return () => clearInterval(t);
  });

  const startOfToday = () => new Date(new Date().toDateString()).getTime() / 1000;
  const todayKey = $derived(dayKey(now));

  // EVERYTHING, everywhere, soonest first — the month grid needs the archive
  // too (last week's dots are part of the month's shape).
  const allItems = $derived(
    S.guilds
      .flatMap((g) => (EV.byGuild[g.id] || []).map((ev) => ({ ev, g })))
      .sort((a, b) => a.ev.startUnix - b.ev.startUnix),
  );

  // The agenda's slice: past ones stay out of the daily driver entirely —
  // that view answers "what's coming", the guild panel keeps the archive.
  const upcoming = $derived(
    allItems.filter(({ ev }) => (ev.endUnix || ev.startUnix + 3600) >= startOfToday()),
  );

  // ---- Agenda | Month ----
  // A device preference, not app state: remembered per device on desktop,
  // while a phone always lands on the agenda — at 393px "what's my day?" is
  // the daily-driver question and the grid stays one tap away.
  const VIEW_KEY = "concord.mycal.view";
  let view = $state(!S.isMobile && localStorage.getItem(VIEW_KEY) === "month" ? "month" : "agenda");
  function setView(v) {
    view = v;
    try {
      localStorage.setItem(VIEW_KEY, v);
    } catch {
      /* private mode: the toggle still works, it just isn't remembered */
    }
  }
  const reduceMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

  const groups = $derived.by(() => {
    const out = [];
    for (const item of upcoming) {
      const k = dayKey(item.ev.startUnix);
      if (!out.length || out[out.length - 1].key !== k) out.push({ key: k, items: [] });
      out[out.length - 1].items.push(item);
    }
    // The daily driver must answer "what's today?" even when the answer is
    // nothing — Today always renders when the river has anything at all (the
    // full empty state below owns the totally-clear case).
    if (out.length && !out.some((grp) => grp.key === todayKey)) out.unshift({ key: todayKey, items: [] });
    let off = 0;
    for (const grp of out) {
      grp.offset = off;
      off += grp.items.length;
    }
    return out;
  });

  // ---- month grid (ModalEvents' grid DNA, re-cut for the blend) ----
  // Where an event comes from, as a dot the eye can sort at 5px: a guild dot
  // wears the same deterministic tint as its rail badge, a DM/chat dot is
  // accent (the srctag.dm color), Private is a hollow ring — the agenda's
  // source-tag language, shrunk to grid scale.
  const srcOf = (g) => (g.dmNotes ? "notes" : g.kind === "dm" ? "dm" : "guild");
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
    // Per day: up to 3 dots in start order, each wearing its source. A live
    // event's dot goes --ok — "now" outranks "where" for the one hour it's on.
    const byDay = {};
    for (const { ev, g } of allItems) {
      const live = happeningNow(ev);
      (byDay[dayKey(ev.startUnix)] ||= []).push({
        cls: live ? "livedot" : srcOf(g),
        tint: !live && srcOf(g) === "guild" ? guildDotColor(g.id) : "",
      });
    }
    const today = new Date().toDateString();
    const out = [];
    // Always 6 rows: the grid keeps one height as months page by, so the ‹ ›
    // buttons don't jump under a paging thumb.
    for (let i = 0; i < 42; i++) {
      const d = new Date(start);
      d.setDate(start.getDate() + i);
      const key = d.toDateString();
      const evs = byDay[key] || [];
      out.push({
        key,
        n: d.getDate(),
        out: d.getMonth() !== month.getMonth(),
        today: key === today,
        count: evs.length,
        dots: evs.slice(0, 3),
      });
    }
    return out;
  });
  function pageMonth(dir) {
    pageDir = dir;
    month = new Date(month.getFullYear(), month.getMonth() + dir, 1);
  }
  function goToday() {
    const d = new Date();
    pageDir = d > month ? 1 : -1;
    month = new Date(d.getFullYear(), d.getMonth(), 1);
    pickDay(d.toDateString());
  }
  // The day under the grid: opens on today, so the month view answers the
  // same "what's my day?" the masthead does before a single tap.
  let selectedDay = $state(new Date().toDateString());
  let dayPanelEl = $state(null);
  function pickDay(key) {
    selectedDay = key;
    // The reveal lives under the grid — walk the eye (and the sheet) there.
    requestAnimationFrame(() =>
      dayPanelEl?.scrollIntoView({ block: "nearest", behavior: reduceMotion ? "auto" : "smooth" }),
    );
  }
  const dayItems = $derived(allItems.filter(({ ev }) => dayKey(ev.startUnix) === selectedDay));
  // Legend rows only for sources actually on the calendar — a three-line
  // glossary under an all-guild month is noise.
  const srcsPresent = $derived.by(() => {
    const s = new Set();
    for (const { g } of allItems) s.add(srcOf(g));
    return s;
  });

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

  // ---- masthead ----
  const mastDay = $derived(new Date(now * 1000).toLocaleDateString([], { weekday: "long" }));
  const mastDate = $derived(new Date(now * 1000).toLocaleDateString([], { month: "long", day: "numeric" }));
  const fmtClock = (u) =>
    new Date(u * 1000).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...clockOpts() });
  // Next: the first thing left TODAY — a live one wins and reads "Now".
  const nextUp = $derived.by(() => {
    const todays = upcoming.filter(({ ev }) => dayKey(ev.startUnix) === todayKey && !isPast(ev, now));
    const live = todays.find(({ ev }) => happeningNow(ev, now));
    if (live) return { kind: "now", ...live };
    return todays.length ? { kind: "next", ...todays[0] } : null;
  });
  const relSoon = (startUnix) => {
    const mins = Math.max(1, Math.round((startUnix - now) / 60));
    if (mins < 60) return `in ${mins} min`;
    return `in ${Math.round(mins / 60)} h`;
  };

  // ---- the now-line ----
  // Inside Today's group: a thin accent rule between what has started/passed
  // and what hasn't. Only drawn with entries on BOTH sides — a line above or
  // below everything is just noise.
  const nowIdx = $derived.by(() => {
    const grp = groups.find((x) => x.key === todayKey);
    if (!grp || grp.items.length < 2) return -1;
    const i = grp.items.findIndex(({ ev }) => ev.startUnix > now);
    return i > 0 ? i : -1;
  });

  // Where an entry comes from, masthead-voiced: a guild is a place ("in"), a
  // DM is people ("with"), Notes is nobody's business but yours ("private").
  const srcPhrase = (g) => (g.dmNotes ? "private" : g.kind === "dm" ? `with ${g.name}` : `in ${g.name}`);

  // Open the guild's own calendar to plan — from here you pick WHERE first.
  function planIn(g) {
    S.modal = { kind: "events", guildId: g.id };
  }
  const firstGuild = $derived(S.guilds.find((g) => g.kind !== "dm") || null);

  // The private door: open (creating on first use) the Notes self-DM's
  // calendar. NotesDM() is idempotent, and a fresh Notes needs a guild
  // refresh or ModalEvents opens onto a guild id S.guilds can't resolve.
  async function planPrivate() {
    try {
      const notes = await api.notesDM();
      if (!S.guilds.some((g) => g.id === notes.id)) await refreshGuilds();
      S.modal = { kind: "events", guildId: notes.id };
    } catch (err) {
      flash(err);
    }
  }
</script>

<Modal title="Your calendar" {onClose} wide>
  <header class="mast">
    <div class="mtxt">
      <div class="kicker mday">{mastDay}</div>
      <div class="mdate">{mastDate}</div>
      {#if nextUp}
        <div class="mnext" class:live={nextUp.kind === "now"}>
          {#if nextUp.kind === "now"}
            <span class="mn-dot"></span>Now: {nextUp.ev.title} — {srcPhrase(nextUp.g)}
          {:else}
            Next: {nextUp.ev.title} — {relSoon(nextUp.ev.startUnix)} · {nextUp.g.dmNotes ? "private" : nextUp.g.name}
          {/if}
        </div>
      {:else if upcoming.length}
        <div class="mnext quiet">Nothing else today</div>
      {/if}
    </div>
    <div class="seg" role="tablist" aria-label="Calendar view">
      <button class="seg-btn" class:on={view === "agenda"} role="tab" aria-selected={view === "agenda"} onclick={() => setView("agenda")}>
        <Icon name="list" size={13} /> Agenda
      </button>
      <button class="seg-btn" class:on={view === "month"} role="tab" aria-selected={view === "month"} onclick={() => setView("month")}>
        <Icon name="calendar" size={13} /> Month
      </button>
    </div>
  </header>

  {#if view === "month"}
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
    <!-- svelte-ignore a11y_no_static_element_interactions -->
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
              role="gridcell"
              aria-label="{c.key}{c.count ? `, ${c.count} event${c.count === 1 ? '' : 's'}` : ''}"
              onclick={() => pickDay(c.key)}
            >
              <span class="dn">{c.n}</span>
              <span class="dots" aria-hidden="true">
                {#each c.dots as d, i (i)}
                  <span class="dot {d.cls}" style={d.tint ? `background:${d.tint}` : ""}></span>
                {/each}
              </span>
            </button>
          {/each}
        </div>
      {/key}
    </div>
    {#if srcsPresent.size}
      <!-- The grid's key, kicker-voiced. The guild swatch is a color wheel:
           "each guild wears its own", the same tints the rail badges use. -->
      <div class="legend kicker" aria-hidden="true">
        {#if srcsPresent.has("guild")}<span class="lg"><span class="lgdot wheel"></span>Guilds</span>{/if}
        {#if srcsPresent.has("dm")}<span class="lg"><span class="lgdot dm"></span>Chats</span>{/if}
        {#if srcsPresent.has("notes")}<span class="lg"><span class="lgdot notes"></span>Private</span>{/if}
      </div>
    {/if}
    <div class="daypanel" bind:this={dayPanelEl}>
      <div class="dayline">
        <strong class="kicker dayk">{fmtDayHeading(selectedDay)}</strong>
        {#if dayItems.length}
          <span class="kicker daycount">{dayItems.length} event{dayItems.length === 1 ? "" : "s"}</span>
        {/if}
      </div>
      {#if dayItems.length}
        {#each dayItems as { ev, g }, i (ev.id)}
          <div class="riser" style="animation-delay:{Math.min(i, 8) * 24}ms">
            <EventCard {ev} {g} showGuild {onJoinVoice} bubble="time" />
          </div>
        {/each}
      {:else}
        <p class="dayempty muted">Nothing this day — anywhere.</p>
      {/if}
    </div>
  {:else}
  <div class="list">
    {#each groups as grp (grp.key)}
      <div class="dayhead kicker">
        {fmtDayHeading(grp.key)}{#if grp.key === todayKey && !grp.items.length}<span class="dh-none"> — nothing scheduled</span>{/if}
      </div>
      {#each grp.items as { ev, g }, i (ev.id)}
        {#if grp.key === todayKey && i === nowIdx}
          <div class="nowline" role="presentation">
            <span class="nl-dot"></span>
            <span class="nl-rule"></span>
            <span class="nl-label kicker">now · {fmtClock(now)}</span>
          </div>
        {/if}
        <div class="riser" style="animation-delay:{Math.min(grp.offset + i, 8) * 24}ms">
          <EventCard {ev} {g} showGuild {onJoinVoice} bubble="time" />
        </div>
      {/each}
    {/each}
    {#if !upcoming.length}
      <EmptyState
        icon="calendar"
        headline="A clear horizon"
        sub="Nothing coming up anywhere — no guild events, no plans in your chats, nothing private. Enjoy it — or be the reason everyone else can't."
      >
        {#snippet actions()}
          {#if firstGuild}
            <button class="primary" onclick={() => planIn(firstGuild)}>
              <Icon name="plus" size={13} /> Open {firstGuild.name}'s calendar
            </button>
          {/if}
          <!-- The private lane is always available — Notes exists (or is
               created) on demand, and only your devices ever see it. -->
          <button class="ghost priv" onclick={planPrivate}>
            <Icon name="lock" size={12} /> Add something private
          </button>
        {/snippet}
      </EmptyState>
    {/if}
  </div>
  {/if}
</Modal>

<style>
  /* ---- masthead: the reason to open this every morning ---- */
  /* A row now: the date column keeps the left edge, the Agenda|Month toggle
     rides the right — no extra bar row stealing height from the river. */
  .mast {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: 2px 2px 6px;
    border-bottom: 1px solid var(--hairline);
  }
  .mtxt {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  /* ModalEvents' segmented control, verbatim geometry — same words, same
     place in the eye's grammar (view navigation, quiet thumb). */
  .seg {
    display: inline-flex;
    padding: 3px;
    gap: 2px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    flex-shrink: 0;
    margin-top: 2px;
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
  .mday {
    color: var(--text-faint);
  }
  .mdate {
    font-size: var(--fs-display);
    font-weight: 700;
    line-height: 1.15;
  }
  .mnext {
    font-size: var(--fs-compact);
    color: var(--accent-hover);
    margin-top: 3px;
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mnext.live {
    color: var(--ok-text);
    font-weight: 600;
  }
  .mnext.quiet {
    color: var(--text-faint);
  }
  .mn-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
    flex-shrink: 0;
    animation: mycal-pulse 1.4s ease-in-out infinite;
  }
  @keyframes mycal-pulse {
    50% {
      opacity: 0.3;
    }
  }
  /* ---- month grid: ModalEvents' editorial grid, hosting the blend ---- */
  .grid-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-2);
    margin-top: var(--sp-2);
  }
  .mtitle {
    display: flex;
    align-items: baseline;
    gap: 7px;
    min-width: 0;
  }
  .mname {
    font-size: var(--fs-title);
    font-weight: 700;
    line-height: 1.1;
  }
  .myear {
    font-size: var(--fs-title);
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
  /* The blend's dots: guild = its rail tint (inline style), chat = accent,
     Private = a hollow ring, live = --ok whatever it came from. */
  .dots {
    display: flex;
    gap: 2px;
    height: 5px;
  }
  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent);
    box-sizing: border-box;
  }
  .dot.notes {
    background: transparent;
    border: 1.5px solid var(--text-muted);
  }
  .dot.livedot {
    background: var(--ok);
  }
  .cell.outm .dot {
    opacity: 0.5;
  }
  /* ---- legend: the grid's key, one whisper ---- */
  .legend {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 6px 2px 0;
    color: var(--text-faint);
  }
  .lg {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .lgdot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    box-sizing: border-box;
  }
  .lgdot.wheel {
    /* "Each guild wears its own color": a tiny wheel of the rail-tint hues. */
    background: conic-gradient(hsl(0 52% 58%), hsl(90 52% 58%), hsl(200 52% 58%), hsl(300 52% 58%), hsl(0 52% 58%));
  }
  .lgdot.dm {
    background: var(--accent);
  }
  .lgdot.notes {
    border: 1.5px solid var(--text-muted);
  }
  /* ---- the day under the grid ---- */
  .daypanel {
    display: flex;
    flex-direction: column;
    margin-top: var(--sp-2);
    /* Never collapse under the dialog's max-height squeeze: the empty line
       must stay a visible answer, not a zero-height mystery. */
    flex: none;
  }
  .dayline {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: 8px 0 4px;
    border-top: 1px solid var(--hairline);
  }
  .dayk {
    color: var(--text-muted);
  }
  .daycount {
    color: var(--text-faint);
  }
  .dayempty {
    margin: 2px 0 8px;
    font-size: var(--fs-compact);
  }
  /* ---- the river ---- */
  .list {
    display: flex;
    flex-direction: column;
  }
  .dayhead {
    position: sticky;
    top: 33px; /* just below the sheet's pinned title strip */
    z-index: 2;
    background: var(--bg-elevated);
    color: var(--text-faint);
    padding: 8px 0 4px;
    margin-top: var(--sp-2);
  }
  .dh-none {
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--text-faint);
  }
  .riser {
    animation: mycal-rise var(--dur-calm) var(--ease-calm) backwards;
  }
  .riser + .riser {
    border-top: 1px solid var(--hairline);
  }
  @keyframes mycal-rise {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  /* The now-line: where you stand in today. Zero animation — it just IS. */
  .nowline {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 3px 0;
  }
  .nl-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
  }
  .nl-rule {
    flex: 1;
    height: 1px;
    background: var(--accent);
    opacity: 0.65;
  }
  .nl-label {
    color: var(--accent-hover);
    flex-shrink: 0;
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
  .ghost.priv {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 7px 14px;
    font-size: var(--fs-ui);
  }
  @media (pointer: coarse), (max-width: 768px) {
    .primary,
    .ghost.priv {
      min-height: var(--tap-min);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .riser,
    .mn-dot {
      animation: none;
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .dayhead {
      top: 44px;
    }
    /* One-handed at 393px: the toggle and the day cells are real targets, and
       the masthead stacks so "August 2" never fights the toggle for width. */
    .mast {
      flex-direction: column;
      align-items: stretch;
      gap: var(--sp-2);
    }
    .seg {
      align-self: stretch;
    }
    .seg-btn {
      flex: 1;
      justify-content: center;
      min-height: 40px;
    }
    .cell {
      min-height: 50px;
    }
    .dn {
      width: 30px;
      height: 30px;
    }
  }
</style>
