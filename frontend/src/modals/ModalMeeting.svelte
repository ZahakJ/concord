<script>
  // The just-created instant meeting. Two ways in:
  //   • the GUEST LINK — they click it and land in a browser meeting window,
  //     no install, no account (the killer demo);
  //   • the invite code — for people who have (or want) the full app, which
  //     keeps them fully end-to-end encrypted.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { api } from "../lib/api.js";
  import { S, flash } from "../lib/state.svelte.js";
  import { haptic } from "../lib/touch.js";

  let { code, guestLink = "", guildId = "", expires = 0, onClose } = $props();

  const DOWNLOAD = "https://github.com/ZahakJ/concord/releases/latest";

  // How long the link works. The room dies with it — one lifetime, not two
  // things to reason about — so the label says so out loud. These MIRROR
  // meetingLifetimes in internal/app/guild.go, which is what actually validates
  // the choice; anything off that menu is refused rather than rounded.
  const LIFETIMES = [
    { hours: 1, label: "1 hour" },
    { hours: 24, label: "24 hours" },
    { hours: 24 * 7, label: "7 days" },
    { hours: 24 * 30, label: "30 days" },
  ];

  let link = $state(guestLink);
  let expiresAt = $state(expires);
  // A brand-new meeting is on the 24h default, so that chip starts selected —
  // the control reads as "this is what it is now", not as an unanswered question.
  let picked = $state(24);
  let busy = $state(false);

  // Absolute date AND the human distance: "until Tue 3 Feb, 14:20" answers the
  // "can I send this to next week's meeting?" question that "in 7 days" doesn't.
  const expiryText = $derived(
    expiresAt
      ? new Date(expiresAt).toLocaleString(undefined, {
          weekday: "short",
          day: "numeric",
          month: "short",
          hour: "2-digit",
          minute: "2-digit",
        })
      : "",
  );

  async function setLifetime(hours) {
    if (!guildId || busy || hours === picked) return;
    busy = true;
    try {
      // Re-minting returns the SAME url with a new expiry, so a link already
      // pasted somewhere keeps working.
      link = await api.createGuestLink(guildId, hours);
      expiresAt = await api.meetingExpiry(guildId);
      picked = hours;
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  const guestBlurb = $derived(
    link
      ? `Hop on a quick call with me ⚡\n\n${link}\n\n` +
        `Just click it — it opens in your browser, no install, no account.` +
        (expiryText ? `\n\n(The link works until ${expiryText}.)` : "")
      : "",
  );

  const appBlurb =
    `Join my Concord meeting ⚡\n` +
    `\n` +
    `1. Grab Concord (free, no account needed): ${DOWNLOAD}\n` +
    `2. Open it, pick any passphrase, and paste this invite:\n` +
    `\n` +
    `${code}\n` +
    `\n` +
    `See you there! (End-to-end encrypted — no server ever sees the call.)`;

  let copied = $state("");
  function copy(what, text) {
    navigator.clipboard?.writeText(text);
    haptic("light");
    copied = what;
    setTimeout(() => (copied = ""), 1600);
  }

  // Getting the link to someone is the entire purpose of this screen, and on a
  // phone the destination is another messaging app. Copy-then-switch-then-paste
  // is the desktop route; the OS share sheet is the native one. Copy stays as
  // the fallback for when the sheet is dismissed or missing.
  const canShare = typeof navigator !== "undefined" && !!navigator.share && S.isMobile;
  async function share(text) {
    try {
      await navigator.share({ text });
      haptic("light");
    } catch {
      /* dismissed or refused — Copy is still right there */
    }
  }
</script>

<Modal title="Your meeting is live" wide {onClose}>
  {#if link}
    <section class="way primary-way">
      <div class="way-head">
        <span class="chip guest"><Icon name="link" size={15} /></span>
        <div class="way-text">
          <strong>Guest link — no install</strong>
          <span class="muted">They click it and they're in, right in their browser.</span>
        </div>
      </div>
      <div class="link-row">
        <code class="link">{link}</code>
        {#if canShare}
          <button onclick={() => share(link)}>Share…</button>
        {/if}
        <button class="ghost" class:done={copied === "link"} onclick={() => copy("link", link)}>
          {copied === "link" ? "Copied ✓" : "Copy link"}
        </button>
      </div>
      <div class="life">
        <span class="muted tiny life-label">Works for</span>
        <div class="chips" role="group" aria-label="Guest link lifetime">
          {#each LIFETIMES as l (l.hours)}
            <button
              class="chip-btn"
              class:on={picked === l.hours}
              disabled={busy || !guildId}
              onclick={() => setLifetime(l.hours)}
            >
              {l.label}
            </button>
          {/each}
        </div>
      </div>
      {#if expiryText}
        <p class="muted tiny" aria-live="polite">
          Link and meeting expire <strong>{expiryText}</strong>. Anyone with the
          link can walk in until then — lock the call (🔒) to make them knock
          first, which is how you'd run office hours.
        </p>
      {/if}
      <button
        class="ghost small"
        class:done={copied === "guest"}
        onclick={() => (canShare ? share(guestBlurb) : copy("guest", guestBlurb))}
      >
        {#if canShare}
          Share link + a friendly note
        {:else}
          {copied === "guest" ? "Copied ✓" : "Copy link + a friendly note"}
        {/if}
      </button>
      <p class="muted tiny">
        Guests are chat-only and labelled in the room — their messages pass
        through you, so they aren't end-to-end encrypted like full members.
      </p>
    </section>
  {/if}

  <section class="way">
    <div class="way-head">
      <span class="chip"><Icon name="concorde" size={15} /></span>
      <div class="way-text">
        <strong>Invite to the full app</strong>
        <span class="muted">Voice + chat, fully end-to-end encrypted. Best for people who'll stick around.</span>
      </div>
    </div>
    <div class="btn-row">
      <button class="ghost" class:done={copied === "code"} onclick={() => copy("code", code)}>
        {copied === "code" ? "Copied ✓" : "Copy invite code"}
      </button>
      <button
        class:done={copied === "app"}
        onclick={() => (canShare ? share(appBlurb) : copy("app", appBlurb))}
      >
        <Icon name="copy" size={14} />
        {#if canShare}
          Share invitation
        {:else}
          {copied === "app" ? "Copied ✓" : "Copy invitation"}
        {/if}
      </button>
    </div>
  </section>

  {#if !link}
    <p class="muted tiny nofoot">
      Guest links (browser, no install) need a rendezvous server — set one in
      Settings → Connection.
    </p>
  {/if}
</Modal>

<style>
  .way {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .way + .way {
    margin-top: 10px;
  }
  .primary-way {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    background: color-mix(in srgb, var(--accent) 6%, transparent);
  }
  .way-head {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  .chip {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    flex: none;
    border-radius: 8px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .chip.guest {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .way-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    font-size: var(--fs-ui);
  }
  .way-text .muted {
    font-size: var(--fs-compact);
    line-height: 1.45;
  }
  .link-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .link {
    flex: 1;
    min-width: 0;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .btn-row {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .btn-row button,
  .link-row button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
  }
  .life {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .life-label {
    flex: none;
  }
  .chips {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .chip-btn {
    font-size: var(--fs-compact);
    padding: 5px 10px;
    background: var(--bg-0);
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .chip-btn.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .chip-btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .small {
    font-size: var(--fs-compact);
    padding: 6px 10px;
    align-self: flex-start;
  }
  .done {
    background: var(--ok) !important;
    color: var(--ok-fg);
  }
  .tiny {
    font-size: var(--fs-small);
    margin: 0;
    line-height: 1.5;
  }
  .nofoot {
    margin-top: 10px;
  }
  /* A 393px sheet cannot hold a truncated URL and two buttons on one line, and
     the pair of copy buttons below it were huddling at the right edge. Both
     become stacked full-width rows — the same treatment app.css gives .actions. */
  @media (pointer: coarse), (max-width: 768px) {
    .link-row,
    .btn-row {
      flex-wrap: wrap;
    }
    .link {
      flex: 1 0 100%;
    }
    .btn-row button,
    .link-row button {
      flex: 1;
      justify-content: center;
    }
  }
</style>
