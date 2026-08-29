<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S } from "../lib/state.svelte.js";
  import { pickImageFile } from "../lib/pickimage.js";
  import {
    GUILD_TEMPLATES,
    EMPTY_TEMPLATE,
    templateChannelCount,
    templatePreview,
    channelGlyph,
  } from "../lib/guildtemplates.js";
  let {
    onSubmit,
    onClose,
    title = "Create a guild",
    hint = "A guild is your own end-to-end-encrypted space with channels.",
    placeholder = "Guild name",
  } = $props();
  let name = $state("");

  // This dialog is reused for categories and renames (App.svelte passes a
  // custom title) — everything below the name field only makes sense for a new
  // guild, and a rename dialog offering a channel template would be absurd.
  const isGuild = title === "Create a guild";

  let icon = $state("");
  let description = $state("");
  let template = $state(EMPTY_TEMPLATE);
  let iconError = $state("");

  // Mirror the rail's initials logic so the preview matches what everyone
  // will actually see.
  const initials = $derived(
    name
      .trim()
      .split(/\s+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2)
      .toUpperCase(),
  );

  function submit() {
    if (!name.trim()) return;
    // The category/rename callers still take a bare string: one dialog, two
    // contracts, and the object shape is the one the guild path wants.
    if (!isGuild) return onSubmit(name);
    onSubmit({ name: name.trim(), icon, description: description.trim(), template });
  }
</script>

