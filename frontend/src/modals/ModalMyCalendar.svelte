<script>
  // "Your calendar": one agenda across every guild — the daily-driver view.
  // A masthead answers "what's my day?" in one glance (weekday, date, the
  // next thing coming); the river below groups every guild's events by day,
  // each entry wearing its guild in the kicker. A now-line floats in Today's
  // group so you can see where you stand in the day. Read-mostly: RSVPs and
  // reminders work here, creating/editing happens in the guild's own calendar.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EventCard from "../EventCard.svelte";
  import { S, clockOpts } from "../lib/state.svelte.js";
  import { on } from "../lib/api.js";
  import { EV, loadAllEvents, dayKey, fmtDayHeading, happeningNow, isPast } from "../lib/events.svelte.js";

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

  // Every guild's events in one river, soonest first. Past ones stay out of
  // the daily driver entirely — this view answers "what's coming", the guild
  // panel keeps the archive.
  const upcoming = $derived(
    S.guilds
      .flatMap((g) => (EV.byGuild[g.id] || []).map((ev) => ({ ev, g })))
      .filter(({ ev }) => (ev.endUnix || ev.startUnix + 3600) >= startOfToday())
      .sort((a, b) => a.ev.startUnix - b.ev.startUnix),
  );

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

  // Open the guild's own calendar to plan — from here you pick WHERE first.
  function planIn(g) {
    S.modal = { kind: "events", guildId: g.id };
  }
  const firstGuild = $derived(S.guilds.find((g) => g.kind !== "dm") || null);
</script>

<Modal title="Your calendar" {onClose} wide>
  <header class="mast">
    <div class="kicker mday">{mastDay}</div>
    <div class="mdate">{mastDate}</div>
    {#if nextUp}
      <div class="mnext" class:live={nextUp.kind === "now"}>
        {#if nextUp.kind === "now"}
          <span class="mn-dot"></span>Now: {nextUp.ev.title} — happening in {nextUp.g.name}
        {:else}
          Next: {nextUp.ev.title} — {relSoon(nextUp.ev.startUnix)} · {nextUp.g.name}
        {/if}
      </div>
    {:else if upcoming.length}
      <div class="mnext quiet">Nothing else today</div>
    {/if}
  </header>

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
      <div class="empty">
        <span class="badge"><Icon name="calendar" size={20} /></span>
        <strong>A clear horizon</strong>
        <p class="muted">
          Nothing scheduled in any of your guilds. Enjoy it — or be the reason everyone else
          can't.
        </p>
        {#if firstGuild}
          <button class="primary" onclick={() => planIn(firstGuild)}>
            <Icon name="plus" size={13} /> Open {firstGuild.name}'s calendar
          </button>
        {/if}
      </div>
    {/if}
  </div>
</Modal>

<style>
  /* ---- masthead: the reason to open this every morning ---- */
  .mast {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 2px 2px 6px;
    border-bottom: 1px solid var(--hairline);
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
  }
</style>
