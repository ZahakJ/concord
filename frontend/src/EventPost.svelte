<script>
  // The card an event posts into its channel when it is created.
  //
  // It renders the LIVE event, looked up by id in the guild's calendar cache —
  // not a snapshot taken at post time. Edit the event and the card in the
  // channel changes with it, RSVP from the card and the answer is the same
  // answer, because there is only ever one record. The token carries a title
  // purely so a peer whose event lane has not caught up yet can still say what
  // the announcement was about instead of drawing an empty box.
  import Icon from "./Icon.svelte";
  import EventCard from "./EventCard.svelte";
  import { S, activeGuild, joinVoiceChannel } from "./lib/state.svelte.js";
  import { EV, loadEvents } from "./lib/events.svelte.js";

  let { token } = $props();

  const g = $derived(activeGuild());
  const ev = $derived((EV.byGuild[S.activeGuildId] || []).find((e) => e.id === token.id) || null);

  // The calendar is loaded lazily by whichever panel opens it, so a channel
  // carrying an announcement is often the FIRST thing to want it. One fetch
  // per guild; loadEvents is idempotent and the cache is shared.
  let asked = false;
  $effect(() => {
    const gid = S.activeGuildId;
    if (!gid || ev || asked) return;
    asked = true;
    loadEvents(gid);
  });
</script>

{#if ev}
  <div class="ep">
    <span class="ep-head"><Icon name="calendar" size={12} /> New event</span>
    <EventCard {ev} {g} onJoinVoice={joinVoiceChannel} bubble="time" />
  </div>
{:else}
  <!-- The record has not arrived (or was deleted). Say which of the two, and
       do not pretend to know a time. -->
  <div class="ep ghost">
    <span class="ep-head"><Icon name="calendar" size={12} /> Event</span>
    <span class="ep-title">{token.title || "An event"}</span>
    <button class="ep-open" onclick={() => (S.modal = { kind: "events" })}>Open the calendar</button>
  </div>
{/if}

<style>
  .ep {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: var(--sp-1);
    max-width: 560px;
  }
  .ep-head {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--accent-hover);
  }
  .ghost {
    padding: 10px var(--sp-3);
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    align-items: flex-start;
  }
  .ep-title {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .ep-open {
    padding: 5px 11px;
    font-size: var(--fs-small);
    background: var(--bg-3);
  }
</style>
