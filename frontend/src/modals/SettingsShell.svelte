<script>
  // Every settings page is this. It is a Modal with the rail bolted to its
  // left, and it exists so that the nine panels do not each have to know how
  // the settings surface is shaped — they say which page they are and hand
  // over their content.
  //
  // On a phone there is no rail (there is no room for one, and the drill-down
  // list is the right shape for a thumb), so this degrades to exactly the
  // dialog it was before: Modal ignores the rail when S.isMobile, the panel
  // keeps its ‹, and nothing else changes.
  import Modal from "./Modal.svelte";
  import SettingsRail from "./SettingsRail.svelte";

  let { title, onClose, here = "", children } = $props();
</script>

{#snippet rail()}
  <SettingsRail {here} />
{/snippet}

<Modal {title} {onClose} {rail}>
  {@render children()}
</Modal>
