<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import Banner from "../Banner.svelte";
  import BannerStudio from "../BannerStudio.svelte";
  import RingStudio from "../RingStudio.svelte";
  import GameShelf from "../GameShelf.svelte";
  import { RING_BY_ID, RINGS } from "../lib/rings.js";
  import { api } from "../lib/api.js";
  let { identity, onSubmit, onClose } = $props();
  let name = $state(identity.displayName || "");
  let status = $state(identity.status || "");
  let emoji = $state(identity.emoji || "");
  let color = $state(identity.color || "#14a394");
  let avatar = $state(identity.avatar || "");
  let presence = $state(identity.presence || "online");
  let bio = $state(identity.bio || "");
  let color2 = $state(identity.color2 || "");
  let frame = $state(identity.frame || "");
  let effect = $state(identity.effect || "");
  let games = $state(identity.games || []);

  // Games save immediately (like in the profile card) — independent of the
  // Save button, which commits the rest of the profile.
  async function saveGames(next) {
    games = next;
    try {
      await api.setGames(next);
    } catch {}
  }
  // Fine-grained dials (RingStudio/BannerStudio own the UI for these).
  const st0 = identity.style || {};
  let speed = $state(st0.speed || "normal");
  let dir = $state(st0.dir || "cw");
  let glow = $state(st0.glow || "soft");
  let ringW = $state(st0.width || 2);
  let angle = $state(Number.isFinite(st0.angle) ? st0.angle : 120);
  let fill = $state(st0.fill || "gradient");
  let sat = $state(st0.sat || ""); // your rider: an emoji or an uploaded picture
  let pal = $state(st0.pal || ""); // the Gradient ring's colorway
  let bannerStudio = $state(false);
  let ringStudio = $state(false);
  const styleObj = $derived({ speed, dir, glow, width: ringW, angle, fill, sat, pal });
  let fileInput;

  const EMOJIS = ["😀", "😎", "🦊", "🐸", "👾", "🧙", "🚀", "🌸", "⚡", "🔥", "🌙", "🎮"];
  const PRESENCES = [
    { id: "online", label: "Online", dot: "var(--ok)" },
    { id: "idle", label: "Idle", dot: "#f0b232" },
    { id: "dnd", label: "Do Not Disturb", dot: "#f04747" },
    { id: "invisible", label: "Invisible", dot: "var(--text-faint)" },
  ];

  // ---- crop editor ----
  // When a picture is chosen or pasted, we open a small editor (drag to
  // reposition, slider to zoom) and only bake it to a 256×256 JPEG on Apply.
  const VIEW = 200;
  const OUT = 256;
  let rawImg = $state(null); // HTMLImageElement being cropped
  let zoom = $state(1);
  let dragX = $state(0);
  let dragY = $state(0);
  let cropCanvas = $state(null);

  function loadForCrop(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      rawImg = img;
      zoom = 1;
      dragX = 0;
      dragY = 0;
    };
    img.onerror = () => URL.revokeObjectURL(url);
    img.src = url;
  }

  // Draw the current crop into a canvas of the given size, returning the
  // clamped drag (so callers converge on valid offsets).
  function drawCrop(canvas, size) {
    if (!rawImg || !canvas) return;
    const iw = rawImg.naturalWidth;
    const ih = rawImg.naturalHeight;
    const base = size / Math.min(iw, ih);
    const eff = base * zoom;
    const dw = iw * eff;
    const dh = ih * eff;
    const slackX = Math.max(0, (dw - size) / 2);
    const slackY = Math.max(0, (dh - size) / 2);
    const scale = size / VIEW;
    const cx = Math.max(-slackX, Math.min(slackX, dragX * scale));
    const cy = Math.max(-slackY, Math.min(slackY, dragY * scale));
    const ctx = canvas.getContext("2d");
    ctx.clearRect(0, 0, size, size);
    ctx.drawImage(rawImg, size / 2 - dw / 2 + cx, size / 2 - dh / 2 + cy, dw, dh);
  }

  // Live preview redraw.
  $effect(() => {
    zoom;
    dragX;
    dragY;
    rawImg;
    drawCrop(cropCanvas, VIEW);
  });

  let dragging = false;
  let lastX = 0;
  let lastY = 0;
  function onDown(e) {
    dragging = true;
    lastX = e.clientX;
    lastY = e.clientY;
    e.currentTarget.setPointerCapture?.(e.pointerId);
  }
  function onMove(e) {
    if (!dragging) return;
    dragX += e.clientX - lastX;
    dragY += e.clientY - lastY;
    lastX = e.clientX;
    lastY = e.clientY;
  }
  function onUp() {
    dragging = false;
  }

  function applyCrop() {
    const c = document.createElement("canvas");
    c.width = c.height = OUT;
    drawCrop(c, OUT);
    avatar = c.toDataURL("image/jpeg", 0.85);
    rawImg = null;
  }

  // ---- banner (wide header image) ----
  let banner = $state(identity.banner || "");
  let bannerInput;
  // Whether a paste should target the banner (pointer over it) vs the avatar.
  let pasteTarget = $state("avatar");

  // Downscale a chosen banner to a reasonable width so the profile broadcast
  // stays small, then store it as a JPEG data URI.
  function loadBanner(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const maxW = 640;
      const scale = Math.min(1, maxW / img.naturalWidth);
      const c = document.createElement("canvas");
      c.width = Math.round(img.naturalWidth * scale);
      c.height = Math.round(img.naturalHeight * scale);
      c.getContext("2d").drawImage(img, 0, 0, c.width, c.height);
      banner = c.toDataURL("image/jpeg", 0.82);
    };
    img.onerror = () => URL.revokeObjectURL(url);
    img.src = url;
  }

  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (!item) return;
    e.preventDefault();
    if (pasteTarget === "banner") loadBanner(item.getAsFile());
    else loadForCrop(item.getAsFile());
  }

  function save() {
    onSubmit({
      name: name.trim(),
      status: status.trim(),
      emoji,
      color,
      avatar,
      banner,
      presence,
      bio: bio.trim(),
      color2,
      frame,
      effect,
      style: styleObj,
    });
  }
