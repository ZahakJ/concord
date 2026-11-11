<script>
  // The guild hub, on a phone.
  //
  // It used to be both surfaces. On a desktop it was sixteen chevron rows in
  // one scrolling column with a hero on top, drilled into one at a time, and
  // the dialog under the cursor resized on every navigation — that is the rail
  // now (lib/guildnav.js, HubRail.svelte), landing straight on Overview,
  // because a list of sixteen doors in front of a rail that already shows all
  // sixteen is a menu in front of a menu.
  //
  // On a 390px sheet there is no room for a rail and no need for one: a sheet
  // you can flick through IS the overview. So the list stays, and it is built
  // from the same table the rail reads, which is what stops the two drifting
  // apart the way the header menu, the phone sheet and this dialog used to.
  import { onMount } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, openPanel, openGuildHub } from "../lib/state.svelte.js";
  import { guildGroupsFor, guildField } from "../lib/guildnav.js";
  import { guildInitials } from "../lib/rail.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const groups = $derived(guildGroupsFor(g));

  // A desktop that lands here (anything that sets the kind directly rather
  // than through openGuildHub) gets the rail it would have got through the
  // front door: there is one guild-settings surface on a desktop, not two.
  onMount(() => {
    if (!S.isMobile) openGuildHub();
  });
</script>

<!-- A page, not a sheet: Guild settings is somewhere you go and come back
     from, like Settings, so on a phone it takes the screen (Modal draws a
     railed dialog as a page there; the rail itself has no room and is not
     drawn). -->
{#snippet noRail()}{/snippet}

{#if S.isMobile}
  <Modal title="Guild settings" rail={noRail} {onClose}>
    {#if g}
      <!-- Which guild these are. The identity row the desktop rail opens with,
           not a banner: art in a settings page is decoration in a place you
           came to change something. -->
      <div class="who">
        {#if g.icon}
          <img class="who-icon" src={g.icon} alt="" />
        {:else}
          <span class="who-mark">{guildInitials(g.name || "")}</span>
        {/if}
        <strong class="who-name">{g.name}</strong>
      </div>

      {#each groups as grp (grp.label)}
        <section class="grp" aria-label={grp.label}>
          <div class="sec-label">{grp.label}</div>
          {#each grp.items as it (it.kind)}
            <button class="row" class:danger={grp.danger} onclick={() => openPanel(it.kind, "guildHub")}>
              <span class="ico"><Icon name={guildField(it.icon, g, S)} size={20} /></span>
              <span class="row-text">
                <span class="row-title">{guildField(it.title, g, S)}</span>
                <span class="row-sub">{guildField(it.sub, g, S)}</span>
              </span>
              <Icon name="chevron" size={16} />
            </button>
          {/each}
        </section>
      {/each}
    {/if}
  </Modal>
{/if}

<style>
  /* The phone settings list: groups under a sentence-case label, 56px rows
     with a muted 20px icon, a title and one line saying what is behind it,
     hairlines between rows — no boxed cards and no tinted icon tiles. */
  .who {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    min-width: 0;
    padding: var(--sp-1) 0 var(--sp-2);
  }
  .who-icon,
  .who-mark {
    width: 40px;
    height: 40px;
    flex: none;
    border-radius: var(--radius-md);
    object-fit: cover;
  }
  .who-mark {
    display: grid;
    place-items: center;
    font-size: var(--fs-ui);
    font-weight: 600;
    background: var(--bg-3);
    color: var(--text);
  }
  .who-name {
    min-width: 0;
    font-size: var(--fs-title);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .grp {
    display: flex;
    flex-direction: column;
  }
  .sec-label {
    padding: var(--sp-3) 0 var(--sp-1);
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    min-height: 56px;
    padding: var(--sp-2) 0;
    background: transparent;
    color: var(--text-faint);
    text-align: start;
    border-radius: 0;
  }
  .row + .row {
    border-top: 1px solid var(--hairline);
  }
  .row:active {
    background: color-mix(in srgb, var(--bg-3) 60%, transparent);
  }
  .ico {
    display: grid;
    place-items: center;
    flex: none;
    color: var(--text-muted);
  }
  .row-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .row-title {
    font-size: var(--fs-ui);
    font-weight: 500;
    color: var(--text);
  }
  .row-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .row.danger .row-title,
  .row.danger .ico {
    color: var(--danger-text);
  }
</style>
