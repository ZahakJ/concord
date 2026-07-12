<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { ringVars } from "../lib/profilestyle.js";
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
  // Fine-grained dials (see lib/profilestyle.js + app.css).
  const st0 = identity.style || {};
  let speed = $state(st0.speed || "normal");
  let dir = $state(st0.dir || "cw");
  let glow = $state(st0.glow || "soft");
  let ringW = $state(st0.width || 2);
  let angle = $state(Number.isFinite(st0.angle) ? st0.angle : 120);
  let fill = $state(st0.fill || "gradient");
  const styleObj = $derived({ speed, dir, glow, width: ringW, angle, fill });
  let fileInput;

  // Avatar frames: decorative rings rendered client-side from an enum id, so
  // they cost bytes on the wire, never images.
  // Avatar rings. The bottom four ANIMATE (spinning conic gradients, a
  // breathing halo, an orbiting satellite) — pure CSS, so they cost the same
  // few bytes on the wire as the plain ones.
  const FRAMES = [
    { id: "", label: "None" },
    { id: "theme", label: "Your colors ✨" },
    { id: "gold", label: "Gold" },
    { id: "neon", label: "Neon" },
    { id: "ember", label: "Ember" },
    { id: "frost", label: "Frost" },
    { id: "aurora", label: "Aurora ✨" },
    { id: "rainbow", label: "Rainbow ✨" },
    { id: "gem", label: "Diamond ✨" },
    { id: "pulse", label: "Pulse ✨" },
    { id: "orbit", label: "Orbit ✨" },
  ];

  // Card effects: animated flourishes over the profile banner.
  const EFFECTS = [
    { id: "", label: "None" },
    { id: "aurora", label: "Aurora drift" },
    { id: "sheen", label: "Sheen sweep" },
    { id: "sparkle", label: "Sparkles" },
    { id: "nebula", label: "Nebula" },
  ];

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
    <div class="pv-card card-effect-{effect || 'none'}">
      <div
        class="banner"
        style={banner
          ? `background-image:url(${banner})`
          : fill === "solid" || !color2
            ? `background:${color}`
            : `background:linear-gradient(${angle}deg, ${color}, ${color2})`}
      ></div>
      <div class="pv-av">
        <Avatar
          name={name || "You"}
          {emoji}
          {color}
          image={avatar}
          size={56}
          {frame}
          style={styleObj}
          {color2}
        />
      </div>
      <div class="pv-name">{name || "Your name"}</div>
    </div>
    <p class="tiny muted pv-note">Live preview — this is your profile card.</p>
  </div>

  <!-- ===== BANNER: an explicit, labelled section (image OR gradient). ===== -->
  <div class="field">
    <span class="muted">Profile banner</span>
    <div class="banner-modes">
      <button
        type="button"
        class="bmode"
        class:sel={!banner && fill === "gradient"}
        onclick={() => {
          banner = "";
          fill = "gradient";
        }}
      >
        Gradient
      </button>
      <button
        type="button"
        class="bmode"
        class:sel={!banner && fill === "solid"}
        onclick={() => {
          banner = "";
          fill = "solid";
        }}
      >
        Solid
      </button>
      <button type="button" class="bmode" class:sel={!!banner} onclick={() => bannerInput.click()}>
        Image…
      </button>
      {#if banner}
        <button type="button" class="bmode remove" onclick={() => (banner = "")}>Remove image</button>
      {/if}
    </div>
    {#if !banner && fill === "gradient"}
      <label class="dial">
        <span class="tiny muted">Gradient angle · {angle}°</span>
        <input type="range" min="0" max="360" step="5" bind:value={angle} />
      </label>
    {/if}
    <p class="tiny muted">
      The gradient uses your two profile colors below. Or drop in an image —
      you can also just paste one anywhere in this dialog.
    </p>
  </div>
  <input
    type="file"
    accept="image/*"
    bind:this={bannerInput}
    style="display:none"
    onchange={(e) => {
      loadBanner(e.target.files?.[0]);
      e.target.value = "";
    }}
  />

  <div class="preview" onmouseenter={() => (pasteTarget = "avatar")} role="group">
    <button
      class="avatar"
      style="background:{color}"
      title="Click to upload a picture"
      onclick={() => fileInput.click()}
    >
      {#if avatar}
        <img src={avatar} alt="avatar" />
      {:else}
        {emoji || (name || "?").slice(0, 2)}
      {/if}
      <span class="cam-overlay">Change</span>
    </button>
    <div class="preview-text">
      <strong>{name || "Your name"}</strong>
      {#if status}<span class="muted">{status}</span>{/if}
    </div>
  </div>
  <input
    type="file"
    accept="image/*"
    bind:this={fileInput}
    style="display:none"
    onchange={(e) => {
      loadForCrop(e.target.files?.[0]);
      e.target.value = "";
    }}
  />

  {#if rawImg}
    <div class="cropper">
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="crop-view"
        onpointerdown={onDown}
        onpointermove={onMove}
        onpointerup={onUp}
        onpointerleave={onUp}
      >
        <canvas bind:this={cropCanvas} width={VIEW} height={VIEW}></canvas>
        <div class="crop-ring"></div>
      </div>
      <label class="zoom">
        <Icon name="search" size={13} />
        <input type="range" min="1" max="3" step="0.01" bind:value={zoom} />
      </label>
      <p class="muted tiny">Drag to reposition · slide to zoom</p>
      <div class="crop-actions">
        <button class="ghost small-btn" onclick={() => (rawImg = null)}>Cancel</button>
        <button class="small-btn" onclick={applyCrop}>Apply</button>
      </div>
    </div>
  {:else if avatar}
    <button class="ghost small-btn" onclick={() => (avatar = "")}>Remove picture</button>
  {/if}
  <p class="muted tiny paste-hint">Tip: you can paste an image from your clipboard.</p>

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
    <span class="muted">Avatar frame <em class="tiny">— ✨ ones animate</em></span>
    <div class="frame-row">
      {#each FRAMES as f (f.id)}
        <button
          type="button"
          class="frame-opt"
          class:sel={frame === f.id}
          onclick={() => (frame = f.id)}
          title={f.label}
        >
          <span
            class="frame-demo avatar-frame-{f.id || 'none'}"
            style={`${ringVars(styleObj, color)}${color2 ? `--ring-color2:${color2};` : ""}`}
          ></span>
          <span class="tiny muted">{f.label}</span>
        </button>
      {/each}
    </div>
  </div>

  {#if frame}
    <div class="field">
      <span class="muted">Ring animation</span>
      <div class="dials">
        <div class="dial-group">
          <span class="tiny muted">Speed</span>
          <div class="seg">
            {#each ["slow", "normal", "fast"] as sp (sp)}
              <button type="button" class:sel={speed === sp} onclick={() => (speed = sp)}>{sp}</button>
            {/each}
          </div>
        </div>
        <div class="dial-group">
          <span class="tiny muted">Direction</span>
          <div class="seg">
            <button type="button" class:sel={dir === "cw"} onclick={() => (dir = "cw")}>↻ cw</button>
            <button type="button" class:sel={dir === "ccw"} onclick={() => (dir = "ccw")}>↺ ccw</button>
          </div>
        </div>
        <div class="dial-group">
          <span class="tiny muted">Glow</span>
          <div class="seg">
            {#each ["off", "soft", "strong"] as gl (gl)}
              <button type="button" class:sel={glow === gl} onclick={() => (glow = gl)}>{gl}</button>
            {/each}
          </div>
        </div>
        <label class="dial-group">
          <span class="tiny muted">Thickness · {ringW}px</span>
          <input type="range" min="1" max="5" step="1" bind:value={ringW} />
        </label>
      </div>
    </div>
  {/if}

  <div class="field">
    <span class="muted">Card effect</span>
    <div class="fx-row">
      {#each EFFECTS as f (f.id)}
        <button
          type="button"
          class="fx-opt card-effect-{f.id || 'none'}"
          class:sel={effect === f.id}
          onclick={() => (effect = f.id)}
          title={f.label}
        >
          <span
            class="banner fx-demo"
            style={`background:linear-gradient(120deg, ${color}, ${color2 || color})`}
          ></span>
          <span class="tiny muted">{f.label}</span>
        </button>
      {/each}
    </div>
    <p class="tiny muted fx-note">
      Effects animate the banner on your profile card — with or without an
      image.
    </p>
  </div>

  <div class="field verify-info">
    <span class="muted">Your identity fingerprint (others verify you with this):</span>
    <code class="mono">{identity.fingerprint}</code>
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={!name.trim()}>Save</button>
  </div>
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
  .pv-card .banner {
    height: 74px;
    background-size: cover;
    background-position: center;
  }
  .pv-av {
    width: fit-content;
    margin: -28px 0 0 14px;
    padding: 3px;
    background: var(--bg-1);
    border-radius: 50%;
    position: relative;
  }
  .pv-name {
    padding: 6px 14px 0;
    font-weight: 700;
    font-size: 15px;
  }
  .pv-note {
    text-align: center;
    margin: 0;
  }
  /* ---- banner mode buttons ---- */
  .banner-modes {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .bmode {
    padding: 6px 12px;
    font-size: 12.5px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }
  .bmode:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .bmode.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  .bmode.remove {
    border-color: color-mix(in srgb, var(--danger) 45%, var(--border));
    color: var(--danger);
  }
  /* ---- dials ---- */
  .dials {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 10px;
  }
  .dial-group,
  .dial {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .seg {
    display: flex;
    gap: 4px;
  }
  .seg button {
    flex: 1;
    padding: 5px 6px;
    font-size: 11.5px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    text-transform: capitalize;
    cursor: pointer;
  }
  .seg button:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .seg button.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  .frame-demo {
    position: relative;
    display: block;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    background: linear-gradient(135deg, var(--bg-3), var(--bg-2));
    margin: 4px;
    isolation: isolate;
  }
  .fx-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .fx-opt {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    padding: 6px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .fx-opt:hover {
    background: var(--bg-3);
  }
  .fx-opt.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .fx-demo {
    display: block;
    width: 62px;
    height: 30px;
    border-radius: 6px;
    overflow: hidden;
  }
  .fx-note {
    margin: 6px 0 0;
    line-height: 1.45;
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
