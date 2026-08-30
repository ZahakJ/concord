<script>
  // Every railed page is this. It is a Modal with a rail bolted to its left,
  // and it exists so that the pages do not each have to know how the surface is
  // shaped — they say which page they are and hand over their content.
  //
  // There are two rails now and one shell, which is why this is no longer
  // called SettingsShell. Settings was rebuilt first; the guild hub was the
  // same dialog with the same fault (a box that swung 380↔460 wide and 168↔810
  // tall between panels, drilled into one row at a time) and it gets the same
  // answer. The two tables — lib/settingsnav.js and lib/guildnav.js — are the
  // only difference between them.
  //
  // It is also what the dialogs that hang OFF a railed page use. Those pass no
  // `here`, and the rail is worked out from the trail instead: Blocked users
  // reached from Privacy keeps the rail and the one constant box, and the same
  // dialog reached from a DM row is just a dialog. Without that, the first
  // thing anybody clicks on the Account page snaps the surface from 1000x660
  // down to 460 — the exact resize this was built to stop.
  //
  // On a phone there is no rail (there is no room for one, and the drill-down
  // list is the right shape for a thumb), so this degrades to exactly the
  // dialog it was before: Modal ignores the rail when S.isMobile, the panel
  // keeps its ‹, and nothing else changes.
  import Modal from "./Modal.svelte";
  import SettingsRail from "./SettingsRail.svelte";
  import HubRail from "./HubRail.svelte";
  import { S } from "../lib/state.svelte.js";
  import { railFor } from "../lib/settingsnav.js";
  import { guildRailFor, onGuildTrail } from "../lib/guildnav.js";

  let { title, onClose, here = null, wide = false, size = "", children } = $props();

  // Read once, at mount, like the entrance direction: a dialog must not grow or
  // lose a rail underneath somebody because the trail changed behind it.
  //
  // The guild trail is asked FIRST and answers with a stamp rather than a kind
  // list, because most guild panels are reachable from somewhere that is not
  // the hub (Events from the header, Roles from the member panel, Stats from a
  // keyboard shortcut) and those must stay ordinary dialogs. Settings pages
  // name themselves with `here`, which is why that wins over either trail.
  const hub = onGuildTrail(S.modal, S.modalStack);
  const hubAt = hub ? guildRailFor(S.modal, S.modalStack) : "";
  const at = hub ? "" : (here ?? railFor(S.modal, S.modalStack));
</script>

{#snippet rail()}
  {#if hub}
    <HubRail here={hubAt} />
  {:else}
    <SettingsRail here={at} />
  {/if}
{/snippet}

{#if hub || at}
  <Modal {title} {onClose} {rail}>
    {@render children()}
  </Modal>
{:else}
  <Modal {title} {onClose} {wide} {size}>
    {@render children()}
  </Modal>
{/if}
