<script>
  import RailShell from "./RailShell.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, flash, refreshGuilds, nameFor, openPanel } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";
  let { code = "", onCopy, onClose } = $props();

  // Reached from the guild rail there is no code in hand: a rail entry is a
  // destination, not a call site that can go and fetch one first. So the panel
  // fetches its own when it was handed none, which is also what makes "Invite
  // people" a place you can walk back to rather than a button you have to find
  // again. Every other caller still passes the code it already had.
  let own = $state("");
  const shown = $derived(code || own);
  $effect(() => {
    if (code || own || !S.activeGuildId) return;
    let live = true;
    api.inviteCode(S.activeGuildId).then(
      (c) => {
        if (live) own = c;
      },
      (err) => flash(err),
    );
    return () => (live = false);
  });

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
    const raw = shown;
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
    onCopy(shown);
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
      await navigator.share({ text: shown });
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
      flash(`Invited ${nameFor(c.fingerprint) || "them"} — they'll show up when they accept`, "success");
      setTimeout(refreshGuilds, 2500);
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }
</script>

<RailShell title="Invite a friend" {onClose}>
  <p class="lead">
    This code carries everything your friend needs — the guild, how to reach you, and your
    relay. They pick a passphrase, paste it into <strong>Join with invite</strong>, and they're in.
  </p>

  <!-- The QR on white with its own quiet zone, and beside it the code as a
       read-only field you can select. The one action, Copy invite, is the
       footer's primary — it was an outlined button inside a boxed well, with
       Done as the only footer action and no primary at all. -->
  <div class="code-well">
    {#if qr}
      <figure class="qr">
        <img src={qr} alt="This invite code, as a QR code" />
        <figcaption>Point a camera at it</figcaption>
      </figure>
    {/if}
    <div class="code-col">
      <span class="lbl">Invite code</span>
      <code class="code-line" title="Copy takes the whole code">{shown || "Fetching this guild's code…"}</code>
      <p class="hint">
        Anyone with this code can join, so share it directly with people you trust.
      </p>
      <!-- A troubleshooting question, so a quiet link under the code it is
           about rather than a third button in the footer. -->
      <button class="link check" onclick={() => openPanel("reach", "invite")}>
        Can people reach me?
      </button>
    </div>
  </div>

  {#if candidates.length}
    <div class="divider"></div>
    <strong class="add-head">Or add a verified contact directly</strong>
    <p class="hint">No code needed. They get an invite; they won't appear in the room until they accept.</p>
    <div class="add-list">
      {#each candidates as c (c.fingerprint)}
        <div class="add-row">
          <Avatar
            name={nameFor(c.fingerprint)}
            image={c.avatar || ""}
            emoji={c.emoji || ""}
            color={c.color || ""}
            size={28}
          />
          <span class="who">
            <strong>{nameFor(c.fingerprint)}</strong>
            <span class="tiny muted mono">{c.fingerprint.slice(0, 9)}</span>
          </span>
          {#if added.has(c.fingerprint)}
            <span class="done tiny"><Icon name="check" size={12} /> Invited</span>
          {:else}
            <button class="quiet sm" disabled={busy === c.fingerprint} onclick={() => add(c)}>
              {busy === c.fingerprint ? "Adding…" : "Add"}
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  <div class="actions">
    {#if canShare}
      <button class="quiet" class:copied onclick={copy}>{copied ? "Copied" : "Copy"}</button>
      <button class="primary" onclick={share}>Share invite…</button>
    {:else}
      <button class="primary" class:copied onclick={copy}>
        {#if copied}<Icon name="check" size={14} />{/if}
        {copied ? "Copied" : "Copy invite"}
      </button>
    {/if}
  </div>
</RailShell>

<style>
  .lead {
    margin: 0;
    line-height: 1.55;
  }
  .divider {
    border-top: 1px solid var(--hairline);
    margin: var(--sp-1) 0;
  }
  .add-head {
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .add-list {
    display: flex;
    flex-direction: column;
    max-height: 240px;
    overflow-y: auto;
  }
  .add-row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 44px;
    padding: 6px 0;
  }
  .add-row + .add-row {
    border-top: 1px solid var(--hairline);
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .who strong {
    font-size: var(--fs-ui);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono {
    font-family: var(--mono-font);
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .done {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    color: var(--ok-text);
  }
  .code-well {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-5);
  }
  .code-col {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lbl {
    font-size: var(--fs-compact);
    font-weight: 500;
  }
  /* The code as a read-only field: one line, mono, scrolls sideways. A few
     hundred characters of reach-me addresses is not a thing anyone reads;
     Copy takes every byte. */
  .code-line {
    display: block;
    min-width: 0;
    padding: var(--sp-2) var(--sp-3);
    font-family: var(--mono-font);
    font-size: var(--fs-small);
    line-height: 1.4;
    color: var(--text);
    background: var(--bg-input);
    box-shadow: inset 0 0 0 1px var(--border);
    border-radius: var(--radius-md);
    white-space: nowrap;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: thin;
    user-select: all;
  }
  .qr {
    margin: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    flex: none;
  }
  .qr img {
    /* White quiet zone regardless of theme — a dark-mode QR with a dark margin
       is one a camera will not lock onto. 168px with a 12px quiet zone. */
    width: 168px;
    height: 168px;
    box-sizing: border-box;
    display: block;
    border-radius: var(--radius-sm);
    background: #fff;
    padding: var(--sp-3);
  }
  .qr figcaption {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .hint {
    margin: 0;
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .check {
    align-self: flex-start;
    font-size: var(--fs-compact);
  }
  .copied {
    background: var(--ok);
    color: var(--ok-fg);
  }
  @media (max-width: 768px) {
    .code-well {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
