<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  let {
    onSubmit,
    onClose,
    title = "Create a guild",
    hint = "A guild is your own end-to-end-encrypted space with channels.",
    placeholder = "Guild name",
  } = $props();
  let name = $state("");

  // This dialog is reused for categories and renames (App.svelte passes a
  // custom title) — the bubble preview only makes sense for a new guild.
  const showHero = title === "Create a guild";

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
</script>

<Modal {title} {onClose}>
  {#if showHero}
    <!-- Live preview: the guild's rail bubble takes shape as you type. -->
    <div class="hero">
      <span class="bubble" class:named={!!initials}>
        {#if initials}
          {initials}
        {:else}
          <Icon name="spark" size={22} />
        {/if}
      </span>
      <span class="bubble-name" class:ph={!name.trim()}>{name.trim() || "Your new space"}</span>
    </div>
  {/if}

  <p class="muted">{hint}</p>
  <input
    {placeholder}
    bind:value={name}
    autofocus
    maxlength="48"
    onkeydown={(e) => e.key === "Enter" && name.trim() && onSubmit(name)}
  />
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onSubmit(name)} disabled={!name.trim()}>Create</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
  }
  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 6px 0 2px;
  }
  .bubble {
    width: 64px;
    height: 64px;
    border-radius: 20px;
    display: grid;
    place-items: center;
    font-size: 22px;
    font-weight: 700;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px dashed color-mix(in srgb, var(--accent) 45%, transparent);
    animation: bubble-in 0.35s cubic-bezier(0.34, 1.5, 0.5, 1);
    transition:
      background 0.25s ease,
      color 0.25s ease,
      border-color 0.25s ease,
      box-shadow 0.25s ease;
  }
  /* The moment it has a name, the placeholder solidifies into a real bubble. */
  .bubble.named {
    color: #fff;
    background: linear-gradient(135deg, var(--accent), color-mix(in srgb, var(--accent) 70%, var(--accent-hover)));
    border: 1px solid transparent;
    box-shadow: var(--accent-glow);
  }
  .bubble-name {
    font-size: 13px;
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
  @keyframes bubble-in {
    from {
      opacity: 0;
      transform: scale(0.6);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bubble {
      animation: none;
      transition: none;
    }
  }
</style>
