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
    background: color-mix(in srgb, var(--bg-0) 55%, var(--bg-elevated));
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
