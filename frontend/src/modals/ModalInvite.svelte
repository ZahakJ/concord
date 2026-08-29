<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, flash, refreshGuilds, nameFor, openPanel } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";
  let { code, onCopy, onClose } = $props();

  // The code as a picture. Three hundred characters is not a thing anyone reads
  // aloud or retypes, and until now the only way to move it between two people
  // in the same room was a messaging app and a round trip through the network
  // the code exists to bootstrap. A camera does it directly.
  //
  // The qrcode library is a lazy chunk (it is 152KB and most sessions never
  // open this dialog); a failure to load simply leaves the code on its own,
  // which is exactly what this dialog was before.
  let qr = $state("");
  $effect(() => {
    let live = true;
    const raw = code;
    if (!raw) return;
    import("qrcode")
      .then(({ default: QRCode }) =>
        QRCode.toDataURL(raw, { margin: 1, width: 320, errorCorrectionLevel: "L" }),
      )
      .then((url) => {
        if (live) qr = url;
      })
      .catch(() => {
        /* no picture, same dialog as before */
      });
    return () => (live = false);
  });

  let copied = $state(false);
  function copy() {
    onCopy(code);
    haptic("light");
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }

  // On a phone the whole point of this screen is to get the code into whatever
  // messaging app the other person is already in. Copy-then-switch-apps-then-
  // paste is the desktop
  // metaphor; the OS share sheet is one tap. Offered only where it exists, and
  // it falls back to Copy if the sheet is dismissed or the API is missing.
  const canShare = typeof navigator !== "undefined" && !!navigator.share && S.isMobile;
  async function share() {
    try {
      await navigator.share({ text: code });
      haptic("light");
    } catch {
      /* dismissed, or the platform refused — the Copy button is still there */
    }
  }

  // Add verified contacts straight in — no code needed. Their client
  // auto-accepts only because they verified US, so this is safe both ways.
  const memberFprs = $derived(new Set(S.members.map((m) => m.fingerprint)));
  const candidates = $derived(
    S.contacts.filter((c) => c.verified && !memberFprs.has(c.fingerprint)),
  );
  let busy = $state("");
  let added = $state(new Set());
  async function add(c) {
    busy = c.fingerprint;
    try {
      await api.addMember(S.activeGuildId, c.fingerprint);
      added = new Set([...added, c.fingerprint]);
      flash(`Added ${nameFor(c.fingerprint) || "them"} — they'll appear once they accept`, "success");
      setTimeout(refreshGuilds, 2500);
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }
</script>

<Modal title="Invite a friend" {onClose}>
  <p class="muted lead">
    This code carries everything your friend needs — the guild, how to reach you, and your
    relay. They pick a passphrase, paste it into <strong>Join with invite</strong>, and they're in.
  </p>

  <div class="code-well">
    <div class="code-row">
      <code>{code}</code>
      {#if qr}
        <figure class="qr">
          <img src={qr} alt="This invite code, as a QR code" />
          <figcaption>Point a camera at it</figcaption>
        </figure>
      {/if}
    </div>
    <div class="give">
      {#if canShare}
        <button class="share" onclick={share}>
          <Icon name="spark" size={14} /> Share code…
        </button>
      {/if}
      <button class="copy" class:copied class:secondary={canShare} onclick={copy}>
        <Icon name={copied ? "check" : "spark"} size={14} />
        {copied ? "Copied" : "Copy code"}
      </button>
    </div>
  </div>

  <p class="hint muted">
    <Icon name="spark" size={12} /> Anyone with this code can join, so share it directly with people
    you trust.
  </p>

  {#if candidates.length}
    <div class="divider"></div>
    <strong class="add-head">Or add a verified contact directly</strong>
    <p class="hint muted">No code needed — they drop straight in.</p>
    <div class="add-list">
      {#each candidates as c (c.fingerprint)}
        <div class="add-row">
          <Avatar
            name={nameFor(c.fingerprint)}
            image={c.avatar || ""}
            emoji={c.emoji || ""}
            color={c.color || ""}
            size={30}
          />
          <span class="who">
            <strong>{nameFor(c.fingerprint)}</strong>
            <span class="tiny muted mono">{c.fingerprint.slice(0, 9)}</span>
          </span>
          {#if added.has(c.fingerprint)}
            <span class="done tiny"><Icon name="check" size={12} /> Added</span>
          {:else}
            <button class="add-btn" disabled={busy === c.fingerprint} onclick={() => add(c)}>
              {busy === c.fingerprint ? "Adding…" : "Add"}
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  <div class="actions">
    <!-- Demoted from a bordered button in the middle of the flow to a link at
         the end of it. The question it answers — "will that actually work from
         their network?" — is a real one and worth reaching from here, but it is
         a troubleshooting question, and putting it between the code and the
         Copy button implied that inviting someone normally requires a
         diagnostic first. -->
    <button class="check" onclick={() => openPanel("reach", "invite")}>
      Can people reach me?
    </button>
    <button class="ghost" onclick={onClose}>Done</button>
  </div>
</Modal>

<style>
  .lead {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.55;
  }
  .divider {
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }
  .add-head {
    font-size: var(--fs-ui);
  }
  .add-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 240px;
    overflow-y: auto;
  }
  .add-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 9px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .who strong {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono {
    font-family: ui-monospace, monospace;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .add-btn {
    flex-shrink: 0;
    padding: 6px 14px;
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
  }
  .add-btn:disabled {
    opacity: 0.6;
  }
  .done {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    color: var(--ok-text);
  }
  .code-well {
    position: relative;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-3);
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow: hidden;
  }
  /* One shimmer sweep on open — "here's the valuable thing". */
  .code-well::after {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(
      105deg,
      transparent 30%,
      color-mix(in srgb, var(--accent) 14%, transparent) 50%,
      transparent 70%
    );
    transform: translateX(-100%);
    animation: shimmer 1.1s ease 0.25s 1 forwards;
    pointer-events: none;
  }
  @keyframes shimmer {
    to {
      transform: translateX(100%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .code-well::after {
      animation: none;
      opacity: 0;
    }
  }
  .code-row {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-3);
  }
  code {
    flex: 1;
    min-width: 0;
    font-family: ui-monospace, monospace;
    font-size: var(--fs-compact);
    line-height: 1.5;
    word-break: break-all;
    color: var(--text);
    /* No max-height. A 120px window over a three-hundred-character code cut a
       glyph in half at the bottom edge, which reads as a code that has been
       truncated rather than one that has been scrolled — and the whole anxiety
       of this dialog is whether you have got all of it. The phone build removed
       the cap for exactly that reason and the desktop one never followed. The
       dialog scrolls; let it. */
  }
  .qr {
    margin: 0;
    flex: none;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
  }
  .qr img {
    /* White quiet zone regardless of theme — a dark-mode QR with a dark margin
       is one a camera will not lock onto. */
    width: 132px;
    height: 132px;
    display: block;
    border-radius: var(--radius-sm);
    background: #fff;
    padding: 5px;
  }
  .qr figcaption {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    text-align: center;
    max-width: 132px;
  }
  .give {
    display: flex;
    gap: var(--sp-2);
  }
  .share {
    flex: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: var(--fs-ui);
    font-weight: 600;
    padding: 7px 16px;
  }
  .copy {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: var(--fs-ui);
    font-weight: 600;
    padding: 7px 16px;
    transition:
      transform var(--dur-quick) ease,
      background var(--dur-calm) ease,
      box-shadow var(--dur-calm) ease;
  }
  .copy:active {
    transform: scale(0.97);
  }
  .copy.copied {
    background: var(--ok);
    color: var(--ok-fg);
    box-shadow: 0 0 14px color-mix(in srgb, var(--ok) 45%, transparent);
    animation: copied-pop 0.3s var(--ease-spring);
  }
  @keyframes copied-pop {
    40% {
      transform: scale(1.06);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .copy.copied {
      animation: none;
    }
  }
  .hint {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: var(--fs-compact);
  }
  /* Quiet by design: it sits beside a warning about who may join, and must not
     compete with Copy for the eye. */
  /* A link in the footer, not a control in the flow: no border, no fill, and
     pushed to the left so it reads as an aside to Done rather than a choice
     alongside it. */
  .check {
    margin-right: auto;
    padding: 6px 0;
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    text-decoration: underline;
    text-underline-offset: 3px;
    text-decoration-color: var(--border);
  }
  .check:hover,
  .check:focus-visible {
    color: var(--text);
    text-decoration-color: var(--accent);
  }
  /* Phone: handing the code over is the whole point — make it unmissable, and
     demote Copy to a quiet partner once the OS share sheet is available. */
  @media (pointer: coarse), (max-width: 768px) {
    .copy {
      align-self: stretch;
      flex: 1;
      min-height: 48px;
    }
    .share {
      min-height: 48px;
    }
    .copy.secondary {
      flex: 0 0 auto;
      background: var(--bg-3);
      color: var(--text);
    }
    /* A sheet is narrow enough that the code and the picture want to be
       stacked, not side by side. */
    .code-row {
      flex-direction: column;
      align-items: stretch;
    }
    .qr {
      align-self: center;
    }
    /* The footer stacks into full-width buttons here; a bare link in that stack
       would look like a third button that forgot its chrome. */
    .check {
      margin-right: 0;
      min-height: 0;
      order: -1;
    }
    .add-list {
      max-height: none;
      overflow-y: visible;
    }
  }
</style>
