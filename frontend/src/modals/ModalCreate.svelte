<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S } from "../lib/state.svelte.js";
  import { pickImageFile } from "../lib/pickimage.js";
  import { GUILD_TEMPLATES, EMPTY_TEMPLATE, templateChannelCount } from "../lib/guildtemplates.js";
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

<Modal {title} {onClose}>
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
      <span class="bubble-name" class:ph={!name.trim()}>{name.trim() || "Your new space"}</span>
      {#if icon}
        <button type="button" class="linky" onclick={() => (icon = "")}>Remove icon</button>
      {/if}
      {#if iconError}
        <span class="err" role="status">{iconError}</span>
      {/if}
    </div>
  {:else}
    <div class="hero">
      <span class="bubble static"><Icon name="spark" size={22} /></span>
    </div>
  {/if}

  <p class="muted">{hint}</p>
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
            <span class="tpl-top">
              <Icon name={t.icon} size={15} />
              <strong>{t.name}</strong>
            </span>
            <span class="tpl-blurb">{t.blurb}</span>
            {#if t.plan.length}
              <span class="tpl-count"
                >{templateChannelCount(t)} channels · {t.plan.length} categor{t.plan.length === 1
                  ? "y"
                  : "ies"}</span
              >
            {/if}
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
  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: 6px 0 2px;
  }
  .bubble {
    position: relative;
    width: 64px;
    height: 64px;
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
    font-size: var(--fs-ui);
    font-weight: 600;
    max-width: 240px;
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
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--sp-1);
    padding: 10px 12px;
    text-align: left;
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text-muted);
  }
  .tpl:hover {
    background: var(--bg-2);
    color: var(--text);
  }
  /* Ring as a box-shadow, not a fatter border — a border that grows on
     selection re-lays the grid out and the tiles twitch. (Same reason
     ModalCreateChannel's type tiles do it.) */
  .tpl.sel {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .tpl-top {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-ui);
  }
  .tpl-top strong {
    font-weight: 600;
  }
  .tpl-blurb {
    font-size: var(--fs-small);
    line-height: 1.35;
    color: var(--text-muted);
  }
  .tpl.sel .tpl-blurb {
    color: inherit;
  }
  .tpl-count {
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  @keyframes bubble-in {
    from {
      opacity: 0;
      transform: scale(0.6);
    }
  }
  @media (max-width: 460px) {
    .tpl-grid {
      grid-template-columns: 1fr;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bubble {
      animation: none;
      transition: none;
    }
  }
</style>
