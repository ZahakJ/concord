<script>
  import Caution from "./Caution.svelte";
  // Leave and delete my copy.
  //
  // The row used to say "Delete guild", in a section headed DANGER ZONE, to the
  // owner of a guild with other people in it. To anyone who has run a community
  // that sentence means the guild is gone. It is not: it is a local
  // unsubscribe. The messages leave THIS device, everybody else keeps theirs,
  // and the guild carries on without an owner — which is the part that actually
  // costs something, because nobody can add or remove a member again unless an
  // heir was named first.
  //
  // Concord cannot offer "close the guild" yet, and a label must not promise
  // what the product cannot do. So the label says what happens, and this page
  // says the consequence out loud with the two doors that answer it beside the
  // sentence rather than three panels away.
  import RailShell from "./RailShell.svelte";
  import Icon from "../Icon.svelte";
  import {
    S,
    activeGuild,
    leaveGuildLabel,
    leaveGuildNow,
    switchPanel,
  } from "../lib/state.svelte.js";
  import { plural } from "../lib/plural.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const others = $derived(Math.max(0, (S.members || []).filter((m) => !m.isSelf).length));
  const heirNamed = $derived(!!g?.heir);
</script>

<RailShell title={leaveGuildLabel(g)} {onClose}>
  {#if g}
    <p class="lead">
      Leaving takes <strong>{g.name}</strong> off this device: its messages are removed from here,
      and it will not come back on its own. Everybody else keeps their copy — nothing you do here
      reaches anyone else's history.
    </p>

    {#if g.isOwner}
      <!-- The owner-specific consequence, and the two things that answer it. -->
      <div class="consequence">
        {#if heirNamed}
          <Caution
            text="You own this guild: until your heir takes over, nobody can add or remove members."
            detail="You have named an heir, so somebody can take the guild over after you go."
          />
        {:else}
          <Caution
            text="You own this guild: after you go, nobody can add or remove members."
            detail="There is no way to appoint anyone once you are gone. Name an heir or transfer ownership first if the guild should carry on."
          />
        {/if}
        <div class="doors">
          <button class="link" onclick={() => switchPanel("ownership")}>
            {heirNamed ? "Change the heir" : "Name an heir first"}
          </button>
          <button class="link" onclick={() => switchPanel("ownership")}>Transfer ownership</button>
        </div>
      </div>
    {/if}

    <ul class="facts">
      <li>Your copy of every message here is deleted from this device.</li>
      <li>
        {#if others > 0}
          The other {plural(others, "member")} keep theirs, and the guild keeps running.
        {:else}
          Nobody else is in this guild, so nothing survives it anywhere.
        {/if}
      </li>
      <li>Rejoining takes a new invite — leaving does not hold your place.</li>
      <li>
        If you want to keep the words, <button class="link" onclick={() => switchPanel("exportGuild")}
          >export the history</button
        > before you go.
      </li>
    </ul>

    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <!-- This page IS the question: its words, Cancel and the verb. A second
           confirm repeating the same sentence was asking twice. -->
      <button class="danger" onclick={() => leaveGuildNow(g)}>
        <Icon name={g.isOwner ? "trash" : "door"} size={14} />
        {leaveGuildLabel(g)}
      </button>
    </div>
  {/if}
</RailShell>


<style>
  .lead {
    margin: 0;
    line-height: 1.55;
  }
  /* The owner's consequence: one warning line and the two ways to answer
     it as links under it — not an amber box with buttons inside. */
  .consequence {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .doors {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-4);
    padding-left: 19px;
  }
  .doors button {
    font-size: var(--fs-small);
  }
  .facts {
    margin: 0;
    padding-left: 1.15em;
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: var(--fs-small);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: auto;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  /* --danger-fg, not --accent-fg: the light theme measures 3.64:1 for accent
     ink on this ground, which the tokens gate fails. */
  .actions .danger {
    background: var(--danger);
    color: var(--danger-fg);
  }
</style>
