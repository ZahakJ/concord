<script>
  import Modal from "./Modal.svelte";
  import Avatar from "../Avatar.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { soundsEnabled, setSoundsEnabled } from "../lib/sounds.js";
  import { S, setPref, flash } from "../lib/state.svelte.js";

  let { onClose, onSaved } = $props();
  let bootstrap = $state("");
  let saved = $state(false);
  let sounds = $state(soundsEnabled());
  let phrase = $state("");
  let copiedPhrase = $state(false);

  function toggleSounds() {
    sounds = !sounds;
    setSoundsEnabled(sounds);
  }

  async function reveal() {
    try {
      phrase = await api.revealMnemonic();
    } catch (err) {
      flash(err);
    }
  }
  function copyPhrase() {
    navigator.clipboard?.writeText(phrase);
    copiedPhrase = true;
    setTimeout(() => (copiedPhrase = false), 1600);
  }

  onMount(async () => {
    try {
      bootstrap = ((await api.getBootstrap()) || []).join("\n");
    } catch {
      /* ignore */
    }
  });

  async function save() {
    try {
      await api.setBootstrapLive(bootstrap);
      saved = true;
      onSaved?.();
      setTimeout(() => onClose(), 700);
    } catch (err) {
      flash(err);
    }
  }
</script>

<Modal title="Settings" {onClose}>
  <button class="profile-row" onclick={() => (S.modal = { kind: "profile" })}>
    <Avatar
      name={S.displayName || S.identity.displayName || "You"}
      emoji={S.identity.emoji}
      color={S.identity.color}
      image={S.identity.avatar}
      size={44}
    />
    <span class="profile-text">
      <strong>{S.displayName || S.identity.displayName || "Your profile"}</strong>
      <span class="muted tiny">Edit name, avatar, color, status &amp; bio</span>
    </span>
    <span class="muted chev-r">›</span>
  </button>

  <hr />
  <p class="muted">
    <strong>Rendezvous server</strong> — the address of the tiny relay that lets
    friends on other networks find you. You only need this if <em>you</em> host
    the server; friends get it automatically from your invite code.
  </p>
  <textarea
    rows="3"
    placeholder="/dns/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
    bind:value={bootstrap}
  ></textarea>
  <p class="muted tiny">
    Leave blank for same-Wi-Fi only. Takes effect immediately for new
    connections; a full restart applies it everywhere.
  </p>
  <div class="actions">
    <button class="ghost" onclick={onClose}>Close</button>
    <button onclick={save}>{saved ? "Saved ✓" : "Save"}</button>
  </div>

  <hr />
  <button class="toggle-row" onclick={toggleSounds} role="switch" aria-checked={sounds}>
    <span>
      <strong>Sounds</strong>
      <span class="muted tiny">Voice join/leave chimes and @mention pings</span>
    </span>
    <span class="switch" class:on={sounds}><span class="knob"></span></span>
  </button>

  <button
    class="toggle-row"
    onclick={() => setPref("linkPreviews", !S.prefs.linkPreviews)}
    role="switch"
    aria-checked={S.prefs.linkPreviews}
  >
    <span>
      <strong>Link previews</strong>
      <span class="muted tiny">
        Off by default: loading a preview reveals your IP to the link's host, so a
        message with a link can pinpoint you. Turn on only among people you trust.
      </span>
    </span>
    <span class="switch" class:on={S.prefs.linkPreviews}><span class="knob"></span></span>
  </button>

  <hr />
  <div class="recovery">
    <strong>Recovery phrase</strong>
    <p class="muted tiny">
      24 words that ARE your account. Write them down and keep them somewhere
      safe — with them you can restore your identity on a new device or after a
      forgotten passphrase. Anyone who has them can become you, so never share them.
    </p>
    {#if phrase}
      <div class="phrase">
        {#each phrase.split(" ") as w, i (i)}
          <span class="word"><span class="num">{i + 1}</span>{w}</span>
        {/each}
      </div>
      <button class="ghost small-btn" class:done={copiedPhrase} onclick={copyPhrase}>
        {copiedPhrase ? "Copied ✓" : "Copy phrase"}
      </button>
    {:else}
      <button class="ghost small-btn" onclick={reveal}>Show recovery phrase</button>
    {/if}
  </div>

  <hr />
  <button
    class="ghost signout"
    onclick={async () => {
      await api.logout();
      location.reload();
    }}
  >
    Sign out (lock this device)
  </button>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    text-align: left;
  }
  .tiny {
    font-size: 11px;
  }
  textarea {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    resize: vertical;
  }
  hr {
    border: none;
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }
  .signout {
    color: var(--danger);
    align-self: flex-start;
    font-size: 13px;
  }
  .recovery {
    display: flex;
    flex-direction: column;
    gap: 8px;
    text-align: left;
  }
  .recovery strong {
    font-size: 13px;
  }
  .phrase {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 5px;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px;
  }
  .word {
    display: flex;
    align-items: baseline;
    gap: 5px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .num {
    color: var(--text-faint);
    font-size: 10px;
    width: 16px;
    text-align: right;
  }
  .small-btn {
    align-self: flex-start;
    font-size: 12px;
    padding: 5px 12px;
  }
  .small-btn.done {
    color: var(--ok);
    border-color: var(--ok);
  }
  .profile-row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text);
    text-align: left;
    padding: 10px 12px;
    border-radius: var(--radius-md);
  }
  .profile-row:hover {
    background: var(--bg-3);
    border-color: var(--accent);
  }
  .profile-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .chev-r {
    font-size: 20px;
    line-height: 1;
  }
  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 2px;
  }
  .toggle-row:hover {
    background: transparent;
  }
  .toggle-row span {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .switch {
    flex-shrink: 0;
    width: 38px;
    height: 22px;
    border-radius: 11px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    display: block;
    position: relative;
    transition: background 0.15s ease;
  }
  .switch.on {
    background: var(--accent);
    border-color: var(--accent);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: white;
    transition: transform 0.15s ease;
  }
  .switch.on .knob {
    transform: translateX(16px);
  }
</style>
