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
  /* A group is a label and its rows — no box around them. Groups are told
     apart by the label and --sp-5 of air; rows by a hairline. */
  .grp {
    display: flex;
    flex-direction: column;
    text-align: start;
    margin-top: var(--sp-5);
  }
  /* The first group on a page sits right under the page's own header (the
     dialog's .sheet-top is its preceding sibling, so :first-child alone
     never matched inside a dialog). */
  .grp:first-child,
  :global(.sheet-top) + .grp {
    margin-top: 0;
  }
  .sec-label {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    font-family: var(--display-font);
    letter-spacing: 0.01em;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
  }
  .card {
    display: flex;
    flex-direction: column;
  }
  .note {
    margin: var(--sp-1) 0 0;
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text-muted);
  }
</style>
