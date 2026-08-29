<script>
  // Every settings page is this. It is a Modal with the rail bolted to its
  // left, and it exists so that the nine panels do not each have to know how
  // the settings surface is shaped — they say which page they are and hand
  // over their content.
  //
  // It is also what the dialogs that hang OFF a settings page use. Those pass
  // no `here`, and the rail is worked out from the trail instead: Blocked users
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
  import { S } from "../lib/state.svelte.js";
  import { railFor } from "../lib/settingsnav.js";

  let { title, onClose, here = null, wide = false, size = "", children } = $props();

  // Read once, at mount, like the entrance direction: a dialog must not grow or
  // lose a rail underneath somebody because the trail changed behind it.
  const at = here ?? railFor(S.modal, S.modalStack);
</script>

{#snippet rail()}
  <SettingsRail here={at} />
{/snippet}

{#if at}
  <Modal {title} {onClose} {rail}>
    {@render children()}
  </Modal>
{:else}
  <Modal {title} {onClose} {wide} {size}>
    {@render children()}
  </Modal>
{/if}