</script>

<svelte:window onpaste={onPaste} />

<Modal title="Your profile" wide {onClose}>
  <!-- ===== LIVE PREVIEW: exactly what other people see. ===== -->
  <div class="studio">
    <!-- Discord-style: a tall banner the avatar straddles, so the card is
         mostly art instead of mostly empty background. Hover the banner and it
         blurs behind an "Edit banner" call to action — the banner IS the
         button. -->
    <div class="pv-card card-effect-{effect || 'none'}">
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <Banner
        {banner}
        {color}
        {color2}
        style={{ angle, fill }}
        class="pv-banner"
        onclick={() => (bannerStudio = true)}
        title="Edit banner"
      >
        <span class="banner-edit"><Icon name="edit" size={14} /> Edit banner</span>
      </Banner>
      <div class="pv-head">
        <div class="pv-av">
          <Avatar
            name={name || "You"}
            {emoji}
            {color}
            image={avatar}
            size={72}
            {frame}
            style={styleObj}
            {color2}
          />
        </div>
        <div class="pv-id">
          <div class="pv-name">{name || "Your name"}</div>
          {#if status}<div class="pv-status tiny muted">{status}</div>{/if}
        </div>
      </div>
    </div>
    <p class="tiny muted pv-note">Live preview — this is your profile card.</p>
  </div>

  <label class="field">
    <span class="muted">Display name</span>
    <input bind:value={name} maxlength="32" placeholder="Your name" />
  </label>
  <div class="field">
    <span class="muted">Availability</span>
    <div class="presence-row">
      {#each PRESENCES as pr (pr.id)}
        <button
          type="button"
          class="presence"
          class:sel={presence === pr.id}
          onclick={() => (presence = pr.id)}
        >
          <span class="pdot" style="background:{pr.dot}"></span>
          {pr.label}
        </button>
      {/each}
    </div>
  </div>
  <label class="field">
    <span class="muted">Status</span>
    <input bind:value={status} maxlength="64" placeholder="e.g. building something epic" />
  </label>
  <label class="field">
    <span class="muted">About me</span>
    <textarea bind:value={bio} maxlength="600" rows="3" placeholder="A short bio — shown on your profile card"></textarea>
  </label>
  <div class="field">
    <span class="muted">Fallback emoji (used when no picture)</span>
    <div class="emoji-row">
      <button class="emoji" class:sel={emoji === ""} onclick={() => (emoji = "")} title="Use initials">Aa</button>
      {#each EMOJIS as e (e)}
        <button class="emoji" class:sel={emoji === e} onclick={() => (emoji = e)}>{e}</button>
      {/each}
    </div>
  </div>
  <div class="field">
    <span class="muted">Profile theme</span>
    <!-- Two colors form the card's gradient; the strip previews it live. -->
    <div
      class="theme-preview"
      style={`background:linear-gradient(120deg, ${color}, ${color2 || color})`}
    ></div>
    <div class="theme-row">
      <label class="theme-color">
        <input type="color" bind:value={color} />
        <span class="muted tiny">Primary</span>
      </label>
      <label class="theme-color">
        <input
          type="color"
          value={color2 || color}
          oninput={(e) => (color2 = e.target.value)}
        />
        <span class="muted tiny">Secondary</span>
      </label>
      {#if color2}
        <button type="button" class="ghost small-btn" onclick={() => (color2 = "")}>
          Single color
        </button>
      {/if}
    </div>
  </div>

  <div class="field">
    <span class="muted">Avatar ring</span>
    <button type="button" class="ring-entry" onclick={() => (ringStudio = true)}>
      <Avatar name={name || "You"} {emoji} {color} image={avatar} size={30} {frame} style={styleObj} {color2} />
      <span class="re-text">
        <strong>{RING_BY_ID[frame]?.name || "None"}</strong>
        <span class="tiny muted">{RINGS.length - 1} effects · weather, orbits, riders, colorways</span>
      </span>
      <span class="chev">›</span>
    </button>
  </div>

  <div class="field">
    <span class="muted">Game collection</span>
    <GameShelf games={games} editable onchange={saveGames} />
  </div>

  <div class="field verify-info">
    <span class="muted">Your identity fingerprint (others verify you with this):</span>
    <code class="mono">{identity.fingerprint}</code>
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={!name.trim()}>Save</button>
  </div>

  {#if ringStudio}
    <RingStudio
      ring={frame}
      speed={speed}
      dir={dir}
      glow={glow}
      width={ringW}
      {sat}
      {pal}
      {color}
      {color2}
      {avatar}
      {emoji}
      name={name || "You"}
      onApply={(r) => {
        frame = r.ring;
        speed = r.speed;
        dir = r.dir;
        glow = r.glow;
        ringW = r.width;
        sat = r.sat;
        pal = r.pal;
        ringStudio = false;
      }}
      onClose={() => (ringStudio = false)}
    />
  {/if}

  {#if bannerStudio}
    <BannerStudio
      {banner}
      {color}
      {color2}
      {angle}
      overlay={effect}
      onApply={(r) => {
        banner = r.banner;
        angle = r.angle;
        effect = r.effect;
        bannerStudio = false;
      }}
      onClose={() => (bannerStudio = false)}
    />
  {/if}
</Modal>

<style>
  .banner-strip {
    position: relative;
    height: 90px;
    border-radius: 8px;
    background: var(--bg-input);
    background-size: cover;
    background-position: center;
    cursor: pointer;
    display: grid;
    place-items: center;
    overflow: hidden;
    border: 1px dashed var(--border);
  }
  .banner-strip {
    transition:
      border-color 0.15s ease,
      box-shadow 0.15s ease;
  }
  .banner-strip:hover {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .banner-hint {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #fff;
    background: rgba(0, 0, 0, 0.5);
    padding: 3px 8px;
    border-radius: 10px;
    opacity: 0.85;
  }
  .banner-remove {
    position: absolute;
    top: 6px;
    right: 6px;
    width: 24px;
    height: 24px;
    padding: 0;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    display: grid;
    place-items: center;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px;
    background: var(--bg-input);
    border-radius: 8px;
  }
  .avatar {
    position: relative;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    font-size: 22px;
    color: white;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
    padding: 0;
    overflow: hidden;
    transition:
      transform 0.18s cubic-bezier(0.34, 1.56, 0.64, 1),
      box-shadow 0.18s ease;
  }
  .avatar:hover {
    transform: scale(1.05);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  @media (prefers-reduced-motion: reduce) {
    .avatar,
    .banner-strip {
      transition: none;
    }
  }
  .avatar img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* A clean centered hover hint instead of a clipped edge badge. */
  .cam-overlay {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #fff;
    background: rgba(0, 0, 0, 0.5);
    opacity: 0;
    transition: opacity 0.12s ease;
  }
  .avatar:hover .cam-overlay,
  .avatar:focus-visible .cam-overlay {
    opacity: 1;
  }
  .small-btn {
    font-size: 12px;
    padding: 4px 10px;
    align-self: flex-start;
  }
  .preview-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 13px;
    text-align: left;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    text-align: left;
    font-size: 12px;
  }
  .row-field {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }
  .theme-preview {
    height: 34px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    margin-bottom: 6px;
  }
  .theme-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .theme-color {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
  }
  .frame-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .frame-opt {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    padding: 7px 9px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .frame-opt:hover {
    background: var(--bg-3);
  }
  .frame-opt.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  /* ---- live preview ---- */
  .studio {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .pv-card {
    position: relative;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--bg-1);
    padding-bottom: 12px;
  }
  .pv-card :global(.pv-banner) {
    height: 120px;
    cursor: pointer;
  }
  /* The avatar straddles the banner's bottom edge and the name sits beside it,
     so the space under the banner is used instead of left blank. */
  .pv-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 14px;
    margin-top: -34px;
  }
  .pv-id {
    padding-top: 30px; /* clears the half of the avatar that hangs down */
    min-width: 0;
  }
  .ring-entry {
    display: flex;
    align-items: center;
    gap: 16px;
    width: 100%;
    padding: 10px 14px;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    text-align: left;
    overflow: clip;
  }
  .ring-entry:hover {
    background: var(--bg-3);
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }
  .re-text {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .chev {
    color: var(--text-faint);
    font-size: 16px;
  }
  .banner-edit {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: 12.5px;
    font-weight: 600;
    color: #fff;
    background: rgba(0, 0, 0, 0.42);
    backdrop-filter: blur(3px);
    opacity: 0;
    transition: opacity 0.15s ease;
  }
  .pv-card :global(.pv-banner:hover .banner-edit),
  .pv-card :global(.pv-banner:focus-visible .banner-edit) {
    opacity: 1;
  }
  .pv-av {
    width: fit-content;
    padding: 3px;
    background: var(--bg-1);
    border-radius: 50%;
    position: relative;
    z-index: 1;
  }
  .pv-name {
    font-weight: 700;
    font-size: 16px;
  }
  .pv-status {
    margin-top: 1px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pv-note {
    text-align: center;
    margin: 0;
  }
  .tiny {
    font-size: 10px;
  }
  .row-field input[type="color"] {
    width: 48px;
    height: 30px;
    padding: 2px;
    cursor: pointer;
  }
  .emoji-row {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .emoji {
    background: var(--bg-input);
    border: 1px solid var(--border);
    padding: 4px 8px;
    font-size: 16px;
    border-radius: 6px;
    color: var(--text);
    transition: transform 0.15s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .emoji:hover {
    transform: scale(1.12);
  }
  .emoji.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    animation: sel-pop 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  @keyframes sel-pop {
    40% {
      transform: scale(1.18);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .emoji {
      transition: none;
      animation: none;
    }
    .emoji.sel {
      animation: none;
    }
  }
  .presence-row {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .presence {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    font-size: 12px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    transition:
      border-color 0.15s ease,
      background 0.15s ease,
      color 0.15s ease;
  }
  .presence.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
    animation: sel-pop 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  @media (prefers-reduced-motion: reduce) {
    .presence,
    .presence.sel {
      transition: none;
      animation: none;
    }
  }
  .pdot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
  }
  textarea {
    resize: vertical;
    font-family: inherit;
    font-size: 13px;
    padding: 8px 10px;
  }
  .cropper {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 12px;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .crop-view {
    position: relative;
    width: 200px;
    height: 200px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    cursor: grab;
    touch-action: none;
    background: var(--bg-1);
  }
  .crop-view:active {
    cursor: grabbing;
  }
  .crop-view canvas {
    display: block;
  }
  .crop-ring {
    position: absolute;
    inset: 0;
    border-radius: 50%;
    box-shadow: 0 0 0 2000px rgba(0, 0, 0, 0.45);
    pointer-events: none;
  }
  .zoom {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    color: var(--text-muted);
  }
  .zoom input[type="range"] {
    flex: 1;
    accent-color: var(--accent);
  }
  .crop-actions {
    display: flex;
    gap: 8px;
    align-self: flex-end;
  }
  .tiny {
    font-size: 11px;
    margin: 0;
  }
  .paste-hint {
    align-self: flex-start;
  }
  .verify-info code {
    font-size: 11px;
    word-break: break-all;
    background: var(--bg-input);
    padding: 6px 8px;
    border-radius: 6px;
    display: block;
  }
</style>
