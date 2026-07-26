<script>
  // A titled card of SettingRows. `note` is the place for the paragraph a row
  // shouldn't carry: one explanation for the group, under it, where it can be
  // read once and ignored after.
  let { label = "", note = "", children } = $props();
</script>

<section class="grp">
  {#if label}<div class="sec-label">{label}</div>{/if}
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
    animation-delay: 0.12s;
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
    font-size: 11px;
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
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-muted);
  }
</style>
