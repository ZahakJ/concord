<script>
  // The decoration picker: things WORN on your avatar, as against the frame
  // that surrounds it. Separate doors because they are separate choices — you
  // wear ears AND a runic ring, and a single list mixing the two would hide
  // that they stack.
  //
  // Every tile draws YOUR avatar with the decoration on it, in your own
  // colours, because the whole point of drawing these as paths rather than
  // shipping images is that they adopt the wearer. A generic swatch would sell
  // that short.
  import { registerOverlay } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { DECORATION_BY_ID, DECORATION_GROUPS } from "./lib/decorations.js";

  let {
    decoration = "",
    color = "#14a394",
    color2 = "",
    avatar = "",
    emoji = "",
    name = "You",
    onApply,
    onClose,
  } = $props();
  $effect(() => registerOverlay(() => onClose?.()));

  let sel = $state(decoration);
</script>

<div class="ds-scrim" role="presentation" onclick={onClose}></div>
<div class="ds" role="dialog" aria-label="Choose an avatar decoration">
  <div class="ds-head">
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="chevron" size={16} /></button>
    <strong>Avatar decoration</strong>
    <span class="tiny muted">{DECORATION_BY_ID[sel]?.name || "None"}</span>
  </div>

  <div class="preview">
    <Avatar {name} {emoji} {color} image={avatar} size={84} decoration={sel} {color2} />
    <p class="tiny muted">
      Worn over whatever frame you pick — the two stack. Drawn, so it takes your
      colours and stays sharp at any size.
    </p>
  </div>

  <div class="library">
    <button class="opt none" class:sel={sel === ""} onclick={() => (sel = "")}>
      <span class="none-dot"></span>
      <span class="oname">None</span>
    </button>
    {#each DECORATION_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid">
        {#each g.ids as id (id)}
          <button
            class="opt"
            class:sel={sel === id}
            onclick={() => (sel = id)}
            title={DECORATION_BY_ID[id]?.name}
          >
            <Avatar {name} {emoji} {color} image={avatar} size={36} decoration={id} {color2} />
            <span class="oname">{DECORATION_BY_ID[id]?.name}</span>
          </button>
        {/each}
      </div>
    {/each}
  </div>

  <div class="ds-foot">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onApply({ decoration: sel })}>Apply</button>
  </div>
</div>

<style>
  .ds-scrim {
    position: fixed;
    inset: 0;
    background: #0006;
    z-index: 60;
  }
  .ds {
    position: fixed;
    inset: 50% auto auto 50%;
    transform: translate(-50%, -50%);
    width: min(560px, 94vw);
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    z-index: 61;
    overflow: hidden;
  }
  .ds-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
  }
  .ds-head .tiny {
    margin-left: auto;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 18px 16px;
    border-bottom: 1px solid var(--border);
  }
  .preview p {
    margin: 0;
    line-height: 1.5;
  }
  .library {
    overflow-y: auto;
    padding: 12px 14px 4px;
  }
  .gtitle {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    margin: 14px 0 8px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(74px, 1fr));
    gap: 8px;
  }
  .opt {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 10px 4px 8px;
    background: var(--bg-1);
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  .opt:hover {
    border-color: var(--border);
  }
  .opt.sel {
    border-color: var(--accent);
  }
  .opt.none {
    flex-direction: row;
    justify-content: center;
    gap: 8px;
    width: 100%;
    padding: 10px;
  }
  .none-dot {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 1px dashed var(--text-faint);
  }
  .oname {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    text-align: center;
    line-height: 1.25;
  }
  .ds-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 14px;
    border-top: 1px solid var(--border);
  }
</style>
