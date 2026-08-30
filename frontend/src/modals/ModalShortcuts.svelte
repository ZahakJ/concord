<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S } from "../lib/state.svelte.js";
  import { SHORTCUTS, SHORTCUT_GROUPS } from "../lib/shortcuts.js";
  let { onClose } = $props();

  // Rendered from the keymap's own registry rather than from a copy of it. The
  // copy had gone nine bindings out of date — the member panel, deafen,
  // push-to-talk, the six composer formatting chords and the direction cycle,
  // and `?` itself, so the one sheet that exists to teach shortcuts could not
  // teach how to reopen itself.
  //
  // A phone has no Ctrl, no Alt and no Esc, so most of this describes chords
  // the device cannot produce — screens of them before reaching the part
  // (markdown, slash commands) that does apply. Show only what is typeable
  // there, and say so in the title.

  const ICONS = {
    Navigation: "hash",
    Reading: "eye",
    Voice: "speaker",
    Composer: "edit",
    Format: "bold",
    "Slash commands": "code",
    Markdown: "imagentext",
  };

  let q = $state("");
  const needle = $derived(q.trim().toLowerCase());

  function matches(row) {
    if (!needle) return true;
    if (row.label.toLowerCase().includes(needle)) return true;
    return row.chords.some((c) => c.join(" ").toLowerCase().includes(needle));
  }

  const shown = $derived(
    SHORTCUT_GROUPS.map((name) => ({
      name,
      icon: ICONS[name] || "spark",
      rows: SHORTCUTS.filter((s) => s.group === name && (!S.isMobile || s.typed) && matches(s)),
    })).filter((g) => g.rows.length),
  );

  // Two real columns, not CSS `columns`: that engine paints against a
  // height-capped dialog and quietly clips the second column's last cards.
  // Left holds moving-around; right holds writing. Row counts stay close.
  const LEFT = new Set(["Navigation", "Reading", "Voice", "Composer"]);
  const cols = $derived({
    left: shown.filter((g) => LEFT.has(g.name)),
    right: shown.filter((g) => !LEFT.has(g.name)),
  });

  function onFindKey(e) {
    if (e.key !== "Escape" || !q) return;
    e.stopPropagation();
    e.preventDefault();
    q = "";
  }

  // Alt+↑ and Alt+↓ are one idea. Printing them as two full chords made the
  // navigation card wrap into a second line per pair and pushed Composer off
  // the bottom of an already-tall dialog. Shared prefix, then the last keys.
  function compact(chords) {
    if (chords.length < 2) return { prefix: [], lasts: null, chords };
    const prefix = chords[0].slice(0, -1);
    if (
      prefix.length &&
      chords.every(
        (c) => c.length === prefix.length + 1 && prefix.every((p, i) => c[i] === p),
      )
    ) {
      return { prefix, lasts: chords.map((c) => c[c.length - 1]), chords };
    }
    return { prefix: [], lasts: null, chords };
  }
</script>

