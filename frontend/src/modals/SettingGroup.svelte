<script>
  // A titled card of SettingRows.
  //
  // `info` is the preferred way to explain a group: a dot beside the heading
  // that opens the paragraph on demand. `note` still prints one underneath and
  // is for the rare case where the text must be read rather than sought out —
  // a page of permanent paragraphs is what made these panels a wall.
  import InfoDot from "./InfoDot.svelte";
  let { label = "", note = "", info = "", children } = $props();
</script>

<section class="grp">
  {#if label}
    <div class="sec-label">
      {label}{#if info}<InfoDot text={info} label="About {label}" />{/if}
    </div>
  {/if}
  <div class="card">{@render children()}</div>
  {#if note}<p class="note">{note}</p>{/if}
</section>

<style>
  .grp {
    display: flex;
    flex-direction: column;
    gap: 7px;
    text-align: left;
    /* Groups arrive a beat apart so a panel resolves top-down instead of
       landing as one slab. */
    animation: grp-in 0.28s ease both;
  }
  .grp:nth-child(2) {
    animation-delay: 0.04s;
  }
  .grp:nth-child(3) {
    animation-delay: 0.08s;
  }
  .grp:nth-child(4) {
    animation-delay: var(--dur-quick);
  }
  @keyframes grp-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .grp {
      animation: none;
    }
  }
  .sec-label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding-left: 2px;
  }
  .card {
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .note {
    margin: 0;
    padding: 0 2px;
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text-muted);
  }
</style>
