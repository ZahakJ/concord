<script>
  // "Your calendar": one agenda across every guild — the daily-driver view.
  // Grouped by day, each event badged with its guild's initials bubble (the
  // card handles that via showGuild). Read-mostly: RSVPs and reminders work
  // here, creating/editing happens in the guild's own calendar.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EventCard from "../EventCard.svelte";
  import { S } from "../lib/state.svelte.js";
  import { on } from "../lib/api.js";
  import { EV, loadAllEvents, dayKey, fmtDayHeading } from "../lib/events.svelte.js";

  let { onClose } = $props();

  $effect(() => {
    loadAllEvents();
    return on("guild-updated", () => loadAllEvents());
  });

  const startOfToday = () => new Date(new Date().toDateString()).getTime() / 1000;

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
    return out;
  });

  // Open the guild's own calendar to plan — from here you pick WHERE first.
  function planIn(g) {
    S.modal = { kind: "events", guildId: g.id };
  }
  const firstGuild = $derived(S.guilds.find((g) => g.kind !== "dm") || null);
</script>

<Modal title="Your calendar" {onClose} wide>
  <div class="list">
    {#each groups as grp (grp.key)}
      <div class="dayhead muted">{fmtDayHeading(grp.key)}</div>
      {#each grp.items as { ev, g } (ev.id)}
        <EventCard {ev} {g} showGuild />
      {/each}
    {:else}
      <div class="empty">
        <span class="badge"><Icon name="calendar" size={20} /></span>
        <strong>A clear horizon</strong>
        <p class="muted">
          Nothing coming up in any of your guilds. Enjoy the quiet — or be the one who plans
          something.
        </p>
        {#if firstGuild}
          <button class="primary" onclick={() => planIn(firstGuild)}>
            <Icon name="plus" size={13} /> Open {firstGuild.name}'s calendar
          </button>
        {/if}
      </div>
    {/each}
  </div>
</Modal>

<style>
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
</style>
