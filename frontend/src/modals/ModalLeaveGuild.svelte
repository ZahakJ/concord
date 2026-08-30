<script>
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
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "../Icon.svelte";
  import {
    S,
    activeGuild,
    leaveGuildCopy,
    leaveGuildLabel,
    leaveGuildNow,
    switchPanel,
  } from "../lib/state.svelte.js";
  import { plural } from "../lib/plural.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  // Raised INSIDE the page rather than by swapping S.modal for it: a page that
  // replaces itself with its own question cannot be cancelled back to.
  let asking = $state(false);
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
        <p>
          <Icon name="crown" size={14} />
          <span>
            You own this guild.
            {#if heirNamed}
              You have named an heir, so somebody can take it over after you go — but until they
              do, nobody can add or remove members.
            {:else}
              <strong>Nobody will be able to add or remove members after you go</strong>, and there
              is no way to appoint anyone once you are gone.
            {/if}
          </span>
        </p>
        <div class="doors">
          <button class="ghost" onclick={() => switchPanel("ownership")}>
            <Icon name="crown" size={13} />
            {heirNamed ? "Change the heir" : "Name an heir first"}
          </button>
          <button class="ghost" onclick={() => switchPanel("ownership")}>
            <Icon name="members" size={13} /> Transfer ownership
          </button>
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
        If you want to keep the words, <button class="linkish" onclick={() => switchPanel("exportGuild")}
          >export the history</button
        > before you go.
      </li>
    </ul>

    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button class="danger" onclick={() => (asking = true)}>
        <Icon name={g.isOwner ? "trash" : "door"} size={14} />
        {leaveGuildLabel(g)}
      </button>
    </div>
  {/if}
</RailShell>

{#if asking && g}
  {@const copy = leaveGuildCopy(g)}
  <ConfirmDialog
    title={copy.title}
    body={copy.body}
    confirmLabel={copy.confirmLabel}
    onConfirm={() => leaveGuildNow(g)}
    onClose={() => (asking = false)}
  />
{/if}

<style>
  .lead {
    margin: 0;
    line-height: 1.55;
  }
  .consequence {
    border: 1px solid color-mix(in srgb, var(--warn) 34%, transparent);
    background: color-mix(in srgb, var(--warn) 10%, transparent);
    border-radius: var(--radius-md);
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .consequence p {
    display: flex;
    gap: var(--sp-2);
    align-items: flex-start;
    margin: 0;
    line-height: 1.5;
    font-size: var(--fs-small);
    color: var(--warn-text);
  }
  .consequence p :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .doors {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .doors button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
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
  .linkish {
    background: none;
    border: 0;
    padding: 0;
    font: inherit;
    color: var(--accent-hover);
    text-decoration: underline;
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
    box-shadow: 0 0 12px color-mix(in srgb, var(--danger) 35%, transparent);
  }
</style>
