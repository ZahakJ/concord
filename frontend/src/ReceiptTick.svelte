<script>
  // The tick under one of your own messages in a DM: one check once it has been
  // received and decrypted on the other side, two (in the accent colour) once it
  // has been read. Quiet by design; the title and aria-label say it in words.
  import { R } from "./lib/receipts.svelte.js";
  import { tickOf, tickLabel, partialLabel } from "./lib/receipts.js";

  let { id } = $props();
  const view = $derived(R.byMsg[id]);
  const kind = $derived(tickOf(view, R.readOn));
  const label = $derived(tickLabel(view, kind));
  const partial = $derived(kind ? "" : partialLabel(view, R.readOn));
</script>

{#if kind}
  <span class="rt" class:read={kind === "read"} role="img" title={label} aria-label={label}>
    <svg viewBox="0 0 18 12" width="14" height="10" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M1.5 6.5 5 10 12 2" />
      {#if kind === "read"}<path d="M7 9.2 8 10 16.5 2" />{/if}
    </svg>
  </span>
{:else if partial}
  <span class="rt part" title={partial} aria-label={partial} role="img">
    <svg viewBox="0 0 18 12" width="14" height="10" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M1.5 6.5 5 10 12 2" opacity="0.45" />
    </svg>
  </span>
{/if}

<style>
  .rt {
    display: inline-flex;
    align-items: center;
    margin-left: 6px;
    vertical-align: baseline;
    color: var(--text-faint);
    user-select: none;
  }
  .rt.read {
    color: var(--accent);
  }
</style>