<Modal {title} {onClose} wide={isGuild}>
  {#if isGuild}
    <!-- Live preview: the guild's rail bubble takes shape as you type, and is
         the icon control. One target, not a bubble beside a button — the
         thing that shows you the icon is the thing that changes it. -->
    <div class="hero">
      <button
        type="button"
        class="bubble"
        class:named={!!initials || !!icon}
        class:hasicon={!!icon}
        aria-label={icon ? "Change guild icon" : "Choose a guild icon"}
        onclick={() => pickImageFile((v) => ((icon = v), (iconError = "")), (m) => (iconError = m))}
      >
        {#if icon}
          <img src={icon} alt="" />
        {:else if initials}
          {initials}
        {:else}
          <Icon name="spark" size={22} />
        {/if}
        <span class="cam" aria-hidden="true"><Icon name="camera" size={13} /></span>
      </button>
      <span class="hero-text">
        <span class="bubble-name" class:ph={!name.trim()}>{name.trim() || "Your new space"}</span>
        <span class="hero-sub">{hint}</span>
        {#if icon}
          <button type="button" class="linky" onclick={() => (icon = "")}>Remove icon</button>
        {/if}
        {#if iconError}
          <span class="err" role="status">{iconError}</span>
        {/if}
      </span>
    </div>
  {:else}
    <div class="hero">
      <span class="bubble static"><Icon name="spark" size={22} /></span>
    </div>
  {/if}

  {#if !isGuild}
    <p class="muted">{hint}</p>
  {/if}
  <!-- Not on a phone: the soft keyboard opens while the sheet is still running
       its 0.28s slide-up, so the sheet's max-height recomputes mid-animation
       and the panel visibly jumps. The field is one tap away. -->
  <input
    {placeholder}
    aria-label={placeholder}
    bind:value={name}
    autofocus={!S.isMobile}
    maxlength="48"
    onkeydown={(e) => e.key === "Enter" && name.trim() && submit()}
  />

  {#if isGuild}
    <label class="fld">
      <span class="lbl">Description <span class="opt">optional</span></span>
      <textarea
        bind:value={description}
        maxlength="200"
        rows="2"
        placeholder="What's this space for?"
      ></textarea>
    </label>

    <div class="fld">
      <span class="lbl">Start with</span>
      <!-- Each tile DRAWS the sidebar it would build, at a sixth the size. It
           is the app's own furniture, so it needs no explaining, and it is the
           one thing about a starter layout you can actually check. The tiles
           are a fixed height for the same reason: four sentences of four
           lengths used to make four tiles of four heights out of one grid. -->
      <div class="tpl-grid" role="radiogroup" aria-label="Starter layout">
        {#each GUILD_TEMPLATES as t (t.id)}
          <button
            type="button"
            class="tpl"
            class:sel={template === t.id}
            role="radio"
            aria-checked={template === t.id}
            onclick={() => (template = t.id)}
          >
            <span class="mini" aria-hidden="true">
              {#each templatePreview(t) as grp, gi (gi)}
                {#if grp.category}
                  <span class="mini-cat">{grp.category}</span>
                {/if}
                {#each grp.channels as ch (ch.name)}
                  <span class="mini-row">
                    <Icon name={channelGlyph(ch.type)} size={9} />
                    <span class="mini-name">{ch.name}</span>
                  </span>
                {/each}
              {/each}
              {#if !t.plan.length}
                <!-- Room, drawn as room. A tile whose whole point is that it
                     builds nothing was otherwise a labelled empty box, which
                     reads as a tile that failed to load rather than as a
                     choice. -->
                {#each [0, 1, 2] as i (i)}
                  <span class="mini-row ghost"></span>
                {/each}
              {/if}
            </span>
            <span class="tpl-foot">
              <strong>{t.name}</strong>
              <span class="tpl-count">
                {#if t.plan.length}
                  {templateChannelCount(t) + 1} channels · {t.plan.length} categor{t.plan.length ===
                  1
                    ? "y"
                    : "ies"}
                {:else}
                  One channel, and a clean slate
                {/if}
              </span>
            </span>
            <span class="tpl-mark" aria-hidden="true"><Icon name="check" size={11} /></span>
          </button>
        {/each}
      </div>
      <!-- Say the thing that stops a template feeling like a commitment. -->
      <p class="muted tiny">Everything a template makes can be renamed or deleted afterwards.</p>
    </div>
  {/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={submit} disabled={!name.trim()}>Create</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  /* A row, not a column. Centred it stacked bubble over name over sentence and
     cost 175px of a dialog that already scrolls; beside each other they read as
     one statement — this is the thing you are making, and here is what a guild
     is — in about half the height. */
  .hero {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 2px 0 4px;
  }
  .hero-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
    align-items: flex-start;
  }
  .hero-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .bubble {
    position: relative;
    flex: none;
    width: 60px;
    height: 60px;
    padding: 0;
    border-radius: 20px;
    display: grid;
    place-items: center;
    font-size: var(--fs-display);
    font-weight: 700;
    color: var(--accent-hover);
    background: var(--accent-soft);
    border: 1px dashed color-mix(in srgb, var(--accent) 45%, transparent);
    animation: bubble-in 0.35s var(--ease-spring);
    transition:
      background 0.25s ease,
      color 0.25s ease,
      border-color 0.25s ease,
      box-shadow 0.25s ease;
  }
  .bubble.static {
    cursor: default;
  }
  .bubble img {
    width: 100%;
    height: 100%;
    border-radius: inherit;
    object-fit: cover;
  }
  /* The moment it has a name, the placeholder solidifies into a real bubble. */
  .bubble.named {
    color: var(--accent-fg);
    background: linear-gradient(135deg, var(--accent), color-mix(in srgb, var(--accent) 70%, var(--accent-hover)));
    border: 1px solid transparent;
    box-shadow: var(--accent-glow);
  }
  .bubble.hasicon {
    background: var(--bg-3);
  }
  /* A visible affordance, not a hover-only reveal: the control has to say it
     is a control before the pointer is on it (and a finger never hovers). */
  .cam {
    position: absolute;
    right: -3px;
    bottom: -3px;
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .bubble.static .cam {
    display: none;
  }
  .bubble:hover .cam {
    color: var(--text);
    border-color: var(--accent);
  }
  .bubble-name {
    font-size: var(--fs-title);
    font-weight: 700;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bubble-name.ph {
    color: var(--text-faint);
    font-weight: 500;
  }
  .linky {
    background: none;
    border: 0;
    padding: 0;
    font-size: var(--fs-small);
    color: var(--text-muted);
    text-decoration: underline;
  }
  .err {
    font-size: var(--fs-small);
    color: var(--danger-text);
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lbl {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .opt {
    font-weight: 500;
    color: var(--text-faint);
  }
  textarea {
    resize: vertical;
    min-height: 48px;
  }
  .tpl-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--sp-2);
  }
  .tpl {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 0;
    padding: 0;
    overflow: hidden;
    text-align: left;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    transition:
      border-color var(--dur-quick) ease,
      box-shadow var(--dur-quick) ease,
      transform var(--dur-quick) var(--ease-out);
  }
  .tpl:hover {
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
    transform: translateY(-1px);
  }
  /* Ring as a box-shadow, not a fatter border — a border that grows on
     selection re-lays the grid out and the tiles twitch. (Same reason
     ModalCreateChannel's type tiles do it.) */
  .tpl.sel {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }
  /* ---- the miniature sidebar ---------------------------------------------
     A fixed height, so the grid is a grid. Anything past it fades out rather
     than being cut through a row, which is the same scroll-fade the app uses
     everywhere else. */
  .mini {
    display: flex;
    flex-direction: column;
    gap: 2px;
    height: 100px;
    padding: 9px 10px 0;
    overflow: hidden;
    background: var(--bg-0);
    border-bottom: 1px solid var(--border);
    /* The layouts are longer than the tile on purpose — the count underneath
       says how much longer. Fading from two-thirds reads as "there is more",
       where a hard edge reads as a row sliced in half. */
    -webkit-mask-image: linear-gradient(#000 62%, transparent);
    mask-image: linear-gradient(#000 62%, transparent);
  }
  .mini-cat {
    margin-top: 3px;
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .mini-cat:first-child {
    margin-top: 0;
  }
  .mini-row {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 2px 4px;
    border-radius: 3px;
    font-size: 9.5px;
    line-height: 1.2;
    color: var(--text-muted);
  }
  /* The first row is #general, which every guild is born with — drawn as the
     one that is open, because that is where a new owner lands. */
  .mini-row:first-child {
    background: var(--bg-3);
    color: var(--text);
    font-weight: 600;
  }
  .mini-row.ghost {
    height: 14px;
    margin: 1px 0;
    border-radius: 3px;
    border: 1px dashed color-mix(in srgb, var(--border) 90%, transparent);
  }
  .mini-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tpl.sel .mini {
    background: color-mix(in srgb, var(--accent) 7%, var(--bg-0));
  }
  .tpl-foot {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 8px 10px 9px;
  }
  .tpl-foot strong {
    font-size: var(--fs-ui);
    font-weight: 650;
  }
  .tpl.sel .tpl-foot strong {
    color: var(--accent-hover);
  }
  .tpl-count {
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  /* The tick, which is the whole of the selected state's chrome. It grows in
     rather than appearing, so choosing feels like something happened. */
  .tpl-mark {
    position: absolute;
    top: 7px;
    right: 7px;
    width: 19px;
    height: 19px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-fg);
    /* A ring in the preview's own ground, so the tick reads as sitting ON the
       miniature rather than as one more row in it. */
    box-shadow: 0 0 0 2px var(--bg-0);
    transform: scale(0);
    transition: transform var(--dur-quick) var(--ease-spring);
  }
  .tpl.sel .tpl-mark {
    transform: scale(1);
  }
  @keyframes bubble-in {
    from {
      opacity: 0;
      transform: scale(0.6);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bubble,
    .tpl,
    .tpl-mark {
      animation: none;
      transition: none;
    }
  }
</style>
