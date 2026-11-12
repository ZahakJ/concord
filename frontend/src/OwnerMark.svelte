<script>
  // "You own this": a small filled crown, and the only owner mark there is.
  //
  // Ownership is a fact about a place, not something waiting for you, so it is
  // never a ring, a dot or a count around a tile: those are state (current,
  // unread, moments) and a ring around a tile you own was read as one. It sits
  // beside a name — a switcher row, the place header, a member, a group in the
  // message list — and in the corner of the Rail layout's larger tiles. The
  // 28px strip tiles carry none: at that size any mark reads as a
  // notification, and the tile's description says it instead.
  //
  // `label` is the accessible name ("you own this guild", "Guild owner");
  // pass `decorative` where the control it sits on already says it.
  import Icon from "./Icon.svelte";
  let { size = 12, label = "You own this guild", decorative = false } = $props();
</script>

{#if decorative}
  <span class="owner-mark" aria-hidden="true"><Icon name="crownFilled" {size} /></span>
{:else}
  <span class="owner-mark" role="img" aria-label={label} title={label}><Icon name="crownFilled" {size} /></span>
{/if}

<style>
  .owner-mark {
    flex: none;
    display: inline-grid;
    place-items: center;
    color: var(--text-muted);
    line-height: 0;
  }
</style>
