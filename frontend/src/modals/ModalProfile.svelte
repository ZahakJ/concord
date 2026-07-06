<script>
  import Modal from "./Modal.svelte";
  let { identity, onSubmit, onClose } = $props();
  let name = $state(identity.displayName || "");
  let status = $state(identity.status || "");
  let emoji = $state(identity.emoji || "");
  let color = $state(identity.color || "#14a394");
  let avatar = $state(identity.avatar || "");
  let fileInput;

  const EMOJIS = ["😀", "😎", "🦊", "🐸", "👾", "🧙", "🚀", "🌸", "⚡", "🔥", "🌙", "🎮"];

  // pickImage downscales the chosen picture to a 96×96 JPEG data URI so the
  // profile broadcast stays tiny (~5-10 KB).
  async function pickImage(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const img = new Image();
    const url = URL.createObjectURL(file);
    await new Promise((res, rej) => {
      img.onload = res;
      img.onerror = rej;
      img.src = url;
    });
    const SIZE = 96;
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = SIZE;
    const ctx = canvas.getContext("2d");
    // Cover-crop to a square from the image center.
    const side = Math.min(img.width, img.height);
    ctx.drawImage(
      img,
      (img.width - side) / 2,
      (img.height - side) / 2,
      side,
      side,
      0,
      0,
      SIZE,
      SIZE,
    );
    URL.revokeObjectURL(url);
    avatar = canvas.toDataURL("image/jpeg", 0.82);
  }

  function save() {
    onSubmit({ name: name.trim(), status: status.trim(), emoji, color, avatar });
  }
</script>

<Modal title="Your profile" {onClose}>
  <div class="preview">
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
      <span class="cam">📷</span>
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
      pickImage(e.target.files?.[0]);
      e.target.value = "";
    }}
  />
  {#if avatar}
    <button class="ghost small-btn" onclick={() => (avatar = "")}>Remove picture</button>
  {/if}

  <label class="field">
    <span class="muted">Display name</span>
    <input bind:value={name} maxlength="32" placeholder="Your name" />
  </label>
  <label class="field">
    <span class="muted">Status</span>
    <input bind:value={status} maxlength="64" placeholder="e.g. building something epic" />
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
  <label class="field row-field">
    <span class="muted">Accent color</span>
    <input type="color" bind:value={color} />
  </label>

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
  }
  .avatar img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .cam {
    position: absolute;
    bottom: -2px;
    right: -2px;
    font-size: 13px;
    background: var(--bg-elevated);
    border-radius: 50%;
    padding: 1px;
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
  }
  .emoji.sel {
    border-color: var(--accent);
    background: rgba(91, 110, 245, 0.2);
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