<Modal title={S.isMobile ? "Formatting" : "Keyboard shortcuts"} size={S.isMobile ? "" : "lg"} {onClose}>
  <div class="find">
    <span class="find-ic" aria-hidden="true"><Icon name="search" size={14} /></span>
    <input
      type="search"
      placeholder="Find a shortcut…"
      aria-label="Find a shortcut"
      bind:value={q}
      onkeydown={onFindKey}
    />
  </div>
  <div class="sc" class:empty={!shown.length} class:one={!cols.right.length || !cols.left.length}>
    {#if shown.length}
      {#each [cols.left, cols.right].filter((c) => c.length) as col, ci (ci)}
        <div class="sc-col">
          {#each col as grp (grp.name)}
            <section class="sc-group">
              <h4>
                <span class="sc-ic" aria-hidden="true"><Icon name={grp.icon} size={12} /></span>
                {grp.name}
              </h4>
              {#each grp.rows as row (row.label + row.chords[0]?.[0])}
                {@const keys = compact(row.chords)}
                <div class="sc-row">
                  <span class="sc-desc">{row.label}</span>
                  <span class="sc-keys">
                    {#if keys.lasts}
                      {#each keys.prefix as k, i (i)}
                        {#if i > 0}<span class="sc-plus">+</span>{/if}<kbd class:typed={row.typed}>{k}</kbd>
                      {/each}
                      {#if keys.prefix.length}<span class="sc-plus">+</span>{/if}
                      {#each keys.lasts as last, li (li)}
                        {#if li > 0}<span class="sc-or">or</span>{/if}<kbd class:typed={row.typed}>{last}</kbd>
                      {/each}
                    {:else}
                      <!-- Alternatives read "or"; the parts of one chord are joined by
                           "+". Printing both as "+" is what made "/shrug + /me" and
                           "Alt + \u2191" look like the same instruction. -->
                      {#each keys.chords as chord, ci2 (ci2)}
                        {#if ci2 > 0}<span class="sc-or">or</span>{/if}
                        {#each chord as k, i (i)}
                          {#if i > 0}<span class="sc-plus">+</span>{/if}<kbd class:typed={row.typed}>{k}</kbd>
                        {/each}
                      {/each}
                    {/if}
                  </span>
                </div>
              {/each}
            </section>
          {/each}
        </div>
      {/each}
    {:else}
      <p class="none muted">Nothing matches “{q.trim()}”.</p>
    {/if}
  </div>
</Modal>

<style>
  .find {
    position: relative;
    flex-shrink: 0;
  }
  .find-ic {
    position: absolute;
    left: var(--sp-3);
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-faint);
    display: grid;
    place-items: center;
    pointer-events: none;
  }
  .find input {
    width: 100%;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font-size: var(--fs-ui);
    padding: var(--sp-2) var(--sp-3) var(--sp-2) var(--sp-6);
  }
  .find input:focus {
    outline: none;
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  }
  /* Two columns of cards on a desktop. Labels used to be full sentences, so a
     second column overflowed; they are one-liners now, and a cheat sheet is
     supposed to be scannable, not a scroll of one stack. A phone stays one
     column — the sheet is already the width of the screen. */
  .sc {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--sp-2);
    align-items: start;
    max-width: 100%;
    min-height: 0;
  }
  .sc.one,
  .sc.empty {
    grid-template-columns: 1fr;
  }
  .sc-col {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    min-width: 0;
  }
  .sc-group {
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-2) var(--sp-3);
  }
  .sc-group h4 {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin: 0;
    font-size: var(--fs-tiny);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-muted);
  }
  .sc-ic {
    display: grid;
    place-items: center;
    color: var(--accent-hover);
  }
  .sc-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: 3px var(--sp-1);
    margin: 0 calc(-1 * var(--sp-1));
    font-size: var(--fs-ui);
    border-radius: var(--radius-sm);
  }
  .sc-desc {
    color: var(--text);
    min-width: 8ch;
    flex: 1 1 auto;
  }
  .sc-keys {
    flex: 0 1 auto;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    flex-wrap: wrap;
    justify-content: flex-end;
    max-width: 68%;
  }
  .sc-plus {
    color: var(--text-faint);
    font-size: var(--fs-tiny);
  }
  .sc-or {
    color: var(--text-muted);
    font-size: var(--fs-tiny);
    font-style: italic;
    padding: 0 var(--sp-1);
  }
  /* Proper keycaps: raised, subtle top-highlight, pressed-looking bottom edge. */
  kbd {
    font-family: ui-monospace, monospace;
    font-size: var(--fs-small);
    font-weight: 600;
    min-width: 22px;
    text-align: center;
    background: linear-gradient(var(--bg-3), var(--bg-2));
    border: 1px solid var(--border);
    border-bottom: 2px solid color-mix(in srgb, var(--border) 70%, black);
    border-radius: var(--radius-sm);
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, var(--text) 8%, transparent),
      0 1px 1px rgb(0 0 0 / 0.12);
    padding: 2px 7px;
    color: var(--text);
    white-space: nowrap;
  }
  /* Markdown is typed, not pressed: leave the leading and trailing spaces of
     "> " and "# " visible, or the sheet quietly teaches the wrong thing. */
  kbd.typed {
    white-space: pre;
  }
  .none {
    margin: var(--sp-4) 0 0;
    text-align: center;
    font-size: var(--fs-ui);
  }
  @media (pointer: fine) {
    .sc-row:hover {
      background: var(--bg-2);
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .sc {
      grid-template-columns: 1fr;
    }
    .find input {
      min-height: var(--tap-min);
    }
    .sc-keys {
      max-width: 100%;
    }
  }
</style>
