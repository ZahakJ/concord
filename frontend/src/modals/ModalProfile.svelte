<script>
  import RailShell from "./RailShell.svelte";
  import { emojiName } from "../lib/emoji.js";
  import Icon from "../Icon.svelte";
  import Select from "../Select.svelte";
  import Avatar from "../Avatar.svelte";
  import Banner from "../Banner.svelte";
  import BannerStudio from "../BannerStudio.svelte";
  import DecorStudio from "../DecorStudio.svelte";
  import EffectStudio from "../EffectStudio.svelte";
  import CardFrameStudio from "../CardFrameStudio.svelte";
  import CardScene from "../CardScene.svelte";
  import CardFrame from "../CardFrame.svelte";
  import FxLayer from "../FxLayer.svelte";
  import { DECORATION_BY_ID, DECORATIONS, COLORWAYS } from "../lib/decorations.js";
  import { CARD_EFFECT_BY_ID, CARD_EFFECTS, cardEffect } from "../lib/cardfx.js";
  import { CARD_FRAME_BY_ID, CARD_FRAMES } from "../lib/cardframes.js";
  import { CARD_SCENE_BY_ID, CARD_SCENES, cardScene } from "../lib/cardscenes.js";
  import GameShelf from "../GameShelf.svelte";
  import { RING_BY_ID, RINGS } from "../lib/rings.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";
  import { S } from "../lib/state.svelte.js";
  import { rangefill } from "../lib/rangefill.js";
  import { pointOf } from "../lib/place.js";
  let { identity, onSubmit, onClose } = $props();
  let name = $state(identity.displayName || "");
  let status = $state(identity.status || "");
  let emoji = $state(identity.emoji || "");
  let color = $state(identity.color || "#14a394");
  let avatar = $state(identity.avatar || "");
  let presence = $state(identity.presence || "online");
  let bio = $state(identity.bio || "");
  // Birthday is "MM-DD" — month and day only. There is no year control here
  // and never will be: Concord doesn't ask, so it can't store or leak it.
  const bday0 = /^(\d{2})-(\d{2})$/.exec(identity.birthday || "");
  let bMonth = $state(bday0 ? bday0[1] : "");
  let bDay = $state(bday0 ? bday0[2] : "");
  const MONTHS = ["January", "February", "March", "April", "May", "June", "July",
    "August", "September", "October", "November", "December"];
  // Real month lengths (Feb 29 exists — leap-day birthdays are real people).
  const MONTH_DAYS = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  const dayOptions = $derived(
    Array.from({ length: bMonth ? MONTH_DAYS[+bMonth - 1] : 31 }, (_, i) => String(i + 1).padStart(2, "0")),
  );
  // Switching from a longer month can strand an impossible day (Mar 31 → Feb);
  // drop it rather than silently saving a date that never comes around.
  $effect(() => {
    if (bDay && bMonth && +bDay > MONTH_DAYS[+bMonth - 1]) bDay = "";
  });
  const birthday = $derived(bMonth && bDay ? `${bMonth}-${bDay}` : "");
  let color2 = $state(identity.color2 || "");
  let frame = $state(identity.frame || "");
  let dec = $state(identity.style?.dec || "");
  let dc = $state(identity.style?.dc || ""); // the decoration's colourway
  let cf = $state(identity.style?.cf || "");
  let decorStudio = $state(false);
  let cfStudio = $state(false);
  let effectStudio = $state(false);
  let effect = $state(identity.effect || "");
  let games = $state(identity.games || []);

  // This is the string another person reads aloud, or compares against a second
  // screen, to establish that you are you. Grouped in fours so an eye can hold
  // a place in it, and tappable, because selecting a run of monospace inside a
  // scrolling sheet with a fingertip is close to impossible.
  //
  // Strip the whitespace BEFORE regrouping. Fingerprints already arrive
  // space-grouped, so chunking the raw string counted those spaces as
  // characters and produced ragged runs — "YXNO  YAD U MX G3" — in the one
  // string in the app whose whole job is to be read out loud and matched
  // character for character. ProfilePopover fixed this; this copy did not.
  const fprGroups = $derived(
    (identity.fingerprint || "")
      .replace(/\s+/g, "")
      .replace(/(.{4})/g, "$1 ")
      .trim(),
  );
  let fprCopied = $state(false);
  async function copyFingerprint() {
    try {
      await navigator.clipboard?.writeText(identity.fingerprint);
      haptic("light");
      fprCopied = true;
      setTimeout(() => (fprCopied = false), 1400);
    } catch {
      /* clipboard denied — the text is still on screen to read out */
    }
  }

  // Games save immediately (like in the profile card) — independent of the
  // Save button, which commits the rest of the profile.
  async function saveGames(next) {
    games = next;
    try {
      await api.setGames(next);
    } catch {}
  }
  // Fine-grained dials (DecorStudio/BannerStudio own the UI for these).
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
  // Every style field has to be listed here or its value is silently dropped
  // on save — `dec` went missing that way once.
  const styleObj = $derived({ speed, dir, glow, width: ringW, angle, fill, sat, pal, dec, dc, cf });
  // What the two card studios need in order to preview YOUR card rather than a
  // wireframe. One object, so a new field reaches both without a third call
  // site learning about it.
  //
  // `style` is the WHOLE style object, not the two banner fields it used to
  // carry. Avatar and AvatarRing read sat/pal/speed/dir/glow/width out of it, so
  // a partial one previewed your card wearing a default ring at default speed
  // with your colourway missing — the same "list every field or lose it" trap
  // that ate `dec` once already, in a second place. The frame and the effect
  // travel too, so the frame studio can draw your effect and the effect studio
  // your frame.
  const cardProps = $derived({
    name: name || "You",
    emoji,
    avatar,
    banner,
    style: styleObj,
    ring: frame,
    frame: cf,
    effect,
    dec,
    status,
  });
  const fxOf = $derived(effect ? cardEffect(effect) : null);
  const sceneOf = $derived(effect ? cardScene(effect) : null);
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
    const p = pointOf(e);
    lastX = p.x;
    lastY = p.y;
    e.currentTarget.setPointerCapture?.(e.pointerId);
  }
  function onMove(e) {
    if (!dragging) return;
    // The drag is measured in the preview's own units, which are layout
    // pixels; a raw pointer delta is visual and pans faster than the hand.
    const p = pointOf(e);
    dragX += p.x - lastX;
    dragY += p.y - lastY;
    lastX = p.x;
    lastY = p.y;
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

  async function save() {
    // The shell's onSubmit handler forwards only the twelve classic fields
    // (the API is positional), so birthday is committed here, and only when it
    // actually changed, to spare a double broadcast. The shell's follow-up
    // save uses the old arity, which the backend reads as "leave birthday
    // alone", so it can't wipe what we just wrote.
    if (birthday !== (identity.birthday || "")) {
      try {
        await api.setProfile(
          name.trim(), status.trim(), emoji, color, avatar, banner, presence,
          bio.trim(), color2, frame, effect, JSON.stringify(styleObj),
          birthday,
        );
      } catch {}
    }
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

<RailShell title="Your profile" here="profile" {onClose}>
  <!-- ===== LIVE PREVIEW: exactly what other people see. ===== -->
  <div class="studio">
    <!-- A tall banner the avatar straddles, so the card is
         mostly art instead of mostly empty background. Hover the banner and it
         blurs behind an "Edit banner" call to action — the banner IS the
         button. -->
    <!-- The hero has to be wearing everything the live card wears. It knew
         about the banner, the avatar and the decoration, and not about the card
         frame or the card effect — so the two cosmetics defined by what they do
         to a card were the two this card would not show you. The frame is a
         SIBLING, as it is on the real popover, because its art overhangs. -->
    <div class="pv-stage" class:framed={!!cf}>
      {#if cf}
        <CardFrame id={cf} {color} color2={color2 || color} />
      {/if}
      <div class="pv-card card-effect-{effect || 'none'}" class:framed={!!cf}>
      {#if fxOf}
        <span class="pv-fx"><FxLayer fx={fxOf.fx} seed={effect} /></span>
      {/if}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <Banner
        {banner}
        {color}
        {color2}
        style={{ angle, fill }}
        class="pv-banner banner"
        onclick={() => (bannerStudio = true)}
        title="Edit banner"
      >
        <span class="banner-edit"><Icon name="edit" size={14} /> Edit banner</span>
      </Banner>
      <!-- After the banner, before the avatar: a drawn scene IS the art and
           its subject lives in the top third, which a 120px banner would bury.
           Same ordering as the popover, for the same reason. -->
      {#if sceneOf}
        <span class="pv-fx"><CardScene id={effect} {color} {color2} /></span>
      {/if}
      <div class="pv-head">
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="pv-av"
          role="button"
          tabindex="0"
          title="Change picture"
          onpointerenter={() => (pasteTarget = "avatar")}
          onclick={() => fileInput?.click()}
        >
          <!-- The decoration belongs here as much as the ring does. It was
               missing, and the card claims to be exactly what other people see
               — the member list and the profile popover have always drawn it.
               Now that the drawn rings are decorations, its absence would have
               meant picking one and watching the preview not change. -->
          <Avatar
            name={name || "You"}
            {emoji}
            {color}
            image={avatar}
            size={72}
            {frame}
            decoration={dec}
            style={styleObj}
            {color2}
          />
          <span class="av-edit"><Icon name="edit" size={13} /></span>
        </div>
        <div class="pv-id">
          <div class="pv-name">{name || "Your name"}</div>
          {#if status}<div class="pv-status tiny muted">{status}</div>{/if}
        </div>
        </div>
      </div>
    </div>
  </div>

  <!-- Hidden picker: clicking the avatar (or "Change picture") opens it, and a
       chosen file drops into the crop editor below. -->
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
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="cropper">
      <p class="tiny muted paste-hint">Drag to reposition · slider to zoom</p>
      <div
        class="crop-view"
        onpointerdown={onDown}
        onpointermove={onMove}
        onpointerup={onUp}
        onpointercancel={onUp}
      >
        <canvas bind:this={cropCanvas} width={VIEW} height={VIEW}></canvas>
        <span class="crop-ring"></span>
      </div>
      <label class="zoom">
        <Icon name="search" size={14} />
        <input type="range" min="1" max="4" step="0.01" bind:value={zoom} use:rangefill={zoom} />
      </label>
      <div class="crop-actions">
        <button type="button" class="ghost" onclick={() => (rawImg = null)}>Cancel</button>
        <button type="button" onclick={applyCrop}>Apply</button>
      </div>
    </div>
  {/if}

  <label class="field">
    <span class="muted">Display name</span>
    <input dir="auto" bind:value={name} maxlength="32" placeholder="Your name" />
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
    <span class="muted">Birthday (optional)</span>
    <div class="bday-row">
      <Select
        label="Birthday month"
        placeholder="Month"
        value={bMonth}
        onPick={(v) => (bMonth = v)}
        options={[
          { value: "", label: "Month" },
          ...MONTHS.map((m, i) => ({ value: String(i + 1).padStart(2, "0"), label: m })),
        ]}
      />
      <Select
        label="Birthday day"
        placeholder="Day"
        disabled={!bMonth}
        value={bDay}
        onPick={(v) => (bDay = v)}
        options={[{ value: "", label: "Day" }, ...dayOptions.map((d) => ({ value: d, label: String(+d) }))]}
      />
      {#if bMonth || bDay}
        <button
          type="button"
          class="ghost small-btn"
          onclick={() => {
            bMonth = "";
            bDay = "";
          }}>Clear</button
        >
      {/if}
    </div>
    <p class="tiny muted">No year, ever. Your guilds see a 🎂 on the day.</p>
  </div>
  <div class="field">
    <span class="muted">Fallback emoji (used when no picture)</span>
    <div class="emoji-row">
      <button class="emoji" class:sel={emoji === ""} onclick={() => (emoji = "")} title="Use initials">Aa</button>
      {#each EMOJIS as e (e)}
        <button
          class="emoji"
          class:sel={emoji === e}
          aria-label={emojiName(e) || "Emoji"}
          onclick={() => (emoji = e)}>{e}</button>
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
        <input type="color" bind:value={color} onchange={(e) => (color = e.target.value)} />
        <span class="muted tiny">Primary</span>
      </label>
      <label class="theme-color">
        <!-- `change` as well as `input`: WebKitGTK hands the colour well to
             GTK's own chooser, which commits on dismissal and does not stream
             `input` events the way Chromium's inline picker does. Without it
             the desktop build was the one place a picked secondary colour
             never arrived. -->
        <input
          type="color"
          value={color2 || color}
          oninput={(e) => (color2 = e.target.value)}
          onchange={(e) => (color2 = e.target.value)}
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

  <!-- Three named things: what you wear on your avatar, the art drawn around
       your card, and what plays across it. Those are three different objects
       on three different surfaces, so they are three rows. What you WEAR is
       one row and not two — a gradient ring and a drawn crown are the same
       choice, and offering them separately only ever produced people wearing
       both. -->
  <div class="field">
    <span class="muted">Avatar decoration</span>
    <button type="button" class="ring-entry" onclick={() => (decorStudio = true)}>
      <Avatar
        name={name || "You"}
        {emoji}
        {color}
        image={avatar}
        size={30}
        decoration={dec}
        {frame}
        style={styleObj}
        {color2}
      />
      <span class="re-text">
        <!-- A gradient ring lives in `frame`, and so does a drawn ring saved
             before the two libraries became one, so the name is looked for in
             both tables. -->
        <strong>
          {DECORATION_BY_ID[dec]?.name || RING_BY_ID[frame]?.name || DECORATION_BY_ID[frame]?.name || "None"}
        </strong>
        <span class="tiny muted"
          >{DECORATIONS.length} drawn · {RINGS.length - 1} gradient · {COLORWAYS.length} colours</span
        >
      </span>
      <span class="chev">›</span>
    </button>
  </div>

  <div class="field">
    <span class="muted">Card frame</span>
    <button type="button" class="ring-entry" onclick={() => (cfStudio = true)}>
      <span class="cf-chip" class:on={!!cf}>
        <span class="cf-mini">
          {#if cf}
            <CardFrame id={cf} {color} color2={color2 || color} />
          {/if}
        </span>
      </span>
      <span class="re-text">
        <strong>{CARD_FRAME_BY_ID[cf]?.name || "None"}</strong>
        <span class="tiny muted">{CARD_FRAMES.length} frames</span>
      </span>
      <span class="chev">›</span>
    </button>
  </div>

  <div class="field">
    <span class="muted">Profile effect</span>
    <button type="button" class="ring-entry" onclick={() => (effectStudio = true)}>
      <!-- A chip in the shape of a preview slot has to BE one. This was a
           gradient of the wearer's two colours, which changed when they picked
           a colour and never when they picked an effect — so the one row whose
           whole job is to say what is on said nothing about it. It runs the
           real components now, the same ones the picker and the card use, so
           it cannot drift from what it claims. -->
      <span class="fx-chip" style="--c1:{color};--c2:{color2 || color}">
        {#if CARD_SCENE_BY_ID[effect]}
          <CardScene id={effect} {color} color2={color2 || color} scale={0.28} />
        {:else if CARD_EFFECT_BY_ID[effect]}
          <FxLayer fx={CARD_EFFECT_BY_ID[effect].fx} seed={effect} scale={0.28} />
        {/if}
      </span>
      <span class="re-text">
        <strong>{CARD_SCENE_BY_ID[effect]?.name || CARD_EFFECT_BY_ID[effect]?.name || (effect ? effect : "None")}</strong>
        <span class="tiny muted">{CARD_SCENES.length} scenes · {CARD_EFFECTS.length} fields</span>
      </span>
      <span class="chev">›</span>
    </button>
  </div>

  <div class="field">
    <!-- GameShelf renders its own "Game collection · N" header, so no field
         label here (that was a duplicate). -->
    <GameShelf games={games} editable onchange={saveGames} />
  </div>

  <div class="field verify-info">
    <span class="muted">Your identity fingerprint (others verify you with this):</span>
    <button class="fpr mono" onclick={copyFingerprint} title="Copy fingerprint">
      {fprGroups}
      <span class="fpr-hint">{fprCopied ? "Copied" : S.isMobile ? "Tap to copy" : "Click to copy"}</span>
    </button>
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={!name.trim()}>Save</button>
  </div>

  {#if decorStudio}
    <DecorStudio
      decoration={dec}
      ring={frame}
      {dc}
      {speed}
      {dir}
      {glow}
      width={ringW}
      {sat}
      {pal}
      {color}
      {color2}
      {avatar}
      {emoji}
      name={name || "You"}
      onApply={(r) => {
        // One slot, two fields. The picker never returns both, so assigning
        // both is what CLEARS the one you did not pick — and it is also what
        // leaves an old profile wearing a ring and a figure alone until the
        // moment its owner chooses, because until then the picker hands back
        // exactly what it was given.
        dec = r.decoration;
        frame = r.ring;
        dc = r.dc;
        speed = r.speed;
        dir = r.dir;
        glow = r.glow;
        ringW = r.width;
        sat = r.sat;
        pal = r.pal;
        decorStudio = false;
      }}
      onClose={() => (decorStudio = false)}
    />
  {/if}

  {#if effectStudio}
    <EffectStudio
      current={effect}
      card={cardProps}
      {color}
      {color2}
      onApply={(r) => {
        effect = r.effect;
        effectStudio = false;
      }}
      onClose={() => (effectStudio = false)}
    />
  {/if}

  {#if cfStudio}
    <CardFrameStudio
      current={cf}
      card={cardProps}
      {color}
      {color2}
      onApply={(r) => {
        cf = r.cf;
        cfStudio = false;
      }}
      onClose={() => (cfStudio = false)}
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
</RailShell>

<style>
  /* The profile-effect row has no avatar to preview against, so it shows the
     card's own colours instead — enough to say "this is about the card". */
  .fx-chip {
    position: relative;
    width: 30px;
    height: 30px;
    flex: none;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: linear-gradient(140deg, var(--c1), var(--c2));
  }

  /* The card-frame row previews a frame, not a colour: a little card with a
     border drawn around it, filled in when one is chosen. */
  /* A frame's identity is its OVERHANG — the battlements above the card, the
     arch over it, the curtain either side. The first version of this chip was
     24×32 with the art squashed into it and clipped to the box, which threw
     away the only part that identifies anything: twelve frames rendered as
     twelve thin coloured outlines, and castle keep was indistinguishable from
     cathedral.
     So the chip is a BOX around a miniature card rather than a card itself.
     The mini keeps the 272×400 proportion the art is authored in, so nothing
     is squashed, and the box around it is the room the overhang needs. */
  /* 30px, the same slot the avatar and the effect chip occupy: three rows one
     under another with a 40px preview in the middle put the middle row's name
     ten pixels right of its neighbours'. The mini card inside is what shrank;
     the chip does not clip, so a frame's overhang still hangs over. */
  .cf-chip {
    position: relative;
    width: 30px;
    height: 30px;
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .cf-mini {
    position: relative;
    width: 17px;
    height: 25px;
    border-radius: 2px;
    background: var(--bg-1);
    box-shadow: 0 0 0 1.5px var(--bg-3);
  }
  .cf-chip.on .cf-mini {
    box-shadow: 0 0 0 1.5px var(--accent);
  }
  .cf-chip.on {
    box-shadow: 0 0 0 3px var(--accent);
  }

  .small-btn {
    font-size: 12px;
    padding: 4px 10px;
    align-self: flex-start;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    text-align: left;
    font-size: 12px;
  }
  .bday-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  /* The two pickers share the row evenly; without this the month name sets
     the width and "Day" is left a stub. */
  .bday-row > :global(.menu-root),
  .bday-row > :global(.pick.off) {
    flex: 1;
    min-width: 0;
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
    gap: var(--sp-3);
  }
  .theme-color {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
  }
  /* ---- live preview ---- */
  .studio {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  /* A card frame's art overhangs its card on purpose, and on the real popover
     it overhangs into the app. Inside a settings sheet it has to stop at the
     sheet: this is the positioned, clipping box that gives it somewhere to be
     — without it the frame anchors to the nearest positioned ancestor, which
     turned out to be the viewport, and painted a cathedral across the app. */
  .pv-stage {
    position: relative;
  }
  .pv-stage.framed {
    overflow: hidden;
    padding: 24px 22px;
  }
  .pv-card {
    position: relative;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--bg-1);
    padding-bottom: var(--sp-3);
  }
  /* A frame draws the card's edge; the card's own border under it reads as a
     seam. */
  .pv-card.framed {
    border-color: transparent;
  }
  .pv-fx {
    position: absolute;
    inset: 0;
    pointer-events: none;
    z-index: 0;
  }
  .pv-head,
  .pv-id {
    position: relative;
    z-index: 1;
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
    gap: var(--sp-3);
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
    gap: var(--sp-4);
    width: 100%;
    padding: 10px 14px;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    text-align: left;
    overflow: clip;
  }
  .ring-entry {
    transition:
      background var(--dur-standard) ease,
      border-color var(--dur-standard) ease;
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
    transition:
      transform var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  .ring-entry:hover .chev {
    color: var(--accent-hover);
    transform: translateX(3px);
  }
  @media (prefers-reduced-motion: reduce) {
    .ring-entry,
    .chev {
      transition: none;
    }
    .ring-entry:hover .chev {
      transform: none;
    }
  }
  .banner-edit {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: #fff;
    background: rgba(0, 0, 0, 0.42);
    backdrop-filter: blur(3px);
    opacity: 0;
    transition: opacity var(--dur-standard) ease;
  }
  .pv-card :global(.pv-banner:hover .banner-edit),
  .pv-card :global(.pv-banner:focus-visible .banner-edit) {
    opacity: 1;
  }
  /* Off a mouse the banner carried NO affordance at all — it is tappable, but
     the only sign of that was a hover scrim. The avatar beside it wears a
     permanent badge, so the banner read as decoration next to something
     obviously editable. Shrink the scrim to a corner pill that is always
     visible, matching the avatar's badge without veiling the artwork. */
  @media (pointer: coarse), (max-width: 768px) {
    .banner-edit {
      inset: auto 8px 8px auto;
      opacity: 1;
      padding: 6px 10px;
      border-radius: 999px;
      background: rgba(0, 0, 0, 0.55);
      backdrop-filter: none;
    }
  }
  .pv-av {
    width: fit-content;
    padding: 3px;
    background: var(--bg-1);
    color: var(--text);
    border-radius: 50%;
    position: relative;
    z-index: 1;
    cursor: pointer;
  }
  /* Camera/edit badge on the avatar — mirrors the banner's "Edit" affordance. */
  .av-edit {
    position: absolute;
    right: 2px;
    bottom: 2px;
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-fg);
    border: 2px solid var(--bg-1);
    opacity: 0.9;
    transition:
      opacity var(--dur-standard) ease,
      transform var(--dur-standard) ease;
  }
  .pv-av:hover .av-edit,
  .pv-av:focus-visible .av-edit {
    opacity: 1;
    transform: scale(1.1);
  }
  .pv-name {
    font-weight: 700;
    font-size: var(--fs-body);
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
  .emoji-row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .emoji {
    background: var(--bg-input);
    border: 1px solid var(--border);
    padding: var(--sp-1) var(--sp-2);
    font-size: 16px;
    border-radius: var(--radius-sm);
    color: var(--text);
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  /* The sheet's touch floor made these 44 tall but left them ~32 wide, so
     thirteen targets sat with their centres 36px apart on the one axis that
     has neighbours — picking 🦊 and getting 🐸. The row already wraps, so the
     width costs a line, not a layout. */
  @media (pointer: coarse), (max-width: 768px) {
    .emoji-row {
      gap: var(--sp-2);
    }
    .emoji {
      min-width: var(--tap-min);
      display: grid;
      place-items: center;
    }
  }
  .emoji:hover {
    transform: scale(1.12);
  }
  .emoji.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    animation: sel-pop 0.25s var(--ease-spring);
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
  /* Four chips of unequal width wrap 3+1, orphaning "Invisible" on a line of
     its own. A 2×2 grid holds all four and reads as one control. */
  @media (pointer: coarse), (max-width: 768px) {
    .presence-row {
      display: grid;
      grid-template-columns: 1fr 1fr;
    }
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
      border-color var(--dur-standard) ease,
      background var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  .presence.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
    animation: sel-pop 0.25s var(--ease-spring);
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
    font-size: var(--fs-ui);
    padding: 8px 10px;
  }
  .cropper {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: var(--sp-3);
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
    gap: var(--sp-2);
    width: 100%;
    color: var(--text-muted);
  }
  .zoom input[type="range"] {
    flex: 1;
  }
  .crop-actions {
    display: flex;
    gap: var(--sp-2);
    align-self: flex-end;
  }
  .tiny {
    font-size: 11px;
    margin: 0;
  }
  .paste-hint {
    align-self: flex-start;
  }
  /* 11px monospace, broken at arbitrary points by word-break, was the worst
     possible treatment for the one string in the app that is read character by
     character. Grouping is done in the markup; this keeps the groups intact
     (break BETWEEN them, never inside) and gives the glyphs room to breathe. */
  .fpr {
    display: block;
    width: 100%;
    text-align: left;
    font-size: var(--fs-ui);
    line-height: 1.6;
    letter-spacing: 0.04em;
    word-break: normal;
    overflow-wrap: break-word;
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 8px 10px;
    border-radius: var(--radius-sm);
  }
  .fpr:hover,
  .fpr:active {
    border-color: var(--accent);
  }
  .fpr-hint {
    display: block;
    margin-top: var(--sp-1);
    font-family: var(--ui-font);
    font-size: var(--fs-small);
    letter-spacing: normal;
    color: var(--text-faint);
  }
</style>
