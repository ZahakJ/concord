<script>
  // The "(edited)" mark, as a button: opens the versions this message has held,
  // oldest first, with when each took effect. History comes from this device's
  // own copy, so it only ever shows text this member already received.
  import { api } from "./lib/api.js";
  import { clockOpts } from "./lib/state.svelte.js";
  import { shortWhen, sealFull } from "./lib/timestamp.js";
  import { rectOf, viewport } from "./lib/place.js";

  let { channelId, messageId, block = false } = $props();

  let open = $state(false);
  let versions = $state([]);
  let failed = $state(false);
  let root = $state(null);
  let btn = $state(null);

  async function load() {
    try {
      versions = (await api.messageVersions(channelId, messageId)) || [];
      failed = false;
    } catch {
      failed = true;
    }
  }
  function toggle(e) {
    e.stopPropagation();
    open = !open;
    if (open) load();
  }
  function close(refocus) {
    open = false;
    if (refocus) btn?.focus();
  }
  function onKey(e) {
    if (e.key === "Escape") {
      e.stopPropagation();
      close(true);
    }
  }
  // Closes on any press outside, since on a phone there is no pointer leaving.
  $effect(() => {
    if (!open) return;
    const away = (e) => {
      if (root && !root.contains(e.target)) open = false;
    };
    document.addEventListener("pointerdown", away, true);
    return () => document.removeEventListener("pointerdown", away, true);
  });
  // Keep the card on screen, flipping below the mark when there is no room above.
  function clamp(node) {
    const pad = 8;
    const r = rectOf(node);
    const vp = viewport();
    let dx = 0;
    if (r.x < pad) dx = pad - r.x;
    else if (r.x + r.w > vp.w - pad) dx = vp.w - pad - (r.x + r.w);
    if (dx) node.style.transform = `translateX(${dx}px)`;
    if (r.y < 90) {
      node.style.bottom = "auto";
      node.style.top = "calc(100% + 6px)";
    }
  }
</script>

<span class="eh" class:block bind:this={root} onkeydown={onKey} role="presentation">
  <button
    type="button"
    class="eh-btn"
    bind:this={btn}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={toggle}>(edited)</button
  >
  {#if open}
    <div class="eh-card" role="dialog" aria-label="Edit history" use:clamp>
      <div class="eh-h">Edit history</div>
      {#if failed}
        <div class="eh-note">Couldn't load the history.</div>
      {:else if versions.length < 2}
        <div class="eh-note">
          This device only has the current version. Earlier ones are kept only
          from edits received here.
        </div>
      {:else}
        <ol class="eh-list">
          {#each versions as v, i (i)}
            <li class="eh-v">
              <span
                class="eh-when"
                title={sealFull(Date.parse(v.at), clockOpts())}
              >
                {i === 0 ? "Original" : i === versions.length - 1 ? "Current" : "Edit"}
                · {shortWhen(Date.parse(v.at), clockOpts())}
              </span>
              <span class="eh-text">{v.content}</span>
            </li>
          {/each}
        </ol>
      {/if}
    </div>
  {/if}
</span>

<style>
  .eh {
    position: relative;
    display: inline;
  }
  .eh.block {
    display: block;
  }
  /* The tag keeps its global look; a button just needs the chrome removed. */
  .eh-btn {
    background: none;
    border: 0;
    padding: 0;
    margin-left: 5px;
    font: inherit;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    cursor: pointer;
    user-select: none;
  }
  .eh.block .eh-btn {
    margin-left: 0;
    margin-top: 2px;
  }
  .eh-btn:hover,
  .eh-btn[aria-expanded="true"] {
    color: var(--text-muted);
    text-decoration: underline;
  }
  .eh-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: 3px;
  }
  .eh-card {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: max-content;
    min-width: 200px;
    max-width: min(360px, 80vw);
    max-height: 50vh;
    overflow-y: auto;
    padding: 8px 10px;
    border-radius: var(--radius-md);
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
    color: var(--text);
    text-align: left;
    font-style: normal;
    white-space: normal;
  }
  .eh-h {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .eh-note {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .eh-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .eh-v {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .eh-when {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .eh-text {
    font-size: var(--fs-compact);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .eh-card {
      left: auto;
      right: 0;
    }
  }
</style>
